// Package model 定义数据库实体模型。
//
// 这些结构体与数据库表一一对应，通过 GORM 的 AutoMigrate 自动建表。
// 命名规则：Go 结构体名 -> 数据库表名（GORM 自动蛇形复数化，如 Player -> players）
package model

import "time"

// Player 玩家账号表
// 存储玩家的基础信息和认证数据，是最核心的表
type Player struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`          // 用户唯一ID，自增主键
	OpenID    string    `gorm:"uniqueIndex;size:64;not null" json:"open_id"` // 微信 OpenID，唯一标识一个微信用户
	Nickname  string    `gorm:"size:64;default:''" json:"nickname"`          // 昵称，默认"玩家"+ID
	AvatarURL string    `gorm:"size:512;default:''" json:"avatar_url"`       // 头像 URL
	Token     string    `gorm:"size:128;default:''" json:"token"`            // 会话令牌
	BestScore int64     `gorm:"default:0" json:"best_score"`                 // 历史最高分（冗余字段，排行榜也用 Redis 维护）
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`            // 注册时间
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`            // 最后更新时间
}

// TableName 指定表名，避免 GORM 默认的蛇形复数规则
func (Player) TableName() string {
	return "players"
}

// Archive 游戏存档表
// 存储玩家的完整游戏存档，以 protobuf 字节形式保存
// 吸血鬼幸存者类游戏的存档结构变化频繁，用 JSON 存储比关系型更灵活
type Archive struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerID  int64     `gorm:"uniqueIndex;not null" json:"player_id"` // 关联玩家 ID，一个玩家一个存档
	Data      []byte    `gorm:"type:blob;not null" json:"data"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (Archive) TableName() string {
	return "archives"
}

// ScoreRecord 分数记录表
// 记录玩家每局游戏的分数，用于历史查询和数据分析
// 排行榜的实时查询走 Redis Sorted Set，这里做持久化备份
type ScoreRecord struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerID  int64     `gorm:"index;not null" json:"player_id"`        // 关联玩家 ID
	Score     int64     `gorm:"not null" json:"score"`                  // 本局分数
	Metadata  string    `gorm:"type:text" json:"metadata"`              // 附加数据（击杀数、存活时间等）
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"` // 用作时间范围查询
}

func (ScoreRecord) TableName() string {
	return "score_records"
}

// PaymentOrder 支付订单表
// 记录每笔支付的全生命周期，是支付系统的核心表
// 订单号必须全局唯一，同一订单只能发货一次（幂等保证）
type PaymentOrder struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderNo   string    `gorm:"uniqueIndex;size:64;not null" json:"order_no"` // 商户订单号，全局唯一
	PlayerID  int64     `gorm:"index;not null" json:"player_id"`              // 关联玩家 ID
	ProductID int       `gorm:"not null" json:"product_id"`                   // 商品ID
	Amount    int64     `gorm:"not null" json:"amount"`                       // 支付金额（单位：分）
	Status    int       `gorm:"not null;default:0" json:"status"`             // 订单状态：0=待支付 1=已支付 2=已发货 3=已取消 4=已退款
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (PaymentOrder) TableName() string {
	return "payment_orders"
}
