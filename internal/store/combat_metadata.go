package store

import (
	"encoding/json"
	"fmt"

	"game-server/internal/protocolpb"
)

// CombatScoreMetadataJSON returns the persisted analytics metadata for one settlement.
func CombatScoreMetadataJSON(req *protocolpb.CombatResultReq) (string, error) {
	if req == nil {
		return "", fmt.Errorf("combat score metadata: nil request")
	}
	metadata, err := json.Marshal(struct {
		Kills        int32   `json:"kills"`
		SurvivalTime float64 `json:"survival_time"`
		DungeonLevel int32   `json:"dungeon_level"`
		StyleID      int32   `json:"style_id"`
	}{
		Kills:        req.Kills,
		SurvivalTime: req.SurvivalTime,
		DungeonLevel: req.DungeonLevel,
		StyleID:      req.StyleId,
	})
	if err != nil {
		return "", fmt.Errorf("combat score metadata: %w", err)
	}
	return string(metadata), nil
}
