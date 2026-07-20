package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

const (
	SessionKeyNickname = "nickname"
	SessionKeyToken    = "token"
)

type Handler struct {
	service *LoginService
}

// NewHandler keeps the current server bootstrap compatible while business
// logic is delegated to repository-backed services.
func NewHandler(players *store.MySQLStore, sessions *store.RedisStore, wechat *WechatClient) *Handler {
	return NewHandlerWithService(NewLoginService(players, sessions, NewWechatCodeExchanger(wechat), GenerateToken))
}

func NewHandlerWithService(service *LoginService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(ctx context.Context, req *protocolpb.LoginReq) (*protocolpb.LoginResp, error) {
	if strings.HasPrefix(req.Code, "dev:") {
		zap.L().Info("development login request", zap.String("code", req.Code))
	}

	result, err := h.service.Login(req.Code)
	if err != nil {
		code, message := classifyLoginError(err)
		zap.L().Error("登录失败",
			zap.String("stage", string(loginOperationStage(err))),
			zap.Int("bizCode", code),
		)
		return nil, protocol.NewBizError(code, message)
	}

	if current := session.FromContext(ctx); current != nil {
		current.Bind(result.UID)
		current.Set(SessionKeyNickname, result.Nickname)
		current.Set(SessionKeyToken, result.Token)
	}

	zap.L().Info("玩家登录成功", zap.Int64("uid", result.UID), zap.String("nickname", result.Nickname))
	return &protocolpb.LoginResp{
		Uid:      result.UID,
		Nickname: result.Nickname,
		Token:    result.Token,
	}, nil
}

func classifyLoginError(err error) (int, string) {
	if errors.Is(err, ErrInvalidLoginCode) {
		return protocol.ErrLoginInvalidCode, "登录凭证无效"
	}
	if isExchangeError(err) {
		return protocol.ErrLoginWechatFailed, "微信登录服务失败"
	}
	return protocol.ErrInternal, "登录失败"
}

func (h *Handler) Heartbeat(ctx context.Context, req *protocolpb.HeartbeatReq) (*protocolpb.HeartbeatResp, error) {
	if current := session.FromContext(ctx); current != nil && current.UID() > 0 {
		if err := h.service.RefreshSession(current.UID()); err != nil {
			return nil, fmt.Errorf("refresh session for uid %d: %w", current.UID(), err)
		}
	}
	return &protocolpb.HeartbeatResp{Timestamp: req.Timestamp}, nil
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
