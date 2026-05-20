// Package store 数据存储层，封装 Redis 和 MySQL 的操作。
//
// 设计原则：
//   - 对上层（business 层）暴露领域语义的方法，如 GetPlayer、SaveArchive
//   - 内部处理数据库连接、重试、错误转换等基础设施逻辑
//   - 上层不需要知道数据来自 Redis 还是 MySQL
package store

import (
	"fmt"

	"game-server/internal/config"
	"game-server/internal/model"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 重新导出 model 类型，业务层只需 import store 即可使用
// 避免业务层同时依赖 store 和 model 两个包
type (
	Player        = model.Player
	Archive       = model.Archive
	ScoreRecord   = model.ScoreRecord
	PaymentOrder  = model.PaymentOrder
)

// MySQLStore MySQL 数据存储
// 封装 gorm.DB 实例，提供业务相关的数据库操作方法
type MySQLStore struct {
	db *gorm.DB
}

// NewMySQLStore 创建并初始化 MySQL 存储实例
//
// 初始化流程：
//  1. 建立数据库连接
//  2. 配置连接池参数（控制资源使用）
//  3. 自动迁移表结构（开发阶段用，生产环境应使用 migration 工具）
func NewMySQLStore(cfg *config.MySQLConfig) (*MySQLStore, error) {
	// GORM 日志配置：开发阶段打印所有 SQL，生产环境只打印慢查询和错误
	logLevel := gormlogger.Info
	// TODO: 根据 config.Log.Level 动态调整

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	// 配置连接池
	// 为什么需要连接池？
	//   - 建立 MySQL 连接很慢（TCP握手 + 认证），复用连接避免重复开销
	//   - 限制最大连接数，防止数据库被压垮
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns) // 空闲连接数，保持一定数量的连接随时可用
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns) // 最大连接数，超过此数量的请求会等待

	// 自动迁移表结构
	// AutoMigrate 会创建不存在的表和列，但不会删除已有的列（安全）
	// 生产环境建议使用独立的 migration 工具（如 golang-migrate）
	if err := db.AutoMigrate(&model.Player{}, &model.Archive{}, &model.ScoreRecord{}, &model.PaymentOrder{}); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	zap.L().Info("MySQL 初始化成功", zap.String("dsn", maskPassword(cfg.DSN())))

	return &MySQLStore{db: db}, nil
}

// Close 关闭数据库连接
func (s *MySQLStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ========== 玩家相关操作 ==========

// GetPlayerByID 根据 ID 查询玩家
func (s *MySQLStore) GetPlayerByID(id int64) (*model.Player, error) {
	var player model.Player
	err := s.db.Where("id = ?", id).First(&player).Error
	if err != nil {
		return nil, err
	}
	return &player, nil
}

// GetPlayerByOpenID 根据微信 OpenID 查询玩家
// 用于微信登录时判断用户是否已注册
func (s *MySQLStore) GetPlayerByOpenID(openID string) (*model.Player, error) {
	var player model.Player
	err := s.db.Where("open_id = ?", openID).First(&player).Error
	if err != nil {
		return nil, err
	}
	return &player, nil
}

// CreatePlayer 创建新玩家
// 微信首次登录时调用
func (s *MySQLStore) CreatePlayer(player *model.Player) error {
	return s.db.Create(player).Error
}

// UpdatePlayer 更新玩家信息
func (s *MySQLStore) UpdatePlayer(player *model.Player) error {
	return s.db.Save(player).Error
}

// ========== 存档相关操作 ==========

// GetArchive 根据玩家 ID 查询存档
func (s *MySQLStore) GetArchive(playerID int64) (*model.Archive, error) {
	var archive model.Archive
	err := s.db.Where("player_id = ?", playerID).First(&archive).Error
	if err != nil {
		return nil, err
	}
	return &archive, nil
}

// SaveArchive 保存或更新存档
// 使用 GORM 的 Save 方法：主键存在则更新，不存在则创建
func (s *MySQLStore) SaveArchive(archive *model.Archive) error {
	return s.db.Save(archive).Error
}

// ========== 分数相关操作 ==========

// CreateScoreRecord 创建分数记录
func (s *MySQLStore) CreateScoreRecord(record *model.ScoreRecord) error {
	return s.db.Create(record).Error
}

// UpdateBestScore 更新玩家最高分
func (s *MySQLStore) UpdateBestScore(playerID int64, score int64) error {
	return s.db.Model(&model.Player{}).Where("id = ? AND best_score < ?", playerID, score).
		Update("best_score", score).Error
}

// maskPassword 遮蔽 DSN 中的密码，用于日志输出
// 防止密码泄露到日志文件
func maskPassword(dsn string) string {
	// 简单实现：只显示用户名@host，隐藏密码
	// 格式：user:password@tcp(host:port)/dbname
	return "***@tcp(***:***)/**"
}

// ========== 订单相关操作 ==========

// 订单状态常量
const (
	OrderStatusPending   = 0 // 待支付
	OrderStatusPaid      = 1 // 已支付
	OrderStatusDelivered = 2 // 已发货
	OrderStatusCanceled  = 3 // 已取消
	OrderStatusRefunded  = 4 // 已退款
)

// CreateOrder 创建支付订单
func (s *MySQLStore) CreateOrder(order *model.PaymentOrder) error {
	return s.db.Create(order).Error
}

// GetOrderByOrderNo 根据订单号查询订单
func (s *MySQLStore) GetOrderByOrderNo(orderNo string) (*model.PaymentOrder, error) {
	var order model.PaymentOrder
	err := s.db.Where("order_no = ?", orderNo).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateOrderStatus 更新订单状态
func (s *MySQLStore) UpdateOrderStatus(orderNo string, status int) error {
	return s.db.Model(&model.PaymentOrder{}).Where("order_no = ?", orderNo).
		Update("status", status).Error
}
