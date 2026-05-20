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
	"fmt"

	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler 登录处理器
type Handler struct {
	mysql *store.MySQLStore
	redis *store.RedisStore
	wx    *WechatClient // 微信 API 客户端
}

// NewHandler 创建登录处理器
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore, wx *WechatClient) *Handler {
	return &Handler{
		mysql: mysql,
		redis: redis,
		wx:    wx,
	}
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

	// 2. 调用微信 API 获取 openid
	result, err := h.wx.Code2Session(req.Code)
	if err != nil {
		zap.L().Error("微信登录失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrLoginWechatFailed,
			Msg:  "微信登录失败",
		})
		return
	}

	// 3. 查找或创建玩家
	player, err := h.mysql.GetPlayerByOpenID(result.OpenID)
	if err != nil {
		// 玩家不存在，自动注册
		player, err = h.registerPlayer(result.OpenID)
		if err != nil {
			zap.L().Error("注册玩家失败", zap.Error(err))
			conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrInternal,
				Msg:  "注册失败",
			})
			return
		}
		zap.L().Info("新玩家注册", zap.Int64("uid", player.ID))
	}

	// 4. 生成 token
	token, err := generateToken()
	if err != nil {
		zap.L().Error("生成 token 失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInternal,
			Msg:  "登录失败",
		})
		return
	}

	// 更新 token 到数据库
	player.Token = token
	if err := h.mysql.UpdatePlayer(player); err != nil {
		zap.L().Error("更新 token 失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInternal,
			Msg:  "登录失败",
		})
		return
	}

	// 5. 写入 Redis 会话缓存
	if err := h.redis.SetSession(player.ID, &store.SessionData{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}); err != nil {
		// Redis 写入失败不应阻止登录，记录日志即可
		// 下次请求会走 MySQL 验证
		zap.L().Error("写入会话缓存失败", zap.Error(err))
	}

	// 6. 设置连接的玩家信息
	conn.SetPlayerInfo(player.ID, token)

	// 7. 返回登录成功响应
	conn.SendMessage(protocol.MsgID_LoginResp, protocol.LoginResp{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	})

	zap.L().Info("玩家登录成功",
		zap.Int64("uid", player.ID),
		zap.String("nickname", player.Nickname),
	)
}

// registerPlayer 注册新玩家
func (h *Handler) registerPlayer(openID string) (*store.Player, error) {
	// 注意：这里引用了 model.Player，但为了避免循环导入，
	// 我们直接使用 store 层返回的类型
	// 实际实现中，model 和 store 应该在同一层
	player := &store.Player{
		OpenID:   openID,
		Nickname: "", // 昵称暂时留空，登录后由客户端设置
		Token:    "",
	}

	if err := h.mysql.CreatePlayer(player); err != nil {
		return nil, fmt.Errorf("创建玩家失败: %w", err)
	}

	// 设置默认昵称
	player.Nickname = fmt.Sprintf("玩家%d", player.ID)
	if err := h.mysql.UpdatePlayer(player); err != nil {
		return nil, fmt.Errorf("设置昵称失败: %w", err)
	}

	return player, nil
}

// HandleHeartbeat 处理心跳请求
// 心跳的作用：保持连接活跃，检测客户端是否在线
func (h *Handler) HandleHeartbeat(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.HeartbeatReq
	if err := json.Unmarshal(body, &req); err != nil {
		return // 心跳解析失败不值得发错误响应
	}

	// 刷新 Redis 会话过期时间
	if uid := conn.GetUID(); uid > 0 {
		_ = h.redis.SetSession(uid, &store.SessionData{
			Uid:      uid,
			Nickname: "",
			Token:    "",
		})
	}

	// 返回服务器时间戳
	conn.SendMessage(protocol.MsgID_HeartbeatResp, protocol.HeartbeatResp{
		Timestamp: req.Timestamp, // 回显客户端时间戳，客户端可计算 RTT
	})
}

// generateToken 生成随机会话令牌
// 使用 crypto/rand 生成 32 字节随机数，比 math/rand 更安全
// 为什么不用 JWT？JWT 需要密钥管理，且无法主动撤销。
//   对于游戏服务器，简单的随机 token + Redis 缓存已足够
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
