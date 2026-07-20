// 游戏服务器入口（pitaya 风格内核版本）
//
// 启动流程：
//  1. 加载配置（configs/config.yaml）
//  2. 初始化日志
//  3. 初始化数据存储（MySQL + Redis）
//  4. 初始化业务组件（登录、游戏、排行榜、支付、战斗、GM）
//  5. 组装 kernel：注册 pipeline 钩子（认证、限流）+ 注册各组件消息
//  6. 启动 WebSocket 传输层 + 支付回调 HTTP 服务器
//
// 与旧版区别：不再有手写 Router/中间件，改为 kernel（反射分发）+ pipeline（钩子）+
// transport（WebSocket 收发）+ session（玩家身份）。业务 handler 均为
// func(ctx,*Req)(*Resp,error) 组件方法。线上协议帧格式不变，客户端零改动。
//
// 优雅关闭：收到 SIGINT/SIGTERM 后，依次关闭回调 HTTP 服务、传输层（断开所有连接）、数据存储、刷新日志。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"game-server/internal/combat"
	"game-server/internal/config"
	"game-server/internal/game"
	"game-server/internal/gm"
	"game-server/internal/hooks"
	"game-server/internal/kernel"
	"game-server/internal/login"
	"game-server/internal/payment"
	"game-server/internal/pipeline"
	"game-server/internal/protocol"
	"game-server/internal/rank"
	"game-server/internal/store"
	"game-server/internal/transport"
	"game-server/pkg/logger"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// ========== 1. 加载配置 ==========
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// ========== 2. 初始化日志 ==========
	logger.Init(cfg.Log.Level, cfg.Log.Filename, cfg.Log.MaxSize, cfg.Log.MaxBackups, cfg.Log.MaxAge)
	defer logger.Sync()

	zap.L().Info("========== 游戏服务器启动 ==========")
	zap.L().Info("配置加载成功", zap.String("path", *configPath))

	// ========== 3. 初始化数据存储 ==========
	mysqlStore, err := store.NewMySQLStore(&cfg.MySQL)
	if err != nil {
		zap.L().Warn("MySQL 初始化失败，将降级运行", zap.Error(err))
	}
	defer func() {
		if mysqlStore != nil {
			mysqlStore.Close()
		}
	}()

	redisStore, err := store.NewRedisStore(&cfg.Redis)
	if err != nil {
		zap.L().Warn("Redis 初始化失败，将降级运行", zap.Error(err))
	}

	// ========== 4. 初始化业务组件 ==========
	wechatClient := login.NewWechatClient(&cfg.Wechat)
	loginHandler := login.NewHandler(mysqlStore, redisStore, wechatClient)
	gameHandler := game.NewHandler(mysqlStore, redisStore)
	rankHandler := rank.NewHandler(redisStore, mysqlStore)
	paymentHandler := payment.NewHandler(mysqlStore, redisStore, &cfg.Wechat)
	combatHandler := combat.NewHandler(mysqlStore, redisStore)

	// ========== 5. 组装 kernel ==========
	hookChain := pipeline.New()
	k := kernel.New(hookChain)

	// 5.1 注册免鉴权消息（登录、心跳）
	k.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp, loginHandler.Login, kernel.AuthFree())
	k.Register(protocol.MsgID_HeartbeatReq, protocol.MsgID_HeartbeatResp, loginHandler.Heartbeat, kernel.AuthFree())

	// 5.2 游戏存档
	k.Register(protocol.MsgID_SaveArchiveReq, protocol.MsgID_SaveArchiveResp, gameHandler.SaveArchive)
	k.Register(protocol.MsgID_LoadArchiveReq, protocol.MsgID_LoadArchiveResp, gameHandler.LoadArchive)

	// 5.3 排行榜
	k.Register(protocol.MsgID_GetRankReq, protocol.MsgID_GetRankResp, rankHandler.GetRank)
	k.Register(protocol.MsgID_SubmitScoreReq, protocol.MsgID_SubmitScoreResp, rankHandler.SubmitScore)

	// 5.4 支付（仅 WebSocket 下单；回调走 HTTP）
	k.Register(protocol.MsgID_CreateOrderReq, protocol.MsgID_CreateOrderResp, paymentHandler.CreateOrder)

	// 5.5 战斗
	k.Register(protocol.MsgID_CombatResultReq, protocol.MsgID_CombatResultResp, combatHandler.CombatResult)
	k.Register(protocol.MsgID_GetEnemyConfigsReq, protocol.MsgID_GetEnemyConfigsResp, combatHandler.GetEnemyConfigs)
	k.Register(protocol.MsgID_GetDungeonConfigReq, protocol.MsgID_GetDungeonConfigResp, combatHandler.GetDungeonConfig)
	k.Register(protocol.MsgID_GetStyleConfigsReq, protocol.MsgID_GetStyleConfigsResp, combatHandler.GetStyleConfigs)
	k.Register(protocol.MsgID_UnlockStyleReq, protocol.MsgID_UnlockStyleResp, combatHandler.UnlockStyle)
	k.Register(protocol.MsgID_GetPlayerStatsReq, protocol.MsgID_GetPlayerStatsResp, combatHandler.GetPlayerStats)
	k.Register(protocol.MsgID_UpdatePlayerStatsReq, protocol.MsgID_UpdatePlayerStatsResp, combatHandler.UpdatePlayerStats)

	// ========== 6. 启动服务器 ==========
	server := transport.NewServer(k)

	// GM 组件需要 Hub 引用（广播、在线统计）。
	gmHandler := gm.NewHandler(mysqlStore, redisStore, server.Hub(), cfg.GM.AdminUIDs)
	k.Register(protocol.MsgID_GMCommandReq, protocol.MsgID_GMCommandResp, gmHandler.Command)

	// 注册 pipeline 前置钩子（先认证后限流）。必须在 kernel 注册完成后，
	// 因为钩子依赖 kernel.IsAuthFree 判断免鉴权消息。
	if redisStore != nil {
		hookChain.AddBefore(hooks.Auth(redisStore, k))
		hookChain.AddBefore(hooks.RateLimit(redisStore, k, 100, time.Second))
	}

	// 在独立 goroutine 启动 WebSocket 服务器
	go func() {
		if err := server.Start(cfg.Server.Addr()); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("服务器启动失败", zap.Error(err))
		}
	}()

	// 支付回调 HTTP 服务器（微信支付回调是 HTTP POST，与 WebSocket 解耦，独立端口）
	callbackMux := http.NewServeMux()
	callbackMux.HandleFunc("/pay/callback", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			zap.L().Error("读取支付回调请求体失败", zap.Error(err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		resp, err := paymentHandler.HandlePayCallback(body)
		if err != nil {
			zap.L().Error("支付回调处理失败", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		jsonResp, _ := json.Marshal(resp)
		w.Write(jsonResp)
	})
	callbackServer := &http.Server{Addr: ":8081", Handler: callbackMux}
	go func() {
		zap.L().Info("HTTP 回调服务器启动", zap.String("addr", ":8081"))
		if err := callbackServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("回调服务器启动失败", zap.Error(err))
		}
	}()

	zap.L().Info("游戏服务器已启动",
		zap.String("ws_addr", cfg.Server.Addr()),
		zap.String("callback_addr", ":8081"),
		zap.String("name", cfg.Server.Name),
	)

	// ========== 7. 等待退出信号 ==========
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	zap.L().Info("收到退出信号，开始优雅关闭...", zap.String("signal", sig.String()))

	// 7.1 关闭 HTTP 回调服务器（5 秒超时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := callbackServer.Shutdown(ctx); err != nil {
		zap.L().Error("回调服务器关闭失败", zap.Error(err))
	}

	// 7.2 关闭传输层（停止收新连接 + 断开所有活跃连接）
	server.Shutdown()

	// 7.3 关闭数据存储
	if redisStore != nil {
		redisStore.Close()
	}
	if mysqlStore != nil {
		mysqlStore.Close()
	}

	// 7.4 刷新日志
	logger.Sync()
	zap.L().Info("服务器已关闭")
}
