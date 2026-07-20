// Package combat 战斗模块（pitaya 风格组件）
//
// 客户端在地牢通关或角色死亡后上报本局数据。服务器做基础反作弊校验、
// 计算奖励并持久化。同时提供敌人/地牢/流派等只读配置查询与流派解锁。
//
// 改造要点：handler 签名统一为 func(ctx, *Req) (*Resp, error)，
// 玩家身份通过 session.FromContext(ctx).UID() 获取，错误用 protocol.NewBizError 返回。
package combat

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

// Handler 战斗模块处理器（组件）。
type Handler struct {
	mysql      *store.MySQLStore
	redis      *store.RedisStore
	cfg        *CombatConfig
	settlement *SettlementService
	archives   store.ArchiveRepository
	stats      store.DevelopmentPlayerStatsRepository
}

// NewHandler 创建战斗模块处理器。
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore) *Handler {
	cfg := DefaultCombatConfig()
	return &Handler{
		mysql:      mysql,
		redis:      redis,
		cfg:        cfg,
		settlement: NewSettlementService(store.NewMySQLCombatSettlementRepository(mysql, settlementRewardPolicy(cfg)), cfg),
		archives:   mysql,
	}
}

// NewDevelopmentHandler exposes only settlement and stats over an in-memory store.
func NewDevelopmentHandler(settlement *SettlementService, archives store.ArchiveRepository, stats store.DevelopmentPlayerStatsRepository) *Handler {
	return &Handler{cfg: DefaultCombatConfig(), settlement: settlement, archives: archives, stats: stats}
}

