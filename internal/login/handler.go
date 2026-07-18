// Package login 登录模块
//
// 负责处理微信小游戏登录流程：
//  1. 客户端调用 wx.login() 获取临时 code
//  2. 客户端将 code 发送给服务器
//  3. 服务器用 code 向微信 API 换取 openid + session_key
//  4. 服务器根据 openid 查找或创建玩家
//  5. 服务器生成 token 返回给客户端
//
// token 的设计：
//   - 使用 UUID v4 生成，保证唯一性
//   - 存储在 MySQL players 表和 Redis 会话缓存中
//   - 客户端每次重连需要重新登录（token 随会话刷新）
package login

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"game-server/internal/gateway"
	"game-server/internal/protocol"

	"go.uber.org/zap"
)

// Handler 登录处理器
type Handler struct {
	service *LoginService
}

// NewHandler 创建登录处理器
func NewHandler(service *LoginService) *Handler {
	return &Handler{service: service}
}

// HandleLogin 处理登录请求
//
// 完整流程：
//  1. 解析客户端发来的微信 code
//  2. 调用微信 code2session API 获取 openid
//  3. 根据 openid 查找玩家，不存在则自动注册
//  4. 生成 token，写入 MySQL + Redis
//  5. 返回登录响应
func (h *Handler) HandleLogin(conn *gateway.Connection, body json.RawMessage) {
	// 1. 解析请求
	var req protocol.LoginReq
	if err := json.Unmarshal(body, &req); err != nil {
		zap.L().Error("登录请求解析失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "请求格式错误",
		})
		return
	}

	zap.L().Info("收到登录请求", zap.String("code", req.Code))

	result, err := h.service.Login(req.Code)
	if err != nil {
		code := protocol.ErrInternal
		message := "登录失败"
		if isExchangeError(err) {
			code = protocol.ErrLoginInvalidCode
			message = "登录凭证无效"
		}
		zap.L().Error("登录失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: code,
			Msg:  message,
		})
		return
	}

	conn.SetPlayerInfo(result.UID, result.Token)

	conn.SendMessage(protocol.MsgID_LoginResp, protocol.LoginResp{
		Uid:      result.UID,
		Nickname: result.Nickname,
		Token:    result.Token,
	})

	zap.L().Info("玩家登录成功",
		zap.Int64("uid", result.UID),
		zap.String("nickname", result.Nickname),
	)
}

// HandleHeartbeat 处理心跳请求
// 心跳的作用：保持连接活跃，检测客户端是否在线
func (h *Handler) HandleHeartbeat(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.HeartbeatReq
	if err := json.Unmarshal(body, &req); err != nil {
		return // 心跳解析失败不值得发错误响应
	}

	if uid := conn.GetUID(); uid > 0 {
		if err := h.service.RefreshSession(uid); err != nil {
			zap.L().Error("刷新会话失败", zap.Int64("uid", uid), zap.Error(err))
		}
	}

	// 返回服务器时间戳
	conn.SendMessage(protocol.MsgID_HeartbeatResp, protocol.HeartbeatResp{
		Timestamp: req.Timestamp, // 回显客户端时间戳，客户端可计算 RTT
	})
}

// GenerateToken 生成随机会话令牌
// 使用 crypto/rand 生成 32 字节随机数，比 math/rand 更安全
// 为什么不用 JWT？JWT 需要密钥管理，且无法主动撤销。
//
//	对于游戏服务器，简单的随机 token + Redis 缓存已足够
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
