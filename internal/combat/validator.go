package combat

import (
	"errors"
	"fmt"

	"game-server/internal/protocol"
)

// validateCombatResult 验证战斗结算数据的合理性
// 基础反作弊：检查分数范围、击杀数与地牢等级的合理性、时间边界
// 这是服务器端验证，不是客户端验证——客户端可以伪造数据，
// 服务器必须对明显不合理的数据拒绝结算
func validateCombatResult(req *protocol.CombatResultReq, cfg *CombatConfig) error {
	// 地牢等级必须大于0
	if req.DungeonLevel <= 0 {
		return errors.New("无效的地牢等级")
	}

	// 分数上限检查：每级有最大合理分数
	maxScore := cfg.MaxScorePerLevel * req.DungeonLevel
	if req.Score < 0 || req.Score > maxScore {
		return fmt.Errorf("分数异常: score=%d, max=%d", req.Score, maxScore)
	}

	// 击杀数上限检查
	dungeonCfg := GetDungeonConfig(req.DungeonLevel)
	if dungeonCfg != nil {
		maxKills := int(float64(dungeonCfg.RoomCount) * dungeonCfg.EnemyDensity * cfg.MaxKillsMultiplier)
		if req.Kills < 0 || req.Kills > maxKills {
			return fmt.Errorf("击杀数异常: kills=%d, max=%d", req.Kills, maxKills)
		}
	}

	// 存活时间边界检查
	if req.SurvivalTime < 0 {
		return errors.New("存活时间不能为负")
	}
	// 合理最长游戏时间：2小时（7200秒）
	if req.SurvivalTime > 7200 {
		return fmt.Errorf("存活时间异常: time=%.1f", req.SurvivalTime)
	}

	// 流派ID合法性检查
	if req.StyleID < 0 || req.StyleID > 5 {
		return fmt.Errorf("无效的流派ID: style_id=%d", req.StyleID)
	}

	return nil
}
