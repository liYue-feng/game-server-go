// Game server entry point.
//
// Startup loads configuration and logging first, composes either the isolated
// development runtime or the complete production runtime, then starts network
// listeners. Shutdown closes the optional payment callback, WebSocket
// connections, and runtime resources in that order.
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
	"sync"
	"syscall"
	"time"

	"game-server/internal/combat"
	"game-server/internal/config"
	"game-server/internal/game"
	"game-server/internal/gateway"
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

type runtime struct {
	router         *gateway.Router
	close          func() error
	development    bool
	paymentHandler *payment.Handler
}

func newRuntime(cfg *config.Config) (*runtime, error) {
	router := gateway.NewRouter()

	if cfg.Development.Enabled {
		memoryStore := store.NewMemoryDevelopmentStore()
		loginService := login.NewLoginService(
			memoryStore,
			memoryStore,
			login.NewDevelopmentCodeExchanger(cfg.Development.LoginEnabled),
			login.GenerateToken,
		)
		loginHandler := login.NewHandler(loginService)
		archiveService := game.NewArchiveService(memoryStore)
		gameHandler := game.NewHandler(archiveService)

		registerA4Routes(router, loginHandler, gameHandler)

		return &runtime{
			router:      router,
			close:       func() error { return nil },
			development: true,
		}, nil
	}

	mysqlStore, err := store.NewMySQLStore(&cfg.MySQL)
	if err != nil {
		return nil, fmt.Errorf("initialize mysql: %w", err)
	}

	redisStore, err := store.NewRedisStore(&cfg.Redis)
	if err != nil {
		_ = mysqlStore.Close()
		return nil, fmt.Errorf("initialize redis: %w", err)
	}

	wechatClient := login.NewWechatClient(&cfg.Wechat)
	loginService := login.NewLoginService(
		mysqlStore,
		redisStore,
		login.NewWechatCodeExchanger(wechatClient),
		login.GenerateToken,
	)
	loginHandler := login.NewHandler(loginService)
	archiveService := game.NewArchiveService(mysqlStore)
	gameHandler := game.NewHandler(archiveService)
	rankHandler := rank.NewHandler(redisStore, mysqlStore)
	paymentHandler := payment.NewHandler(mysqlStore, redisStore, &cfg.Wechat)
	combatHandler := combat.NewHandler(mysqlStore, redisStore)
	gmHandler := gm.NewHandler(mysqlStore, redisStore, nil, []int64{})

	router.Use(middleware.AuthMiddleware(redisStore))
	router.Use(middleware.RateLimitMiddleware(redisStore, 100, time.Second))
	registerA4Routes(router, loginHandler, gameHandler)
	router.Register(protocol.MsgID_GetRankReq, rankHandler.HandleGetRank)
	router.Register(protocol.MsgID_SubmitScoreReq, rankHandler.HandleSubmitScore)
	router.Register(protocol.MsgID_CreateOrderReq, paymentHandler.HandleCreateOrder)
	router.Register(protocol.MsgID_CombatResultReq, combatHandler.HandleCombatResult)
	router.Register(protocol.MsgID_GetEnemyConfigsReq, combatHandler.HandleGetEnemyConfigs)
	router.Register(protocol.MsgID_GetDungeonConfigReq, combatHandler.HandleGetDungeonConfig)
	router.Register(protocol.MsgID_GetStyleConfigsReq, combatHandler.HandleGetStyleConfigs)
	router.Register(protocol.MsgID_UnlockStyleReq, combatHandler.HandleUnlockStyle)
	router.Register(protocol.MsgID_GetPlayerStatsReq, combatHandler.HandleGetPlayerStats)
	router.Register(protocol.MsgID_UpdatePlayerStatsReq, combatHandler.HandleUpdatePlayerStats)
	router.Register(protocol.MsgID_GMCommandReq, gmHandler.HandleCommand)

	return &runtime{
		router:         router,
		close:          newRuntimeClose(redisStore.Close, mysqlStore.Close),
		paymentHandler: paymentHandler,
	}, nil
}

func registerA4Routes(router *gateway.Router, loginHandler *login.Handler, gameHandler *game.Handler) {
	router.Register(protocol.MsgID_LoginReq, loginHandler.HandleLogin)
	router.Register(protocol.MsgID_HeartbeatReq, loginHandler.HandleHeartbeat)
	router.Register(protocol.MsgID_SaveArchiveReq, gameHandler.HandleSaveArchive)
	router.Register(protocol.MsgID_LoadArchiveReq, gameHandler.HandleLoadArchive)
}

func newRuntimeClose(closers ...func() error) func() error {
	var once sync.Once
	var closeErr error

	return func() error {
		once.Do(func() {
			for _, closeResource := range closers {
				if err := closeResource(); err != nil && closeErr == nil {
					closeErr = err
				}
			}
		})
		return closeErr
	}
}

func main() {
	configPath := flag.String("config", "configs/config.yaml", "configuration file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("load configuration: %v\n", err)
		os.Exit(1)
	}

	logger.Init(
		cfg.Log.Level,
		cfg.Log.Filename,
		cfg.Log.MaxSize,
		cfg.Log.MaxBackups,
		cfg.Log.MaxAge,
	)
	defer logger.Close()

	appRuntime, err := newRuntime(cfg)
	if err != nil {
		zap.L().Error("initialize server runtime", zap.Error(err))
		logger.Close()
		os.Exit(1)
	}

	server := gateway.NewServer(appRuntime.router)
	go func() {
		if err := server.Start(cfg.Server.Addr()); err != nil {
			zap.L().Fatal("start WebSocket server", zap.Error(err))
		}
	}()

	var callbackServer *http.Server
	if !appRuntime.development {
		callbackMux := http.NewServeMux()
		callbackMux.HandleFunc("/pay/callback", func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			body, err := io.ReadAll(r.Body)
			if err != nil {
				zap.L().Error("read payment callback body", zap.Error(err))
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			resp, err := appRuntime.paymentHandler.HandlePayCallback(body)
			if err != nil {
				zap.L().Error("handle payment callback", zap.Error(err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			jsonResp, _ := json.Marshal(resp)
			_, _ = w.Write(jsonResp)
		})

		callbackServer = &http.Server{
			Addr:    ":8081",
			Handler: callbackMux,
		}
		go func() {
			zap.L().Info("payment callback server started", zap.String("addr", callbackServer.Addr))
			if err := callbackServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				zap.L().Fatal("start payment callback server", zap.Error(err))
			}
		}()
	}
	zap.L().Info("game server started",
		zap.String("ws_addr", cfg.Server.Addr()),
		zap.Bool("development", appRuntime.development),
		zap.String("name", cfg.Server.Name),
	)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	zap.L().Info("shutdown signal received", zap.String("signal", sig.String()))

	if callbackServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := callbackServer.Shutdown(ctx); err != nil {
			zap.L().Error("shutdown payment callback server", zap.Error(err))
		}
		cancel()
	}

	server.Shutdown()
	if err := appRuntime.close(); err != nil {
		zap.L().Error("close runtime resources", zap.Error(err))
	}
}
