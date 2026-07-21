package gm

import (
	"context"
	"encoding/json"
	"fmt"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"
	"game-server/internal/transport"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type Handler struct {
	mysql     *store.MySQLStore
	redis     *store.RedisStore
	hub       Broadcaster
	adminUIDs map[int64]bool
}
type Broadcaster interface {
	Broadcast(uint16, proto.Message)
	OnlineCount() int
}

func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore, hub *transport.Hub, adminUIDs []int64) *Handler {
	allowed := make(map[int64]bool, len(adminUIDs))
	for _, uid := range adminUIDs {
		allowed[uid] = true
	}
	return &Handler{mysql: mysql, redis: redis, hub: hub, adminUIDs: allowed}
}

// Command is the only WebSocket GM boundary. ArgsJson intentionally remains
// JSON because it is command-specific opaque data inside this handler.
func (h *Handler) Command(ctx context.Context, req *protocolpb.GMCommandReq) (*protocolpb.GMCommandResp, error) {
	uid := uidFromCtx(ctx)
	if !h.adminUIDs[uid] {
		return nil, protocol.NewBizError(protocol.ErrUnauthorized, "GM permission required")
	}
	var result string
	var err error
	switch req.Cmd {
	case "kick":
		result, err = h.handleKick(req.ArgsJson)
	case "broadcast":
		result, err = h.handleBroadcast(req.ArgsJson)
	case "query_player":
		result, err = h.handleQueryPlayer(req.ArgsJson)
	case "online":
		result = fmt.Sprintf("online: %d", h.hub.OnlineCount())
	case "reload_config":
		result, err = h.handleReloadConfig(req.ArgsJson)
	default:
		result = fmt.Sprintf("unknown command: %s", req.Cmd)
	}
	if err != nil {
		result = fmt.Sprintf("execution failed: %v", err)
	}
	return &protocolpb.GMCommandResp{Cmd: req.Cmd, Result: result}, nil
}

func (h *Handler) handleKick(args json.RawMessage) (string, error) {
	var params struct {
		UID int64 `json:"uid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.UID <= 0 {
		return "", fmt.Errorf("invalid uid")
	}
	zap.L().Info("GM kick requested", zap.Int64("target_uid", params.UID))
	return fmt.Sprintf("kick requested for %d", params.UID), nil
}

func (h *Handler) handleBroadcast(args json.RawMessage) (string, error) {
	var params struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Content == "" {
		return "", fmt.Errorf("broadcast content is empty")
	}
	h.hub.Broadcast(protocol.MsgID_GMCommandResp, &protocolpb.GMCommandResp{Cmd: "broadcast", Result: params.Content})
	return fmt.Sprintf("broadcast sent: %s", params.Content), nil
}

func (h *Handler) handleQueryPlayer(args json.RawMessage) (string, error) {
	var params struct {
		UID int64 `json:"uid"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	player, err := h.mysql.GetPlayerByID(params.UID)
	if err != nil {
		return "", fmt.Errorf("get player: %w", err)
	}
	return fmt.Sprintf("UID=%d nickname=%s best_score=%d", player.ID, player.Nickname, player.BestScore), nil
}

func (h *Handler) handleReloadConfig(json.RawMessage) (string, error) {
	return "reload config is not implemented", nil
}
func uidFromCtx(ctx context.Context) int64 {
	if current := session.FromContext(ctx); current != nil {
		return current.UID()
	}
	return 0
}