// CombatResult 处理战斗结算请求：反作弊校验 -> 计算奖励 -> 持久化 -> 同步排行榜。
func (h *Handler) CombatResult(ctx context.Context, req *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error) {
	if err := validateCombatResult(req, h.cfg); err != nil {
		return nil, protocol.NewBizError(protocol.ErrCombatCheatDetected, err.Error())
	}
	if h.settlement != nil {
		response, err := h.settlement.Settle(uidFromCtx(ctx), req)
		if err != nil {
			return nil, protocol.NewBizError(protocol.ErrInternal, "combat settlement failed")
		}
		if !response.Duplicate && h.redis != nil {
			if err := h.redis.UpdateRank(1, uidFromCtx(ctx), strconv.FormatInt(uidFromCtx(ctx), 10), req.Score); err != nil {
				zap.L().Error("同步排行榜失败", zap.Int64("uid", uidFromCtx(ctx)), zap.Error(err))
			}
		}
		return response, nil
	}
	// 基础反作弊校验
	if err := validateCombatResult(req, h.cfg); err != nil {
		return nil, protocol.NewBizError(protocol.ErrCombatCheatDetected, err.Error())
	}

	uid := uidFromCtx(ctx)
	rewardGold := int(req.Kills) * h.cfg.GoldPerKill
	rewardExp := int(req.Kills) * h.cfg.ExpPerKill

	// 读取或初始化玩家战斗属性
	stats, err := h.mysql.GetPlayerStats(uid)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		stats = h.defaultStats(uid)
		if createErr := h.mysql.CreatePlayerStats(stats); createErr != nil {
			zap.L().Error("创建玩家属性失败", zap.Int64("uid", uid), zap.Error(createErr))
			return nil, protocol.NewBizError(protocol.ErrInternal, "服务器内部错误")
		}
	} else if err != nil {
		zap.L().Error("查询玩家属性失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "服务器内部错误")
	}

	// 原子更新：加金币、加经验、累加击杀与局数（失败仅记日志，不阻断结算）
	if rewardGold > 0 {
		if err := h.mysql.AddPlayerGold(uid, rewardGold); err != nil {
			zap.L().Error("增加金币失败", zap.Int64("uid", uid), zap.Error(err))
		}
	}
	if rewardExp > 0 {
		if err := h.mysql.AddPlayerExp(uid, rewardExp); err != nil {
			zap.L().Error("增加经验失败", zap.Int64("uid", uid), zap.Error(err))
		}
	}
	if req.Kills > 0 {
		if err := h.mysql.IncrementPlayerTotalKills(uid, int(req.Kills)); err != nil {
			zap.L().Error("累加击杀数失败", zap.Int64("uid", uid), zap.Error(err))
		}
	}
	if err := h.mysql.IncrementPlayerTotalGames(uid); err != nil {
		zap.L().Error("累加局数失败", zap.Int64("uid", uid), zap.Error(err))
	}

	// 更新最高分（仅高于历史才更新）
	var bestScore int64
	if err := h.mysql.UpdatePlayerBestScore(uid, req.Score); err != nil {
		zap.L().Error("更新最高分失败", zap.Int64("uid", uid), zap.Error(err))
	}
	if updated, err := h.mysql.GetPlayerStats(uid); err == nil {
		bestScore = updated.BestScore
	}

	// 保存分数记录（历史查询用途）
	scoreRecord := &store.ScoreRecord{
		PlayerID: uid,
		Score:    req.Score,
		Metadata: fmt.Sprintf(`{"kills":%d,"duration_ms":%d,"dungeon_level":%d,"style_id":%d}`,
			req.Kills, req.DurationMs, req.DungeonLevel, req.StyleId),
	}
	if err := h.mysql.CreateScoreRecord(scoreRecord); err != nil {
		zap.L().Error("保存分数记录失败", zap.Int64("uid", uid), zap.Error(err))
	}

	// 同步提交到 Redis 排行榜
	if h.redis != nil {
		nickname := strconv.FormatInt(uid, 10)
		if err := h.redis.UpdateRank(1, uid, nickname, req.Score); err != nil {
			zap.L().Error("提交排行榜失败", zap.Int64("uid", uid), zap.Error(err))
		}
	}

	zap.L().Info("战斗结算成功",
		zap.Int64("uid", uid), zap.Int64("score", req.Score), zap.Int32("kills", req.Kills),
		zap.Int("reward_gold", rewardGold), zap.Int("reward_exp", rewardExp))

	return &protocolpb.CombatResultResp{
		Success:    true,
		RewardGold: int32(rewardGold),
		RewardExp:  int32(rewardExp),
		BestScore:  bestScore,
	}, nil
}

// GetEnemyConfigs 返回敌人配置表。
func (h *Handler) GetEnemyConfigs(ctx context.Context, req *protocolpb.GetEnemyConfigsReq) (*protocolpb.GetEnemyConfigsResp, error) {
	return &protocolpb.GetEnemyConfigsResp{Configs: GetEnemyConfigs()}, nil
}

// GetDungeonConfig 返回指定等级的地牢配置。
func (h *Handler) GetDungeonConfig(ctx context.Context, req *protocolpb.GetDungeonConfigReq) (*protocolpb.GetDungeonConfigResp, error) {
	if req.Level <= 0 {
		return nil, protocol.NewBizError(protocol.ErrCombatConfigNotFound, "无效的地牢等级")
	}
	cfg := GetDungeonConfig(int(req.Level))
	if cfg == nil {
		return nil, protocol.NewBizError(protocol.ErrCombatConfigNotFound, "地牢配置不存在")
	}
	return &protocolpb.GetDungeonConfigResp{
		Level:        cfg.Level,
		RoomCount:    cfg.RoomCount,
		EnemyDensity: cfg.EnemyDensity,
		BossId:       cfg.BossId,
		EnemyConfigs: GetEnemyConfigs(),
	}, nil
}

// GetStyleConfigs 返回流派配置表。
func (h *Handler) GetStyleConfigs(ctx context.Context, req *protocolpb.GetStyleConfigsReq) (*protocolpb.GetStyleConfigsResp, error) {
	return &protocolpb.GetStyleConfigsResp{Styles: GetStyleConfigs()}, nil
}

