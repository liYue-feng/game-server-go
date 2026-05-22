// Package rank 排行榜模块
//
// 排行榜是游戏的重要社交功能，实现方案：
//   - Redis Sorted Set（ZSET）存储排行榜数据，天然支持排名计算
//   - MySQL 存储历史分数记录，用于数据分析和灾备
//   - ZREVRANGE 按分数从高到低排序，ZREVRANK 查询指定玩家排名
//
// 数据流：
//   提交分数 -> 更新 Redis ZSET + 写入 MySQL -> 返回排名信息
//   查询排行榜 -> 从 Redis ZSET 读取 -> 返回排名列表
package rank

import (
	"encoding/json"
	"strconv"
	"strings"

	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler 排行榜处理器
type Handler struct {
	redis *store.RedisStore
	mysql *store.MySQLStore
}

// NewHandler 创建排行榜处理器
func NewHandler(redis *store.RedisStore, mysql *store.MySQLStore) *Handler {
	return &Handler{
		redis: redis,
		mysql: mysql,
	}
}

// HandleGetRank 处理获取排行榜请求
func (h *Handler) HandleGetRank(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.GetRankReq
	if err := json.Unmarshal(body, &req); err != nil {
		zap.L().Error("获取排行榜请求解析失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "请求格式错误",
		})
		return
	}

	// 参数校验
	if req.Count <= 0 || req.Count > 100 {
		req.Count = 100 // 默认返回前100名
	}
	if req.Start < 0 {
		req.Start = 0
	}

	// 从 Redis 读取排行榜
	// ZREVRANGEWITHSCORES 返回按分数从高到低排序的结果
	results, err := h.redis.GetRank(req.RankType, req.Start, req.Start+req.Count-1)
	if err != nil {
		zap.L().Error("获取排行榜失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInternal,
			Msg:  "获取排行榜失败",
		})
		return
	}

	// 解析 Redis 返回的结果
	// member 格式为 "uid:nickname"（在 UpdateRank 时设定）
	ranks := make([]protocol.RankItem, 0, len(results))
	for i, z := range results {
		member, ok := z.Member.(string)
		if !ok {
			continue
		}

		uid, nickname := parseMember(member)
		ranks = append(ranks, protocol.RankItem{
			Uid:      uid,
			Nickname: nickname,
			Score:    int64(z.Score),
			Rank:     req.Start + i + 1, // 排名从1开始
		})
	}

	conn.SendMessage(protocol.MsgID_GetRankResp, protocol.GetRankResp{
		Ranks: ranks,
	})

	zap.L().Debug("获取排行榜",
		zap.Int("rankType", req.RankType),
		zap.Int("count", len(ranks)),
	)
}

// HandleSubmitScore 处理提交分数请求
//
// 流程：
//  1. 校验分数合法性（防作弊：分数不能为负数等）
//  2. 更新 Redis 排行榜
//  3. 写入 MySQL 历史记录
//  4. 如果是新最高分，更新玩家表的 best_score 字段
//  5. 返回历史最高分
func (h *Handler) HandleSubmitScore(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.SubmitScoreReq
	if err := json.Unmarshal(body, &req); err != nil {
		zap.L().Error("提交分数请求解析失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "请求格式错误",
		})
		return
	}

	uid := conn.GetUID()

	// 基础校验
	if req.Score < 0 {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "分数不能为负数",
		})
		return
	}

	zap.L().Info("提交分数",
		zap.Int64("uid", uid),
		zap.Int64("score", req.Score),
	)

	// 获取玩家昵称用于排行榜显示
	nickname := strconv.FormatInt(uid, 10) // 默认用 uid
	if player, err := h.mysql.GetPlayerByID(uid); err == nil {
		nickname = player.Nickname
	}

	// 更新 Redis 排行榜
	if err := h.redis.UpdateRank(RankType_TopScore, uid, nickname, req.Score); err != nil {
		zap.L().Error("更新排行榜失败", zap.Error(err))
	}

	// 写入 MySQL 历史记录（异步，不阻塞响应）
	// 为什么异步？因为这只是归档记录，不需要等写入完成才返回
	go func() {
		record := &store.ScoreRecord{
			PlayerID: uid,
			Score:    req.Score,
			Metadata: req.Metadata,
		}
		if err := h.mysql.CreateScoreRecord(record); err != nil {
			zap.L().Error("写入分数记录失败", zap.Error(err))
		}
	}()

	// 更新最高分
	var bestScore int64
	if err := h.mysql.UpdateBestScore(uid, req.Score); err == nil {
		// 更新成功，说明是新最高分
		bestScore = req.Score
		zap.L().Info("刷新最高分", zap.Int64("uid", uid), zap.Int64("score", req.Score))
	} else {
		// 更新失败（条件不满足或数据库错误），查询当前最高分
		player, err := h.mysql.GetPlayerByID(uid)
		if err == nil {
			bestScore = player.BestScore
		}
	}

	conn.SendMessage(protocol.MsgID_SubmitScoreResp, protocol.SubmitScoreResp{
		Success:   true,
		BestScore: bestScore,
	})
}

// parseMember 解析 Redis Sorted Set 的 member 字段
// member 格式: "uid:nickname"
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

// 排行榜类型常量
const (
	RankType_TopScore  = 1 // 最高分排行榜
	RankType_KillCount = 2 // 击杀数排行榜（可扩展）
)
