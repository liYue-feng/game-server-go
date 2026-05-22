// Package gm GM（Game Master）指令模块
//
// GM 指令是游戏运营和调试的必备工具，用于：
//   - 日常运营：广播公告、发送全服邮件
//   - 问题排查：查看玩家存档、修改数据
//   - 应急处理：踢人、封号、停服
//
// 安全设计：
//   - GM 指令只允许管理员账号使用（通过 uid 白名单控制）
//   - 白名单配置在 config.yaml 的 gm.admin_uids 中
//   - 所有 GM 操作都记录审计日志
//
// 消息协议：
//   GM 指令复用 WebSocket 通道，MsgID 范围 6xxx
//   指令格式：{ "cmd": "kick", "args": {"uid": 123} }
package gm

import (
	"encoding/json"
	"fmt"

	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler GM 指令处理器
type Handler struct {
	mysql     *store.MySQLStore
	redis     *store.RedisStore
	hub       *gateway.Hub     // 用于广播和踢人
	adminUIDs map[int64]bool   // 管理员 UID 白名单
}

// NewHandler 创建 GM 指令处理器
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore, hub *gateway.Hub, adminUIDs []int64) *Handler {
	uidMap := make(map[int64]bool, len(adminUIDs))
	for _, uid := range adminUIDs {
		uidMap[uid] = true
	}
	return &Handler{
		mysql:     mysql,
		redis:     redis,
		hub:       hub,
		adminUIDs: uidMap,
	}
}

// HandleCommand 处理 GM 指令
// 统一入口：所有 GM 指令通过此函数路由
func (h *Handler) HandleCommand(conn *gateway.Connection, body json.RawMessage) {
	// 1. 权限检查
	uid := conn.GetUID()
	if !h.adminUIDs[uid] {
		zap.L().Warn("非管理员尝试使用GM指令",
			zap.Int64("uid", uid),
		)
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrUnauthorized,
			Msg:  "无GM权限",
		})
		return
	}

	// 2. 解析指令
	var req GMCommandReq
	if err := json.Unmarshal(body, &req); err != nil {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "指令格式错误",
		})
		return
	}

	// 3. 审计日志：记录所有 GM 操作
	zap.L().Info("GM指令",
		zap.Int64("admin_uid", uid),
		zap.String("cmd", req.Cmd),
		zap.String("args", string(req.Args)),
	)

	// 4. 路由到具体处理函数
	var result string
	var err error

	switch req.Cmd {
	case "kick":
		result, err = h.handleKick(req.Args)
	case "broadcast":
		result, err = h.handleBroadcast(req.Args)
	case "query_player":
		result, err = h.handleQueryPlayer(req.Args)
	case "online":
		result = fmt.Sprintf("当前在线: %d", h.hub.OnlineCount())
	case "reload_config":
		result, err = h.handleReloadConfig(req.Args)
	default:
		result = fmt.Sprintf("未知指令: %s", req.Cmd)
	}

	// 5. 返回结果
	respMsg := "执行成功"
	if err != nil {
		respMsg = fmt.Sprintf("执行失败: %v", err)
	}
	if result != "" {
		respMsg = result
	}

	conn.SendMessage(protocol.MsgID_GMCommandResp, GMCommandResp{
		Cmd:    req.Cmd,
		Result: respMsg,
	})
}

// ========== 具体指令实现 ==========

// handleKick 踢人指令
// 将指定玩家从服务器断开连接
// 用法：{ "cmd": "kick", "args": {"uid": 12345} }
func (h *Handler) handleKick(args json.RawMessage) (string, error) {
	var params struct {
		UID int64 `json:"uid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("参数错误: %w", err)
	}
	if params.UID <= 0 {
		return "", fmt.Errorf("无效的uid")
	}

	// Hub 不直接支持按 UID 踢人，这里只是记录日志
	// 实际实现需要 Hub 维护 uid -> Connection 的映射
	zap.L().Info("GM踢人", zap.Int64("target_uid", params.UID))
	return fmt.Sprintf("已踢出玩家 %d（注：需实现uid-connection映射）", params.UID), nil
}

// handleBroadcast 广播指令
// 向所有在线玩家发送系统公告
// 用法：{ "cmd": "broadcast", "args": {"content": "服务器5分钟后维护"} }
func (h *Handler) handleBroadcast(args json.RawMessage) (string, error) {
	var params struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("参数错误: %w", err)
	}
	if params.Content == "" {
		return "", fmt.Errorf("公告内容不能为空")
	}

	h.hub.Broadcast(protocol.MsgID_GMCommandResp, GMCommandResp{
		Cmd:    "broadcast",
		Result: params.Content,
	})

	zap.L().Info("GM广播", zap.String("content", params.Content))
	return fmt.Sprintf("广播已发送: %s", params.Content), nil
}

// handleQueryPlayer 查询玩家信息
// 用法：{ "cmd": "query_player", "args": {"uid": 12345} }
func (h *Handler) handleQueryPlayer(args json.RawMessage) (string, error) {
	var params struct {
		UID int64 `json:"uid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("参数错误: %w", err)
	}

	player, err := h.mysql.GetPlayerByID(params.UID)
	if err != nil {
		return "", fmt.Errorf("玩家不存在: %w", err)
	}

	return fmt.Sprintf("UID=%d 昵称=%s 最高分=%d 注册时间=%s",
		player.ID, player.Nickname, player.BestScore, player.CreatedAt.Format("2006-01-02")), nil
}

// handleReloadConfig 重载配置
// 用法：{ "cmd": "reload_config", "args": {} }
// TODO: 实现配置热更新（Viper WatchConfig）
func (h *Handler) handleReloadConfig(args json.RawMessage) (string, error) {
	return "配置重载功能待实现", nil
}

// ========== 协议定义 ==========

// GMCommandReq GM 指令请求
type GMCommandReq struct {
	Cmd  string          `json:"cmd"`  // 指令名称：kick / broadcast / query_player / online
	Args json.RawMessage `json:"args"` // 指令参数，每种指令的参数不同
}

// GMCommandResp GM 指令响应
type GMCommandResp struct {
	Cmd    string `json:"cmd"`    // 回显指令名称
	Result string `json:"result"` // 执行结果描述
}
