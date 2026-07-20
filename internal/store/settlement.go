package store

import (
	"errors"
	"fmt"
	"math"

	"game-server/internal/protocolpb"
)

var ErrMalformedSettlementArchive = errors.New("store: malformed settlement archive")

func validateCombatRewardPolicy(policy CombatRewardPolicy) error {
	if policy.GoldPerKill < 0 || policy.ExpPerKill < 0 {
		return fmt.Errorf("store: invalid combat reward policy")
	}
	return nil
}

func settleArchive(archive *protocolpb.PlayerArchive, req *protocolpb.CombatResultReq, policy CombatRewardPolicy) (*protocolpb.CombatResultResp, error) {
	if archive == nil {
		archive = &protocolpb.PlayerArchive{SchemaVersion: 1}
	}
	gold := int64(req.Kills) * int64(policy.GoldPerKill)
	exp := int64(req.Kills) * int64(policy.ExpPerKill)
	if gold > math.MaxInt32 || exp > math.MaxInt32 {
		return nil, fmt.Errorf("store: settlement reward overflow")
	}
	if int64(archive.Gold)+gold > math.MaxInt32 || int64(archive.Exp)+exp > math.MaxInt32 {
		return nil, fmt.Errorf("store: archive reward overflow")
	}

	if int64(req.Kills) > math.MaxInt64-archive.TotalKills || archive.TotalGames == math.MaxInt64 {
		return nil, fmt.Errorf("store: archive totals overflow")
	}
	archive.Gold += int32(gold)
	archive.Exp += int32(exp)
	archive.TotalKills += int64(req.Kills)
	archive.TotalGames++
	if req.Score > archive.BestScore {
		archive.BestScore = req.Score
	}
	if req.Outcome == protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY && req.DungeonLevel > archive.HighestClearedDungeon {
		archive.HighestClearedDungeon = req.DungeonLevel
	}
	archive.LastStyleId = req.StyleId

	return &protocolpb.CombatResultResp{
		Success:    true,
		RewardGold: int32(gold),
		RewardExp:  int32(exp),
		BestScore:  archive.BestScore,
		Archive:    archive,
	}, nil
}
