// Game server entry point.
//
// Startup composes either an isolated in-memory development runtime or the
// complete production runtime before opening any network listener.
package main

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	maxPaymentCallbackBodySize int64 = 1 << 20
	callbackReadHeaderTimeout        = 5 * time.Second
	callbackReadTimeout              = 10 * time.Second
	callbackWriteTimeout             = 10 * time.Second
	callbackIdleTimeout              = 60 * time.Second
)

type runtime struct {
	kernel         *kernel.Kernel
	server         *transport.Server
	close          func() error
	development    bool
	paymentHandler *payment.Handler
}

var openMySQLStore = func(cfg *config.MySQLConfig) (*store.MySQLStore, func() error, error) {
	mysqlStore, err := store.NewMySQLStore(cfg)
	if err != nil {
		return nil, nil, err
	}
	return mysqlStore, mysqlStore.Close, nil
}

var openRedisStore = func(cfg *config.RedisConfig) (*store.RedisStore, func() error, error) {
	redisStore, err := store.NewRedisStore(cfg)
	if err != nil {
		return nil, nil, err
	}
	return redisStore, redisStore.Close, nil
}

func newRuntime(cfg *config.Config) (*runtime, error) {
	hookChain := pipeline.New()
	k := kernel.New(hookChain)

	if cfg.Development.Enabled {
		combatConfig := combat.DefaultCombatConfig()
		memoryStore := store.NewMemoryDevelopmentStoreWithSettlementPolicy(store.CombatRewardPolicy{
			GoldPerKill: combatConfig.GoldPerKill,
			ExpPerKill:  combatConfig.ExpPerKill,
		})
		loginService := login.NewLoginService(
			memoryStore,
			memoryStore,
			login.NewDevelopmentCodeExchanger(cfg.Development.LoginEnabled),
			login.GenerateToken,
		)
		loginHandler := login.NewHandlerWithService(loginService)
		gameHandler := game.NewHandlerWithService(game.NewArchiveService(memoryStore))
		combatHandler := combat.NewDevelopmentHandler(combat.NewSettlementService(memoryStore, combatConfig), memoryStore, memoryStore)

		registerOnlineSessionHandlers(k, loginHandler, gameHandler)
		k.RegisterRoute(protocol.MsgID_CombatResultReq, combatHandler.CombatResult)
		k.RegisterRoute(protocol.MsgID_GetPlayerStatsReq, combatHandler.GetPlayerStats)
		hookChain.AddBefore(hooks.Auth(memoryStore, k))

		server := transport.NewServer(k)
		return &runtime{
			kernel:      k,
			server:      server,
			close:       newRuntimeClose(),
			development: true,
		}, nil
	}

	mysqlStore, closeMySQL, err := openMySQLStore(&cfg.MySQL)
	if err != nil {
		return nil, fmt.Errorf("initialize mysql: %w", err)
	}

	redisStore, closeRedis, err := openRedisStore(&cfg.Redis)
	if err != nil {
		if closeMySQL != nil {
			_ = closeMySQL()
		}
		return nil, fmt.Errorf("initialize redis: %w", err)
	}

	wechatClient := login.NewWechatClient(&cfg.Wechat)
	loginHandler := login.NewHandlerWithService(login.NewLoginService(
		mysqlStore,
		redisStore,
		login.NewWechatCodeExchanger(wechatClient),
		login.GenerateToken,
	))
	gameHandler := game.NewHandlerWithService(game.NewArchiveService(mysqlStore))
	rankHandler := rank.NewHandler(redisStore, mysqlStore)
	paymentHandler := payment.NewHandler(mysqlStore, redisStore, &cfg.Wechat)
	combatHandler := combat.NewHandler(mysqlStore, redisStore)

	registerOnlineSessionHandlers(k, loginHandler, gameHandler)
	k.RegisterRoute(protocol.MsgID_GetRankReq, rankHandler.GetRank)
	k.RegisterRoute(protocol.MsgID_SubmitScoreReq, rankHandler.SubmitScore)
	k.RegisterRoute(protocol.MsgID_CreateOrderReq, paymentHandler.CreateOrder)
	k.RegisterRoute(protocol.MsgID_CombatResultReq, combatHandler.CombatResult)
	k.RegisterRoute(protocol.MsgID_GetEnemyConfigsReq, combatHandler.GetEnemyConfigs)
	k.RegisterRoute(protocol.MsgID_GetDungeonConfigReq, combatHandler.GetDungeonConfig)
	k.RegisterRoute(protocol.MsgID_GetStyleConfigsReq, combatHandler.GetStyleConfigs)
	k.RegisterRoute(protocol.MsgID_UnlockStyleReq, combatHandler.UnlockStyle)
	k.RegisterRoute(protocol.MsgID_GetPlayerStatsReq, combatHandler.GetPlayerStats)
	k.RegisterRoute(protocol.MsgID_UpdatePlayerStatsReq, combatHandler.UpdatePlayerStats)

	server := transport.NewServer(k)
	gmHandler := gm.NewHandler(mysqlStore, redisStore, server.Hub(), cfg.GM.AdminUIDs)
	k.RegisterRoute(protocol.MsgID_GMCommandReq, gmHandler.Command)

	hookChain.AddBefore(hooks.Auth(redisStore, k))
	hookChain.AddBefore(hooks.RateLimit(redisStore, k, 100, time.Second))

	return &runtime{
		kernel:         k,
		server:         server,
		close:          newRuntimeClose(closeRedis, closeMySQL),
		paymentHandler: paymentHandler,
	}, nil
}

