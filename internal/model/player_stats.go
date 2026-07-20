package model

import "time"

// PlayerStats 玩家战斗属性表
// 存储玩家的等级、经验、金币和战斗属性，与 Player 表一对一关联
type PlayerStats struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerID    int64     `gorm:"uniqueIndex;not null" json:"player_id"` // 关联玩家 ID，一对一
	Level       int       `gorm:"default:1" json:"level"`                // 玩家等级
	Exp         int       `gorm:"default:0" json:"exp"`                  // 当前经验值
	Gold        int       `gorm:"default:0" json:"gold"`                 // 金币数量
	MaxHp       int       `gorm:"default:100" json:"max_hp"`             // 最大生命值
	MaxStamina  int       `gorm:"default:100" json:"max_stamina"`        // 最大耐力值
	AttackPower int       `gorm:"default:10" json:"attack_power"`        // 攻击力
	BestScore   int64     `gorm:"default:0" json:"best_score"`           // 历史最高分
	TotalKills  int64     `gorm:"default:0" json:"total_kills"`          // 累计击杀数
	TotalGames  int64     `gorm:"default:0" json:"total_games"`          // 累计游戏局数
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (PlayerStats) TableName() string {
	return "player_stats"
}

// PlayerStyle 玩家已解锁流派表
// 记录玩家解锁了哪些战斗流派
type PlayerStyle struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerID  int64     `gorm:"uniqueIndex:idx_player_style;not null" json:"player_id"` // 关联玩家 ID
	StyleID   int       `gorm:"uniqueIndex:idx_player_style;not null" json:"style_id"`  // 流派ID
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`                       // 解锁时间
}

func (PlayerStyle) TableName() string {
	return "player_styles"
}
