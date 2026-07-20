package store

import (
	"math"
	"sync"
	"testing"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

func settlementRequest(runID string, outcome protocolpb.BattleOutcome) *protocolpb.CombatResultReq {
	return &protocolpb.CombatResultReq{
		RunId:        runID,
		DungeonLevel: 3,
		Score:        321,
		Kills:        4,
		DurationMs:   42_000,
		StyleId:      3,
		Outcome:      outcome,
		PlayerLevel:  2,
	}
}

func TestMemoryDevelopmentStoreRejectsArchiveTotalOverflow(t *testing.T) {
	for _, test := range []struct {
		name    string
		archive *protocolpb.PlayerArchive
	}{
		{name: "kills", archive: &protocolpb.PlayerArchive{SchemaVersion: 1, TotalKills: math.MaxInt64}},
		{name: "games", archive: &protocolpb.PlayerArchive{SchemaVersion: 1, TotalGames: math.MaxInt64}},
	} {
		t.Run(test.name, func(t *testing.T) {
			developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
			data, err := proto.Marshal(test.archive)
			if err != nil {
				t.Fatal(err)
			}
			if err := developmentStore.SaveArchive(&Archive{PlayerID: 15, Data: data}); err != nil {
				t.Fatal(err)
			}
			if _, err := developmentStore.Settle(15, settlementRequest("overflow-"+test.name, protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY)); err == nil {
				t.Fatal("Settle() error = nil, want total overflow")
			}
		})
	}
}

func TestMemoryDevelopmentStoreSettlesDuplicateOnceWithStableArchive(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	initialArchive := &protocolpb.PlayerArchive{
		SchemaVersion:         1,
		Gold:                  7,
		Exp:                   11,
		BestScore:             17,
		TotalKills:            2,
		TotalGames:            1,
		HighestClearedDungeon: 1,
		TalentPoints:          5,
		UnlockedStyles:        []int32{1, 3},
		LastStyleId:           3,
	}
	encoded, err := proto.Marshal(initialArchive)
	if err != nil {
		t.Fatalf("marshal archive: %v", err)
	}
	if err := developmentStore.SaveArchive(&Archive{PlayerID: 7, Data: encoded}); err != nil {
		t.Fatalf("SaveArchive() error = %v", err)
	}

	first, err := developmentStore.Settle(7, settlementRequest("stable-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY))
	if err != nil {
		t.Fatalf("first Settle() error = %v", err)
	}
	if first.Duplicate || first.RewardGold != 20 || first.RewardExp != 40 || first.RunId != "stable-run" {
		t.Fatalf("first response = %#v, want first settlement rewards", first)
	}
	if first.Archive == nil || first.Archive.TalentPoints != 5 || !proto.Equal(first.Archive, &protocolpb.PlayerArchive{
		SchemaVersion:         1,
		Gold:                  27,
		Exp:                   51,
		BestScore:             321,
		TotalKills:            6,
		TotalGames:            2,
		HighestClearedDungeon: 3,
		TalentPoints:          5,
		UnlockedStyles:        []int32{1, 3},
		LastStyleId:           3,
	}) {
		t.Fatalf("settled archive = %#v, want complete merged snapshot", first.Archive)
	}

	duplicate, err := developmentStore.Settle(7, settlementRequest("stable-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY))
	if err != nil {
		t.Fatalf("duplicate Settle() error = %v", err)
	}
	if !duplicate.Duplicate {
		t.Fatal("duplicate response has Duplicate = false, want true")
	}
	if duplicate.RunId != "stable-run" {
		t.Fatalf("duplicate run ID = %q, want stable-run", duplicate.RunId)
	}
	duplicateAgain, err := developmentStore.Settle(7, settlementRequest("stable-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY))
	if err != nil {
		t.Fatalf("second duplicate Settle() error = %v", err)
	}
	if !proto.Equal(duplicate, duplicateAgain) {
		t.Fatalf("duplicate responses differ: first=%v second=%v", duplicate, duplicateAgain)
	}
	if duplicateAgain.RunId != "stable-run" {
		t.Fatalf("stored duplicate run ID = %q, want stable-run", duplicateAgain.RunId)
	}
	firstBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(duplicate)
	if err != nil {
		t.Fatalf("marshal first duplicate: %v", err)
	}
	secondBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(duplicateAgain)
	if err != nil {
		t.Fatalf("marshal second duplicate: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("duplicate protobuf bytes differ: first=%x second=%x", firstBytes, secondBytes)
	}
}

func TestMemoryDevelopmentStoreDuplicateBackfillsRunIDFromLegacySnapshot(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	req := settlementRequest("legacy-memory-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY)
	legacy := &protocolpb.CombatResultResp{
		Success: true, RewardGold: 20, RewardExp: 40, BestScore: 321,
		Archive: &protocolpb.PlayerArchive{SchemaVersion: 1, Gold: 20, Exp: 40},
	}
	legacyBytes, err := proto.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy response: %v", err)
	}
	developmentStore.settlements[memorySettlementKey{playerID: 7, runID: req.RunId}] = legacyBytes

	response, err := developmentStore.Settle(7, req)
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if !response.Duplicate {
		t.Fatal("legacy duplicate response has Duplicate = false, want true")
	}
	if response.RunId != req.RunId {
		t.Fatalf("legacy duplicate run ID = %q, want current request %q", response.RunId, req.RunId)
	}
}

func TestMemoryDevelopmentStoreDefeatDoesNotAdvanceHighestClearedDungeon(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	response, err := developmentStore.Settle(9, settlementRequest("defeat-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_DEFEAT))
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if response.Archive.HighestClearedDungeon != 0 {
		t.Fatalf("defeat highest cleared = %d, want 0", response.Archive.HighestClearedDungeon)
	}
}

func TestMemoryDevelopmentStoreConvergesConcurrentDuplicateSettlement(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 5, ExpPerKill: 10})
	const workers = 32
	responses := make([]*protocolpb.CombatResultResp, workers)
	errs := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			defer group.Done()
			responses[index], errs[index] = developmentStore.Settle(11, settlementRequest("parallel-run", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY))
		}(index)
	}
	group.Wait()

	firstCount := 0
	for index, response := range responses {
		if errs[index] != nil {
			t.Fatalf("Settle(%d) error = %v", index, errs[index])
		}
		if !response.Duplicate {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Fatalf("non-duplicate responses = %d, want 1", firstCount)
	}
}

func TestMemoryDevelopmentStoreUsesInjectedSettlementRewardPolicy(t *testing.T) {
	developmentStore := NewMemoryDevelopmentStoreWithSettlementPolicy(CombatRewardPolicy{GoldPerKill: 7, ExpPerKill: 13})
	response, err := developmentStore.Settle(13, settlementRequest("configured-rewards", protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY))
	if err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	if response.RewardGold != 28 || response.RewardExp != 52 {
		t.Fatalf("configured rewards = gold %d exp %d, want gold 28 exp 52", response.RewardGold, response.RewardExp)
	}
}