// UnlockStyle 处理流派解锁请求：校验存在性 -> 幂等检查 -> 扣金币 -> 记录解锁。
func (h *Handler) UnlockStyle(ctx context.Context, req *protocolpb.UnlockStyleReq) (*protocolpb.UnlockStyleResp, error) {
	if getStyleConfig(int(req.StyleId)) == nil {
		return nil, protocol.NewBizError(protocol.ErrCombatStyleLocked, "流派不存在")
	}

	uid := uidFromCtx(ctx)

	// 幂等：已解锁则直接返回成功
	styles, err := h.mysql.GetPlayerStyles(uid)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		zap.L().Error("查询已解锁流派失败", zap.Int64("uid", uid), zap.Error(err))
	}
	for _, s := range styles {
		if s.StyleID == int(req.StyleId) {
			return &protocolpb.UnlockStyleResp{Success: true, GoldCost: 0}, nil
		}
	}

	// 解锁费用：默认 100 金币，流派 1（刀）免费
	goldCost := 100
	if req.StyleId == 1 {
		goldCost = 0
	}

	if goldCost > 0 {
		if err := h.mysql.DeductPlayerGold(uid, goldCost); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, protocol.NewBizError(protocol.ErrCombatInsufficientGold, "金币不足")
			}
			zap.L().Error("扣除金币失败", zap.Int64("uid", uid), zap.Error(err))
			return nil, protocol.NewBizError(protocol.ErrInternal, "服务器内部错误")
		}
	}

	if err := h.mysql.UnlockPlayerStyle(uid, int(req.StyleId)); err != nil {
		zap.L().Error("记录流派解锁失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "解锁失败")
	}

	zap.L().Info("流派解锁成功", zap.Int64("uid", uid), zap.Int32("style_id", req.StyleId), zap.Int("cost", goldCost))
	return &protocolpb.UnlockStyleResp{Success: true, GoldCost: int32(goldCost)}, nil
}

// GetPlayerStats 获取玩家战斗属性；新玩家返回默认值并自动创建记录。
func (h *Handler) GetPlayerStats(ctx context.Context, req *protocolpb.GetPlayerStatsReq) (*protocolpb.GetPlayerStatsResp, error) {
	if h.mysql == nil {
		return h.getDevelopmentPlayerStats(uidFromCtx(ctx))
	}
	uid := uidFromCtx(ctx)

	stats, err := h.mysql.GetPlayerStats(uid)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		stats = h.defaultStats(uid)
		if createErr := h.mysql.CreatePlayerStats(stats); createErr != nil {
			zap.L().Error("创建玩家属性失败", zap.Int64("uid", uid), zap.Error(createErr))
		}
	} else if err != nil {
		zap.L().Error("查询玩家属性失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "服务器内部错误")
	}

	// 已解锁流派；新玩家默认解锁流派 1
	playerStyles, _ := h.mysql.GetPlayerStyles(uid)
	unlockedStyleIDs := make([]int32, 0, len(playerStyles))
	for _, s := range playerStyles {
		unlockedStyleIDs = append(unlockedStyleIDs, int32(s.StyleID))
	}
	if len(unlockedStyleIDs) == 0 {
		unlockedStyleIDs = []int32{1}
		h.mysql.UnlockPlayerStyle(uid, 1)
	}

	return &protocolpb.GetPlayerStatsResp{
		Level:          int32(stats.Level),
		Exp:            int32(stats.Exp),
		Gold:           int32(stats.Gold),
		MaxHp:          int32(stats.MaxHp),
		MaxStamina:     int32(stats.MaxStamina),
		AttackPower:    int32(stats.AttackPower),
		UnlockedStyles: unlockedStyleIDs,
	}, nil
}

