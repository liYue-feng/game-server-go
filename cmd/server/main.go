// 游戏服务器入口
//
// 启动流程：
//  1. 加载配置文件 (configs/config.yaml)
//  2. 初始化日志系统
//  3. 初始化数据存储 (MySQL + Redis)
//  4. 初始化业务模块 (登录、游戏、排行榜、支付、GM)
//  5. 注册中间件（认证、限流）
//  6. 注册消息路由
//  7. 启动 WebSocket 网关服务器 + HTTP 回调服务器
//
// 优雅关闭：
//  监听系统信号 (SIGINT/SIGTERM)，收到信号后：
//  1. 停止接收新连接
//  2. 关闭 HTTP 回调服务器
//  3. 关闭数据存储连接
//  4. 刷新日志缓冲
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
	"game-server/internal/gateway"
	"game-server/internal/game"
	"game-server/internal/gm"
	"game-server/internal/login"
	"game-server/internal/middleware"
	"game-server/internal/payment"
	"game-server/internal/protocol"
	"game-server/internal/rank"
	"game-server/internal/store"
	"game-server/pkg/logger"

	"go.uber.org/zap"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// ========== 1. 加载配置 ==========
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// ========== 2. 初始化日志 ==========
	logger.Init(
		cfg.Log.Level,
		cfg.Log.Filename,
		cfg.Log.MaxSize,
		cfg.Log.MaxBackups,
		cfg.Log.MaxAge,
	)
	defer logger.Sync() // 确保程序退出前所有日志都写入磁盘

	zap.L().Info("========== 游戏服务器启动 ==========")
	zap.L().Info("配置加载成功", zap.String("path", *configPath))

	// ========== 3. 初始化数据存储 ==========
	// MySQL：玩家账号、存档、分数记录、订单的持久化存储
	mysqlStore, err := store.NewMySQLStore(&cfg.MySQL)
	if err != nil {
		zap.L().Warn("MySQL 初始化失败，将降级运行", zap.Error(err))
	}
	defer func() {
		if mysqlStore != nil {
			mysqlStore.Close()
		}
	}()

	// Redis：会话缓存、排行榜、限流
	redisStore, err := store.NewRedisStore(&cfg.Redis)
	if err != nil {
		zap.L().Warn("Redis 初始化失败，将降级运行", zap.Error(err))
		// Redis 连接失败不退出，部分功能降级运行
	}

	// ========== 4. 初始化业务模块 ==========
	// 微信 API 客户端
	wechatClient := login.NewWechatClient(&cfg.Wechat)

	// 各模块 Handler
	loginHandler := login.NewHandler(mysqlStore, redisStore, wechatClient)
	gameHandler := game.NewHandler(mysqlStore, redisStore)
	rankHandler := rank.NewHandler(redisStore, mysqlStore)
	paymentHandler := payment.NewHandler(mysqlStore, redisStore, &cfg.Wechat)
		combatHandler := combat.NewHandler(mysqlStore, redisStore)

	// ========== 5. 注册消息路由 ==========
	router := gateway.NewRouter()

	// 注册中间件（按顺序执行：先认证，后限流）
	// 注意：登录和心跳请求在 Router 内部已跳过认证检查
	if redisStore != nil {
		router.Use(middleware.AuthMiddleware(redisStore))
		router.Use(middleware.RateLimitMiddleware(redisStore, 100, time.Second))
	}

	// 登录模块
	router.Register(protocol.MsgID_LoginReq, loginHandler.HandleLogin)
	router.Register(protocol.MsgID_HeartbeatReq, loginHandler.HandleHeartbeat)

	// 游戏存档模块
	router.Register(protocol.MsgID_SaveArchiveReq, gameHandler.HandleSaveArchive)
	router.Register(protocol.MsgID_LoadArchiveReq, gameHandler.HandleLoadArchive)

	// 排行榜模块
	router.Register(protocol.MsgID_GetRankReq, rankHandler.HandleGetRank)
	router.Register(protocol.MsgID_SubmitScoreReq, rankHandler.HandleSubmitScore)

	// 支付模块
	router.Register(protocol.MsgID_CreateOrderReq, paymentHandler.HandleCreateOrder)

	// 战斗模块
		router.Register(protocol.MsgID_CombatResultReq, combatHandler.HandleCombatResult)
		router.Register(protocol.MsgID_GetEnemyConfigsReq, combatHandler.HandleGetEnemyConfigs)
		router.Register(protocol.MsgID_GetDungeonConfigReq, combatHandler.HandleGetDungeonConfig)
		router.Register(protocol.MsgID_GetStyleConfigsReq, combatHandler.HandleGetStyleConfigs)
		router.Register(protocol.MsgID_UnlockStyleReq, combatHandler.HandleUnlockStyle)
		router.Register(protocol.MsgID_GetPlayerStatsReq, combatHandler.HandleGetPlayerStats)
		router.Register(protocol.MsgID_UpdatePlayerStatsReq, combatHandler.HandleUpdatePlayerStats)

		// GM 指令模块（管理员专用）
	// TODO: 从配置文件读取管理员 UID 列表
	gmHandler := gm.NewHandler(mysqlStore, redisStore, nil, []int64{})
	router.Register(protocol.MsgID_GMCommandReq, gmHandler.HandleCommand)

	// ========== 6. 启动服务器 ==========
	server := gateway.NewServer(router)

	// 在独立 goroutine 中启动 WebSocket 服务器
	go func() {
		if err := server.Start(cfg.Server.Addr()); err != nil {
			zap.L().Fatal("服务器启动失败", zap.Error(err))
		}
	}()

	// 启动 HTTP 回调服务器（微信支付回调等）
	// 为什么需要单独的 HTTP 服务器？
	//   - 微信支付回调是 HTTP POST 请求，不是 WebSocket
	//   - 与游戏 WebSocket 服务解耦，互不影响
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

	callbackServer := &http.Server{
		Addr:    ":8081", // 回调服务使用独立端口
		Handler: callbackMux,
	}

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

	// 7.1 关闭 HTTP 回调服务器（5秒超时）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := callbackServer.Shutdown(ctx); err != nil {
		zap.L().Error("回调服务器关闭失败", zap.Error(err))
	}

	// 7.2 关闭所有 WebSocket 连接
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
