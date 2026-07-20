// Package login 登录模块（pitaya 风格组件）
//
// 负责微信小游戏登录流程：
//  1. 客户端调用 wx.login() 获取临时 code
//  2. 客户端把 code 发给服务器
//  3. 服务器用 code 向微信 API 换取 openid
//  4. 服务器按 openid 查找或创建玩家
//  5. 服务器生成 token，绑定会话，返回给客户端
//
// 改造要点（对齐 pitaya component）：
//   - handler 签名统一为 func(ctx, *Req) (*Resp, error)
//   - 玩家身份通过 session.Session 承载：登录成功后 s.Bind(uid)
//   - 错误用 protocol.NewBizError 返回，由 kernel 统一编码为错误帧
package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// 会话业务态的 key：登录成功后写入 nickname 与 token，供后续鉴权/展示使用。
const (
	SessionKeyNickname = "nickname"
	SessionKeyToken    = "token"
)

// Handler 登录处理器（组件）。
type Handler struct {
	mysql *store.MySQLStore
	redis *store.RedisStore
	wx    *WechatClient
}

// NewHandler 创建登录处理器。
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore, wx *WechatClient) *Handler {
	return &Handler{mysql: mysql, redis: redis, wx: wx}
}

// Login 处理登录请求。
//
// 完整流程见包注释。任何业务失败都返回 *protocol.BizError，
// 系统性失败（DB/Redis）返回普通 error（kernel 会归一为 ErrInternal）。
func (h *Handler) Login(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
	zap.L().Info("收到登录请求", zap.String("code", req.Code))

	// 1. 用微信 code 换取 openid
	result, err := h.wx.Code2Session(req.Code)
	if err != nil {
		zap.L().Error("微信登录失败", zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrLoginWechatFailed, "微信登录失败")
	}

	// 2. 查找或创建玩家
	player, err := h.mysql.GetPlayerByOpenID(result.OpenID)
	if err != nil {
		player, err = h.registerPlayer(result.OpenID)
		if err != nil {
			zap.L().Error("注册玩家失败", zap.Error(err))
			return nil, protocol.NewBizError(protocol.ErrInternal, "注册失败")
		}
		zap.L().Info("新玩家注册", zap.Int64("uid", player.ID))
	}

	// 3. 生成 token
	token, err := generateToken()
	if err != nil {
		zap.L().Error("生成 token 失败", zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "登录失败")
	}

	// 4. 更新 token 到数据库
	player.Token = token
	if err := h.mysql.UpdatePlayer(player); err != nil {
		zap.L().Error("更新 token 失败", zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "登录失败")
	}

	// 5. 写入 Redis 会话缓存（失败不阻断登录，下次请求会走 MySQL 校验）
	if err := h.redis.SetSession(player.ID, &store.SessionData{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}); err != nil {
		zap.L().Error("写入会话缓存失败", zap.Error(err))
	}

	// 6. 绑定会话（对齐 pitaya session.Bind）
	if s := session.FromContext(ctx); s != nil {
		s.Bind(player.ID)
		s.Set(SessionKeyNickname, player.Nickname)
		s.Set(SessionKeyToken, token)
	}

	zap.L().Info("玩家登录成功", zap.Int64("uid", player.ID), zap.String("nickname", player.Nickname))

	return &protocol.LoginResp{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}, nil
}

// registerPlayer 注册新玩家：先建号拿到自增 ID，再用 ID 生成默认昵称。
func (h *Handler) registerPlayer(openID string) (*store.Player, error) {
	player := &store.Player{OpenID: openID}
	if err := h.mysql.CreatePlayer(player); err != nil {
		return nil, fmt.Errorf("创建玩家失败: %w", err)
	}
	player.Nickname = fmt.Sprintf("玩家%d", player.ID)
	if err := h.mysql.UpdatePlayer(player); err != nil {
		return nil, fmt.Errorf("设置昵称失败: %w", err)
	}
	return player, nil
}

// Heartbeat 处理心跳请求：刷新 Redis 会话 TTL，回显时间戳供客户端估算 RTT。
//
// 心跳的作用是保持连接活跃、探测客户端是否在线。
func (h *Handler) Heartbeat(ctx context.Context, req *protocol.HeartbeatReq) (*protocol.HeartbeatResp, error) {
	if s := session.FromContext(ctx); s != nil {
		if uid := s.UID(); uid > 0 {
			// 刷新会话 TTL：优先保留原有 nickname/token。
			existing, _ := h.redis.GetSession(uid)
			if existing != nil {
				_ = h.redis.SetSession(uid, existing)
			} else {
				_ = h.redis.SetSession(uid, &store.SessionData{Uid: uid})
			}
		}
	}
	return &protocol.HeartbeatResp{Timestamp: req.Timestamp}, nil
}

// generateToken 生成随机会话令牌（32 字节 crypto/rand）。
//
// 为什么不用 JWT？JWT 需要密钥管理且无法主动撤销；对游戏服务器而言，
// 随机 token + Redis 缓存已足够，且能通过删除会话立即失效。
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
