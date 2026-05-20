// Package store — Redis 存储实现
//
// Redis 在游戏服务器中的用途：
//   - 会话缓存：玩家登录后，将玩家信息缓存到 Redis，避免每次请求都查 MySQL
//   - 排行榜：使用 Sorted Set（ZSET），天然支持排名计算和范围查询
//   - 在线状态：用 SET 记录当前在线玩家，用于跨服匹配等
//   - 限流：用 INCR + EXPIRE 实现请求频率限制
package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"game-server/internal/config"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// RedisStore Redis 数据存储
type RedisStore struct {
	client *redis.Client
	ctx    context.Context // 默认上下文，用于简单操作
}

// NewRedisStore 创建并初始化 Redis 存储实例
func NewRedisStore(cfg *config.RedisConfig) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr(),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx := context.Background()

	// 测试连接是否正常
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	zap.L().Info("Redis 初始化成功", zap.String("addr", cfg.Addr()))

	return &RedisStore{
		client: client,
		ctx:    ctx,
	}, nil
}

// Close 关闭 Redis 连接
func (s *RedisStore) Close() error {
	return s.client.Close()
}

// ========== 会话缓存操作 ==========

// sessionKeyPrefix 会话缓存的 key 前缀
// 使用前缀命名空间，避免不同业务的数据冲突
const sessionKeyPrefix = "session:"

// SessionData 会话缓存数据
// 存储在 Redis 中的玩家会话信息，避免每次请求都查 MySQL
type SessionData struct {
	Uid      int64  // 用户ID
	Nickname string // 昵称
	Token    string // 会话令牌
}

// SetSession 设置玩家会话缓存
// 过期时间应略长于心跳超时时间，这里设为 2 小时
func (s *RedisStore) SetSession(uid int64, data *SessionData) error {
	key := fmt.Sprintf("%s%d", sessionKeyPrefix, uid)
	// 用 Hash 存储会话数据，方便单独读取/更新某个字段
	err := s.client.HSet(s.ctx, key, map[string]interface{}{
		"uid":      data.Uid,
		"nickname": data.Nickname,
		"token":    data.Token,
	}).Err()
	if err != nil {
		return err
	}
	// 设置过期时间，防止僵尸会话永远占用内存
	return s.client.Expire(s.ctx, key, 2*time.Hour).Err()
}

// GetSession 获取玩家会话缓存
func (s *RedisStore) GetSession(uid int64) (*SessionData, error) {
	key := fmt.Sprintf("%s%d", sessionKeyPrefix, uid)
	result, err := s.client.HGetAll(s.ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil // 缓存未命中，返回 nil（不是错误）
	}

	uidVal, _ := strconv.ParseInt(result["uid"], 10, 64)
	return &SessionData{
		Uid:      uidVal,
		Nickname: result["nickname"],
		Token:    result["token"],
	}, nil
}

// DelSession 删除玩家会话缓存（登出时调用）
func (s *RedisStore) DelSession(uid int64) error {
	key := fmt.Sprintf("%s%d", sessionKeyPrefix, uid)
	return s.client.Del(s.ctx, key).Err()
}

// ========== 排行榜操作 ==========

// rankKeyPrefix 排行榜 key 前缀
// 不同类型的排行榜用不同的 key，如 rank:1 (最高分)、rank:2 (击杀数)
const rankKeyPrefix = "rank:"

// getRankKey 获取排行榜的 Redis key
func getRankKey(rankType int) string {
	return fmt.Sprintf("%s%d", rankKeyPrefix, rankType)
}

// UpdateRank 更新排行榜分数
// 使用 ZADD 命令，如果分数更高则更新
// member 格式为 "uid:nickname"，方便排行榜直接展示
func (s *RedisStore) UpdateRank(rankType int, uid int64, nickname string, score int64) error {
	key := getRankKey(rankType)
	member := fmt.Sprintf("%d:%s", uid, nickname)
	// ZADD 默认会覆盖旧分数，这里我们不限制，始终更新为最新分数
	return s.client.ZAdd(s.ctx, key, &redis.Z{
		Score:  float64(score),
		Member: member,
	}).Err()
}

// GetRank 获取排行榜
// 使用 ZREVRANGE 按分数从高到低排序
// start, stop 是排名范围（0-based），-1 表示到最后
func (s *RedisStore) GetRank(rankType, start, stop int) ([]redis.Z, error) {
	key := getRankKey(rankType)
	// ZREVRANGEWITHSCORES 返回成员和分数
	return s.client.ZRevRangeWithScores(s.ctx, key, int64(start), int64(stop)).Result()
}

// GetPlayerRank 获取玩家在排行榜中的排名
// ZREVRANK 返回按分数从高到低的排名（0-based）
func (s *RedisStore) GetPlayerRank(rankType int, uid int64, nickname string) (int64, error) {
	key := getRankKey(rankType)
	member := fmt.Sprintf("%d:%s", uid, nickname)
	rank, err := s.client.ZRevRank(s.ctx, key, member).Result()
	if err != nil {
		return 0, err
	}
	return rank + 1, nil // +1 转换为 1-based 排名
}

// ========== 限流操作 ==========

// rateLimitKeyPrefix 限流 key 前缀
const rateLimitKeyPrefix = "rate:"

// CheckRateLimit 检查请求频率限制
// 使用 INCR + EXPIRE 实现：
//  1. 对 key 执行 INCR（计数+1）
//  2. 如果是第一次（值为1），设置过期时间
//  3. 如果计数超过限制，返回 false
//
// 参数：
//   - key: 限流标识（如 "uid:12345" 表示对某个用户限流）
//   - limit: 时间窗口内允许的最大请求数
//   - window: 时间窗口
func (s *RedisStore) CheckRateLimit(key string, limit int, window time.Duration) (bool, error) {
	fullKey := fmt.Sprintf("%s%s", rateLimitKeyPrefix, key)

	// 使用 Lua 脚本保证原子性
	// 为什么用 Lua？INCR + EXPIRE 是两个命令，非原子操作可能导致竞态
	luaScript := `
		local count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('EXPIRE', KEYS[1], ARGV[2])
		end
		if count > tonumber(ARGV[1]) then
			return 0
		end
		return 1
	`
	result, err := s.client.Eval(s.ctx, luaScript, []string{fullKey}, limit, int(window.Seconds())).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