func registerOnlineSessionHandlers(k *kernel.Kernel, loginHandler *login.Handler, gameHandler *game.Handler) {
	k.RegisterRoute(protocol.MsgID_LoginReq, loginHandler.Login, kernel.AuthFree())
	k.RegisterRoute(protocol.MsgID_HeartbeatReq, loginHandler.Heartbeat, kernel.AuthFree())
	k.RegisterRoute(protocol.MsgID_SaveArchiveReq, gameHandler.SaveArchive)
	k.RegisterRoute(protocol.MsgID_LoadArchiveReq, gameHandler.LoadArchive)
}

func newRuntimeClose(closers ...func() error) func() error {
	var once sync.Once
	var firstErr error

	return func() error {
		once.Do(func() {
			for _, closeResource := range closers {
				if closeResource == nil {
					continue
				}
				if err := closeResource(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		})
		return firstErr
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

	logger.Init(cfg.Log.Level, cfg.Log.Filename, cfg.Log.MaxSize, cfg.Log.MaxBackups, cfg.Log.MaxAge)

	appRuntime, err := newRuntime(cfg)
	if err != nil {
		zap.L().Error("initialize server runtime", zap.Error(err))
		logger.Close()
		os.Exit(1)
	}

	go func() {
		if err := appRuntime.server.Start(cfg.Server.Addr()); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("start WebSocket server", zap.Error(err))
		}
	}()

	callbackServer := newPaymentCallbackServer(appRuntime)
	if callbackServer != nil {
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
	signal.Stop(quit)
	zap.L().Info("shutdown signal received", zap.String("signal", sig.String()))

	if callbackServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := callbackServer.Shutdown(ctx); err != nil {
			zap.L().Error("shutdown payment callback server", zap.Error(err))
		}
		cancel()
	}

	appRuntime.server.Shutdown()
	if err := appRuntime.close(); err != nil {
		zap.L().Error("close runtime resources", zap.Error(err))
	}
	zap.L().Info("game server stopped")
	logger.Close()
}

func newPaymentCallbackServer(appRuntime *runtime) *http.Server {
	if appRuntime.development || appRuntime.paymentHandler == nil {
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle("/pay/callback", newPaymentCallbackHandler(appRuntime.paymentHandler.HandlePayCallback))

	return &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: callbackReadHeaderTimeout,
		ReadTimeout:       callbackReadTimeout,
		WriteTimeout:      callbackWriteTimeout,
		IdleTimeout:       callbackIdleTimeout,
	}
}

func newPaymentCallbackHandler(handle func([]byte) (*payment.CallbackResp, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, maxPaymentCallbackBodySize)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				http.Error(w, "payment callback body too large", http.StatusRequestEntityTooLarge)
				return
			}
			zap.L().Error("read payment callback body", zap.Error(err))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		response, err := handle(body)
		if err != nil {
			zap.L().Error("handle payment callback", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		payload, err := json.Marshal(response)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(payload)
	})
}
