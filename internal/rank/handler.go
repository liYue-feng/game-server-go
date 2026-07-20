// Package rank 排行榜模块（pitaya 风格组件）
//
// 排行榜是游戏重要社交功能，实现方案：
//   - Redis Sorted Set（ZSET）存储排行榜数据，天然支持排名计算
//   - MySQL 存储历史分数记录，用于数据分析和灾备
//   - ZREVRANGE 按分数从高到低排序，ZREVRANK 查询指定玩家排名
//
// 改造要点：handler 签名统一为 func(ctx, *Req) (*Resp, error)，
// 玩家身份通过 session.FromContext(ctx).UID() 获取。
package rank

import (
	"context"
	"strconv"
	"strings"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler 排行榜处理器（组件）。
type Handler struct {
	redis *store.RedisStore
	mysql *store.MySQLStore
}

// NewHandler 创建排行榜处理器。
func NewHandler(redis *store.RedisStore, mysql *store.MySQLStore) *Handler {
	return &Handler{redis: redis, mysql: mysql}
}

// GetRank 处理获取排行榜请求。
func (h *Handler) GetRank(ctx context.Context, req *protocol.GetRankReq) (*protocol.GetRankResp, error) {
	// 参数校验与默认值
	if req.Count <= 0 || req.Count > 100 {
		req.Count = 100
	}
	if req.Start < 0 {
		req.Start = 0
	}

	// 从 Redis 读取排行榜（按分数从高到低）
	results, err := h.redis.GetRank(req.RankType, req.Start, req.Start+req.Count-1)
	if err != nil {
		zap.L().Error("获取排行榜失败", zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "获取排行榜失败")
	}

	// member 格式为 "uid:nickname"（在 UpdateRank 时约定）
	ranks := make([]protocol.RankItem, 0, len(results))
	for i, z := range results {
		member, ok := z.Member.(string)
		if !ok {
			continue
		}
		uid, nickname := parseMember(member)
		level := 0
		if stats, err := h.mysql.GetPlayerStats(uid); err == nil {
			level = stats.Level
		}
		ranks = append(ranks, protocol.RankItem{
			Uid:      uid,
			Nickname: nickname,
			Level:    level,
			Score:    int64(z.Score),
			Rank:     req.Start + i + 1,
		})
	}

	zap.L().Debug("获取排行榜", zap.Int("rankType", req.RankType), zap.Int("count", len(ranks)))
	return &protocol.GetRankResp{Ranks: ranks}, nil
}

// SubmitScore 处理提交分数请求。
//
// 流程：校验分数 -> 更新 Redis 排行榜 -> 异步写 MySQL 历史 -> 刷新最高分 -> 返回最高分。
func (h *Handler) SubmitScore(ctx context.Context, req *protocol.SubmitScoreReq) (*protocol.SubmitScoreResp, error) {
	uid := uidFromCtx(ctx)

	// 基础防作弊：分数不能为负
	if req.Score < 0 {
		return nil, protocol.NewBizError(protocol.ErrInvalidParam, "分数不能为负数")
	}

	zap.L().Info("提交分数", zap.Int64("uid", uid), zap.Int64("score", req.Score))

	// 获取昵称用于排行榜展示（默认用 uid）
	nickname := strconv.FormatInt(uid, 10)
	if player, err := h.mysql.GetPlayerByID(uid); err == nil {
		nickname = player.Nickname
	}

	// 更新 Redis 排行榜
	if err := h.redis.UpdateRank(RankType_TopScore, uid, nickname, req.Score); err != nil {
		zap.L().Error("更新排行榜失败", zap.Error(err))
	}

	// 异步写入 MySQL 历史记录（归档用途，不阻塞响应）
	go func() {
		record := &store.ScoreRecord{PlayerID: uid, Score: req.Score, Metadata: req.Metadata}
		if err := h.mysql.CreateScoreRecord(record); err != nil {
			zap.L().Error("写入分数记录失败", zap.Error(err))
		}
	}()

	// 刷新最高分
	var bestScore int64
	if err := h.mysql.UpdateBestScore(uid, req.Score); err == nil {
		bestScore = req.Score
		zap.L().Info("刷新最高分", zap.Int64("uid", uid), zap.Int64("score", req.Score))
	} else if player, err := h.mysql.GetPlayerByID(uid); err == nil {
		bestScore = player.BestScore
	}

	return &protocol.SubmitScoreResp{Success: true, BestScore: bestScore}, nil
}

// parseMember 解析 Redis Sorted Set 的 member 字段，格式 "uid:nickname"。
func parseMember(member string) (int64, string) {
	parts := strings.SplitN(member, ":", 2)
	if len(parts) != 2 {
		return 0, member
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, member
	}
	return uid, parts[1]
}

// uidFromCtx 从会话取玩家 ID。
func uidFromCtx(ctx context.Context) int64 {
	if s := session.FromContext(ctx); s != nil {
		return s.UID()
	}
	return 0
}

// 排行榜类型常量
const (
	RankType_TopScore  = 1 // 最高分排行榜
	RankType_KillCount = 2 // 击杀数排行榜（可扩展）
)
