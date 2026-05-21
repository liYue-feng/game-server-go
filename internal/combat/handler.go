package combat

import (
	"encoding/json"
	"log"

	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"
)

// Handler 战斗模块处理器
// 遵循项目统一的 handler 模式：持有 store 引用，方法签名为 (conn, body)
type Handler struct {
	mysql *store.MySQLStore
	redis *store.RedisStore
	cfg   *CombatConfig
}

// NewHandler 创建战斗模块处理器
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore) *Handler {
	return &Handler{
		mysql: mysql,
		redis: redis,
		cfg:   DefaultCombatConfig(),
	}
}

// HandleCombatResult 处理战斗结算请求
// 客户端在地牢通关或角色死亡后上报本局数据。
// 服务器做基础反作弊校验，计算奖励。
func (h *Handler) HandleCombatResult(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.CombatResultReq
	if err := json.Unmarshal(body, &req); err != nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "参数解析失败",
		})
		return
	}

	// 基础反作弊校验
	if err := validateCombatResult(&req, h.cfg); err != nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrCombatCheatDetected,
			Msg:  err.Error(),
		})
		return
	}

	// 计算奖励
	rewardGold := req.Kills * h.cfg.GoldPerKill
	rewardExp := req.Kills * h.cfg.ExpPerKill

	// TODO: 更新玩家数据（金币、经验、最高分）
	// 需要 PlayerStats 表，阶段5实现

	// TODO: 记录地牢运行历史
	// 需要 DungeonRun 表，阶段3实现

	conn.SendMessage(protocol.MsgID_CombatResultResp, protocol.CombatResultResp{
		Success:    true,
		RewardGold: rewardGold,
		RewardExp:  rewardExp,
		BestScore:  int64(req.Score), // 暂时返回本局分数
	})
}

// HandleGetEnemyConfigs 返回敌人配置表
func (h *Handler) HandleGetEnemyConfigs(conn *gateway.Connection, body json.RawMessage) {
	configs := GetEnemyConfigs()
	conn.SendMessage(protocol.MsgID_GetEnemyConfigsResp, protocol.GetEnemyConfigsResp{
		Configs: configs,
	})
}

// HandleGetDungeonConfig 返回地牢配置
func (h *Handler) HandleGetDungeonConfig(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.GetDungeonConfigReq
	if err := json.Unmarshal(body, &req); err != nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "参数解析失败",
		})
		return
	}

	if req.Level <= 0 {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrCombatConfigNotFound,
			Msg:  "无效的地牢等级",
		})
		return
	}

	cfg := GetDungeonConfig(req.Level)
	if cfg == nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrCombatConfigNotFound,
			Msg:  "地牢配置不存在",
		})
		return
	}

	conn.SendMessage(protocol.MsgID_GetDungeonConfigResp, protocol.GetDungeonConfigResp{
		Level:        cfg.Level,
		RoomCount:    cfg.RoomCount,
		EnemyDensity: cfg.EnemyDensity,
		BossID:       cfg.BossID,
		EnemyConfigs: GetEnemyConfigs(),
	})
}

// HandleGetStyleConfigs 返回流派配置表
func (h *Handler) HandleGetStyleConfigs(conn *gateway.Connection, body json.RawMessage) {
	styles := GetStyleConfigs()
	conn.SendMessage(protocol.MsgID_GetStyleConfigsResp, protocol.GetStyleConfigsResp{
		Styles: styles,
	})
}

// HandleUnlockStyle 处理流派解锁请求
// TODO: 阶段4实现完整逻辑（检查金币、记录解锁状态）
func (h *Handler) HandleUnlockStyle(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.UnlockStyleReq
	if err := json.Unmarshal(body, &req); err != nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "参数解析失败",
		})
		return
	}

	// 检查流派是否存在
	validStyle := false
	for _, s := range GetStyleConfigs() {
		if s.StyleID == req.StyleID {
			validStyle = true
			break
		}
	}
	if !validStyle {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrCombatStyleLocked,
			Msg:  "流派不存在",
		})
		return
	}

	// TODO: 检查玩家金币是否足够
	// TODO: 记录解锁状态到 PlayerStyle 表
	goldCost := 100 // 暂定解锁费用

	conn.SendMessage(protocol.MsgID_UnlockStyleResp, protocol.UnlockStyleResp{
		Success:  true,
		GoldCost: goldCost,
	})

	log.Printf("[combat] 玩家解锁流派 style_id=%d, cost=%d", req.StyleID, goldCost)
}

// HandleGetPlayerStats 获取玩家战斗属性
// TODO: 阶段5实现，从 PlayerStats 表读取
func (h *Handler) HandleGetPlayerStats(conn *gateway.Connection, body json.RawMessage) {
	uid := conn.GetUID()

	// 暂时返回默认值
	conn.SendMessage(protocol.MsgID_GetPlayerStatsResp, protocol.GetPlayerStatsResp{
		Level:          1,
		Exp:            0,
		Gold:           0,
		MaxHp:          h.cfg.MaxHp,
		MaxStamina:     h.cfg.MaxStamina,
		AttackPower:    h.cfg.BaseAttackPower,
		UnlockedStyles: []int{1}, // 默认解锁"刃"流派
	})

	log.Printf("[combat] 获取玩家属性 uid=%d", uid)
}

// HandleUpdatePlayerStats 更新玩家战斗属性
// TODO: 阶段5实现，写入 PlayerStats 表
func (h *Handler) HandleUpdatePlayerStats(conn *gateway.Connection, body json.RawMessage) {
	conn.SendMessage(protocol.MsgID_UpdatePlayerStatsResp, protocol.UpdatePlayerStatsResp{
		Success: true,
	})
}
