// Package combat — 战斗模块：处理地牢结算、敌人配置、流派系统等。
// 战斗逻辑运行在客户端（客户端权威），服务器负责：
//   - 验证战斗结果（基础反作弊）
//   - 下发敌人/地牢/流派配置
//   - 结算奖励（金币、经验）
//   - 持久化玩家战斗属性
package combat

import "game-server/internal/protocol"

// ========== 战斗参数配置 ==========

// CombatConfig 战斗系统全局参数
// 后续可从配置文件或数据库加载，初期硬编码
type CombatConfig struct {
	MaxHp              int     // 玩家最大HP
	MaxStamina         int     // 玩家最大耐力
	BaseAttackPower    int     // 基础攻击力
	DashCost           int     // 冲刺耐力消耗
	HeavyAttackCost    int     // 重击耐力消耗
	ParryCost          int     // 弹反耐力消耗
	ParryWindow        float64 // 弹反窗口（秒）
	GoldPerKill        int     // 每次击杀金币奖励
	ExpPerKill         int     // 每次击杀经验奖励
	MaxScorePerLevel   int     // 每级地牢最高合理分数
	MaxKillsMultiplier float64 // 击杀数上限 = 房间数 * 密度 * 此倍率
}

// DefaultCombatConfig 默认战斗参数
func DefaultCombatConfig() *CombatConfig {
	return &CombatConfig{
		MaxHp:              100,
		MaxStamina:         100,
		BaseAttackPower:    10,
		DashCost:           25,
		HeavyAttackCost:    30,
		ParryCost:          15,
		ParryWindow:        0.3,
		GoldPerKill:        5,
		ExpPerKill:         10,
		MaxScorePerLevel:   10000,
		MaxKillsMultiplier: 3.0,
	}
}

// ========== 敌人配置表 ==========

// GetEnemyConfigs 返回所有敌人类型配置
// 初期硬编码，后续从数据库/配置文件加载
func GetEnemyConfigs() []protocol.EnemyConfigItem {
	return []protocol.EnemyConfigItem{
		{
			ID:          1,
			Name:        "杂兵",
			Hp:          30,
			Damage:      10,
			Speed:       2.0,
			AttackRange: 1.2,
			EnemyType:   "grunt",
		},
		{
			ID:          2,
			Name:        "弓手",
			Hp:          20,
			Damage:      8,
			Speed:       1.5,
			AttackRange: 6.0,
			EnemyType:   "archer",
		},
		{
			ID:          3,
			Name:        "精英",
			Hp:          80,
			Damage:      20,
			Speed:       3.0,
			AttackRange: 1.5,
			EnemyType:   "elite",
		},
		{
			ID:          4,
			Name:        "Boss",
			Hp:          300,
			Damage:      25,
			Speed:       2.5,
			AttackRange: 2.0,
			EnemyType:   "boss",
		},
	}
}

// ========== 地牢配置表 ==========

// GetDungeonConfig 返回指定等级的地牢配置
func GetDungeonConfig(level int) *protocol.DungeonConfigItem {
	// 基础配置，随等级线性增长
	roomCount := 8 + level*2
	if roomCount > 20 {
		roomCount = 20
	}
	enemyDensity := 1.0 + float64(level-1)*0.15
	if enemyDensity > 3.0 {
		enemyDensity = 3.0
	}

	return &protocol.DungeonConfigItem{
		Level:         level,
		RoomCount:     roomCount,
		EnemyDensity:  enemyDensity,
		BossID:        4, // 默认Boss
	}
}

// ========== 流派配置表 ==========

// GetStyleConfigs 返回所有流派配置
func GetStyleConfigs() []protocol.StyleConfigItem {
	return []protocol.StyleConfigItem{
		{
			StyleID:             1,
			StyleName:           "刃",
			DamageMult:          1.0,
			SpeedMult:           1.2,
			ParryMult:           1.0,
			DashSpeedMult:       1.0,
			DashCostMult:        1.0,
			SpecialResourceMax:  100,
			SpecialResourceName: "怒气",
			Description:         "高速连击流派，刃风暴消耗怒气",
		},
		{
			StyleID:             2,
			StyleName:           "印",
			DamageMult:          0.8,
			SpeedMult:           1.0,
			ParryMult:           1.5,
			DashSpeedMult:       1.0,
			DashCostMult:        1.0,
			SpecialResourceMax:  5,
			SpecialResourceName: "印记",
			Description:         "弹反强化流派，弹反放置印记可引爆",
		},
		{
			StyleID:             3,
			StyleName:           "毒",
			DamageMult:          0.6,
			SpeedMult:           1.0,
			ParryMult:           0.8,
			DashSpeedMult:       1.0,
			DashCostMult:        1.0,
			SpecialResourceMax:  100,
			SpecialResourceName: "毒液",
			Description:         "持续伤害流派，攻击叠毒，毒雾范围DoT",
		},
		{
			StyleID:             4,
			StyleName:           "血",
			DamageMult:          1.5,
			SpeedMult:           0.8,
			ParryMult:           0.6,
			DashSpeedMult:       1.0,
			DashCostMult:        1.0,
			SpecialResourceMax:  100,
			SpecialResourceName: "鲜血",
			Description:         "高风险高回报流派，血祭扣HP换爆发伤害",
		},
		{
			StyleID:             5,
			StyleName:           "剑",
			DamageMult:          1.2,
			SpeedMult:           1.0,
			ParryMult:           1.3,
			DashSpeedMult:       1.0,
			DashCostMult:        1.0,
			SpecialResourceMax:  100,
			SpecialResourceName: "专注",
			Description:         "均衡反击流派，完美弹反增益，剑气远程斩击",
		},
	}
}