func (h *Handler) getDevelopmentPlayerStats(uid int64) (*protocolpb.GetPlayerStatsResp, error) {
	level := int32(1)
	if h.stats != nil {
		var err error
		level, err = h.stats.GetDevelopmentPlayerLevel(uid)
		if err != nil {
			return nil, protocol.NewBizError(protocol.ErrInternal, "load development stats failed")
		}
	}
	archive := &protocolpb.PlayerArchive{SchemaVersion: 1, UnlockedStyles: []int32{1}}
	if h.archives != nil {
		stored, err := h.archives.GetArchive(uid)
		if err != nil && !store.IsNotFound(err) {
			return nil, protocol.NewBizError(protocol.ErrInternal, "load development stats failed")
		}
		if err == nil {
			if err := proto.Unmarshal(stored.Data, archive); err != nil {
				return nil, protocol.NewBizError(protocol.ErrInternal, "decode development stats failed")
			}
		}
	}
	return &protocolpb.GetPlayerStatsResp{
		Level:          level,
		Exp:            archive.Exp,
		Gold:           archive.Gold,
		MaxHp:          int32(h.cfg.MaxHp),
		MaxStamina:     int32(h.cfg.MaxStamina),
		AttackPower:    int32(h.cfg.BaseAttackPower),
		UnlockedStyles: append([]int32(nil), archive.UnlockedStyles...),
	}, nil
}

// UpdatePlayerStats 全量覆盖更新玩家战斗属性（客户端上报完整快照）。
func (h *Handler) UpdatePlayerStats(ctx context.Context, req *protocolpb.UpdatePlayerStatsReq) (*protocolpb.UpdatePlayerStatsResp, error) {
	uid := uidFromCtx(ctx)

	// 基础反作弊：数值边界检查
	if req.Level < 1 || req.Level > 1000 {
		return nil, protocol.NewBizError(protocol.ErrCombatInvalidResult, "等级数值异常")
	}
	if req.MaxHp < 1 || req.MaxHp > 100000 {
		return nil, protocol.NewBizError(protocol.ErrCombatInvalidResult, "生命值异常")
	}

	stats, err := h.mysql.GetPlayerStats(uid)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		stats = &store.PlayerStats{PlayerID: uid}
	} else if err != nil {
		zap.L().Error("查询玩家属性失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "服务器内部错误")
	}

	stats.Level = int(req.Level)
	stats.Exp = int(req.Exp)
	stats.Gold = int(req.Gold)
	stats.MaxHp = int(req.MaxHp)
	stats.MaxStamina = int(req.MaxStamina)
	stats.AttackPower = int(req.AttackPower)

	if err := h.mysql.UpdatePlayerStats(stats); err != nil {
		zap.L().Error("更新玩家属性失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "更新失败")
	}

	// 同步更新解锁的流派
	for _, styleID := range req.UnlockedStyles {
		h.mysql.UnlockPlayerStyle(uid, int(styleID))
	}

	return &protocolpb.UpdatePlayerStatsResp{Success: true}, nil
}

// defaultStats 构造新玩家的默认战斗属性。
func (h *Handler) defaultStats(uid int64) *store.PlayerStats {
	return &store.PlayerStats{
		PlayerID:    uid,
		Level:       1,
		Exp:         0,
		Gold:        0,
		MaxHp:       h.cfg.MaxHp,
		MaxStamina:  h.cfg.MaxStamina,
		AttackPower: h.cfg.BaseAttackPower,
	}
}

// getStyleConfig 根据流派 ID 查找配置，找不到返回 nil。
func getStyleConfig(styleID int) *protocolpb.StyleConfigItem {
	for _, s := range GetStyleConfigs() {
		if int(s.StyleId) == styleID {
			return s
		}
	}
	return nil
}

// uidFromCtx 从会话取玩家 ID。
func uidFromCtx(ctx context.Context) int64 {
	if s := session.FromContext(ctx); s != nil {
		return s.UID()
	}
	return 0
}
