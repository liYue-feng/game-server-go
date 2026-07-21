package combat

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"
)

func validCombatResult() *protocolpb.CombatResultReq {
	return &protocolpb.CombatResultReq{
		RunId:        "run-1",
		DungeonLevel: 2,
		Score:        100,
		Kills:        3,
		SurvivalTime: 30,
		StyleId:      1,
		Outcome:      protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY,
		PlayerLevel:  1,
	}
}

func TestDevelopmentHandlerReportsMonotonicSettlementLevel(t *testing.T) {
	memory := store.NewMemoryDevelopmentStoreWithSettlementPolicy(store.CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	service := NewSettlementService(memory, DefaultCombatConfig())
	handler := NewDevelopmentHandler(service, memory, memory)
	ctx := session.WithSession(context.Background(), session.New(nil))
	session.FromContext(ctx).Bind(19)

	high := validCombatResult()
	high.RunId = "level-high"
	high.PlayerLevel = 2
	if _, err := handler.CombatResult(ctx, high); err != nil {
		t.Fatal(err)
	}
	low := validCombatResult()
	low.RunId = "level-low"
	low.PlayerLevel = 1
	if _, err := handler.CombatResult(ctx, low); err != nil {
		t.Fatal(err)
	}
	stats, err := handler.GetPlayerStats(ctx, &protocolpb.GetPlayerStatsReq{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Level != 2 {
		t.Fatalf("development level = %d, want 2", stats.Level)
	}
}

func TestSettlementServiceRejectsInvalidCombatResult(t *testing.T) {
	service := NewSettlementService(store.NewMemoryDevelopmentStoreWithSettlementPolicy(store.CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10}), DefaultCombatConfig())
	tests := []struct {
		name string
		edit func(*protocolpb.CombatResultReq)
	}{
		{name: "missing run ID", edit: func(req *protocolpb.CombatResultReq) { req.RunId = "" }},
		{name: "blank run ID", edit: func(req *protocolpb.CombatResultReq) { req.RunId = " \t" }},
		{name: "leading run ID whitespace", edit: func(req *protocolpb.CombatResultReq) { req.RunId = " run-1" }},
		{name: "trailing run ID whitespace", edit: func(req *protocolpb.CombatResultReq) { req.RunId = "run-1 " }},
		{name: "oversized run ID", edit: func(req *protocolpb.CombatResultReq) { req.RunId = string(make([]byte, 129)) }},
		{name: "unspecified outcome", edit: func(req *protocolpb.CombatResultReq) {
			req.Outcome = protocolpb.BattleOutcome_BATTLE_OUTCOME_UNSPECIFIED
		}},
		{name: "unknown outcome", edit: func(req *protocolpb.CombatResultReq) { req.Outcome = protocolpb.BattleOutcome(99) }},
		{name: "invalid player level", edit: func(req *protocolpb.CombatResultReq) { req.PlayerLevel = 0 }},
		{name: "over max player level", edit: func(req *protocolpb.CombatResultReq) { req.PlayerLevel = 101 }},
		{name: "negative score", edit: func(req *protocolpb.CombatResultReq) { req.Score = -1 }},
		{name: "over max score", edit: func(req *protocolpb.CombatResultReq) { req.Score = 20_001 }},
		{name: "negative kills", edit: func(req *protocolpb.CombatResultReq) { req.Kills = -1 }},
		{name: "over max kills", edit: func(req *protocolpb.CombatResultReq) { req.Kills = 10_000 }},
		{name: "negative survival time", edit: func(req *protocolpb.CombatResultReq) { req.SurvivalTime = -1 }},
		{name: "over max survival time", edit: func(req *protocolpb.CombatResultReq) { req.SurvivalTime = 7_200.1 }},
		{name: "NaN survival time", edit: func(req *protocolpb.CombatResultReq) { req.SurvivalTime = math.NaN() }},
		{name: "positive infinite survival time", edit: func(req *protocolpb.CombatResultReq) { req.SurvivalTime = math.Inf(1) }},
		{name: "negative infinite survival time", edit: func(req *protocolpb.CombatResultReq) { req.SurvivalTime = math.Inf(-1) }},
		{name: "invalid dungeon", edit: func(req *protocolpb.CombatResultReq) { req.DungeonLevel = 0 }},
		{name: "over max dungeon", edit: func(req *protocolpb.CombatResultReq) { req.DungeonLevel = 101 }},
		{name: "invalid style", edit: func(req *protocolpb.CombatResultReq) { req.StyleId = 0 }},
		{name: "over max style", edit: func(req *protocolpb.CombatResultReq) { req.StyleId = 6 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validCombatResult()
			test.edit(req)
			if _, err := service.Settle(7, req); err == nil {
				t.Fatal("Settle() error = nil, want validation error")
			}
		})
	}
}

func TestSettlementServiceAcceptsSurvivalTimeBoundariesAndFraction(t *testing.T) {
	for _, survivalTime := range []float64{0, 12.5, 7200} {
		t.Run(fmt.Sprintf("%g", survivalTime), func(t *testing.T) {
			service := NewSettlementService(store.NewMemoryDevelopmentStoreWithSettlementPolicy(store.CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10}), DefaultCombatConfig())
			req := validCombatResult()
			req.SurvivalTime = survivalTime
			if _, err := service.Settle(7, req); err != nil {
				t.Fatalf("Settle() error = %v, want survival_time=%g accepted", err, survivalTime)
			}
		})
	}
}

func TestSettlementServiceAcceptsBoundaryRunID(t *testing.T) {
	service := NewSettlementService(store.NewMemoryDevelopmentStoreWithSettlementPolicy(store.CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10}), DefaultCombatConfig())
	req := validCombatResult()
	req.RunId = strings.Repeat("r", 128)
	if _, err := service.Settle(7, req); err != nil {
		t.Fatalf("Settle() error = %v, want boundary run ID accepted", err)
	}
}
