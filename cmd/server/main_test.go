package main

import (
	"errors"
	"strings"
	"testing"

	"game-server/internal/config"
	"game-server/internal/protocol"
)

func TestNewRuntimeDevelopmentRegistersOnlyA4Messages(t *testing.T) {
	cfg := &config.Config{
		Development: config.DevelopmentConfig{
			Enabled:      true,
			LoginEnabled: true,
		},
	}

	runtime, err := newRuntime(cfg)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.close(); err != nil {
			t.Errorf("runtime.close() error = %v", err)
		}
	})

	if !runtime.development {
		t.Fatal("runtime.development = false, want true")
	}
	if runtime.paymentHandler != nil {
		t.Fatal("runtime.paymentHandler is non-nil in development mode")
	}

	present := []uint16{
		protocol.MsgID_LoginReq,
		protocol.MsgID_HeartbeatReq,
		protocol.MsgID_SaveArchiveReq,
		protocol.MsgID_LoadArchiveReq,
	}
	for _, msgID := range present {
		if !runtime.router.HasHandler(msgID) {
			t.Errorf("development router missing message %d", msgID)
		}
	}

	absent := []uint16{
		protocol.MsgID_GetRankReq,
		protocol.MsgID_SubmitScoreReq,
		protocol.MsgID_CreateOrderReq,
		protocol.MsgID_CombatResultReq,
		protocol.MsgID_GMCommandReq,
	}
	for _, msgID := range absent {
		if runtime.router.HasHandler(msgID) {
			t.Errorf("development router unexpectedly registered message %d", msgID)
		}
	}

	if err := runtime.close(); err != nil {
		t.Fatalf("first runtime.close() error = %v", err)
	}
	if err := runtime.close(); err != nil {
		t.Fatalf("second runtime.close() error = %v", err)
	}
}

func TestNewRuntimeProductionFailsBeforeServingWhenMySQLIsUnavailable(t *testing.T) {
	cfg := &config.Config{
		MySQL: config.MySQLConfig{
			Host:   "127.0.0.1",
			Port:   1,
			User:   "test",
			DBName: "test",
		},
	}

	runtime, err := newRuntime(cfg)
	if err == nil {
		t.Fatal("newRuntime() error = nil, want MySQL initialization failure")
	}
	if runtime != nil {
		t.Fatal("newRuntime() returned a runtime after MySQL initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize mysql") {
		t.Fatalf("newRuntime() error = %q, want initialize mysql context", err)
	}
}

func TestNewRuntimeCloseAttemptsAllResourcesOnceAndReturnsFirstError(t *testing.T) {
	redisErr := errors.New("redis close failed")
	mysqlErr := errors.New("mysql close failed")
	redisCalls := 0
	mysqlCalls := 0

	closeRuntime := newRuntimeClose(
		func() error {
			redisCalls++
			return redisErr
		},
		func() error {
			mysqlCalls++
			return mysqlErr
		},
	)

	if err := closeRuntime(); !errors.Is(err, redisErr) {
		t.Fatalf("first close error = %v, want %v", err, redisErr)
	}
	if err := closeRuntime(); !errors.Is(err, redisErr) {
		t.Fatalf("second close error = %v, want %v", err, redisErr)
	}
	if redisCalls != 1 || mysqlCalls != 1 {
		t.Fatalf("close calls = redis %d, mysql %d; want 1 each", redisCalls, mysqlCalls)
	}
}
