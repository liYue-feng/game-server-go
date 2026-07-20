package game

import (
	"context"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

type Handler struct {
	service *ArchiveService
}

// NewHandler keeps the current server bootstrap compatible while business
// logic is delegated to the archive service.
func NewHandler(archives *store.MySQLStore, _ *store.RedisStore) *Handler {
	return NewHandlerWithService(NewArchiveService(archives))
}

func NewHandlerWithService(service *ArchiveService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SaveArchive(ctx context.Context, req *protocol.SaveArchiveReq) (*protocol.SaveArchiveResp, error) {
	uid := uidFromCtx(ctx)
	zap.L().Info("保存存档", zap.Int64("uid", uid), zap.Int("dataLen", len(req.Data)))
	if err := h.service.Save(uid, req.Data); err != nil {
		zap.L().Error("存档保存失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrArchiveSaveFailed, "存档保存失败")
	}
	return &protocol.SaveArchiveResp{Success: true}, nil
}

func (h *Handler) LoadArchive(ctx context.Context, req *protocol.LoadArchiveReq) (*protocol.LoadArchiveResp, error) {
	uid := uidFromCtx(ctx)
	data, err := h.service.Load(uid)
	if err != nil {
		zap.L().Error("加载存档失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrInternal, "加载存档失败")
	}
	return &protocol.LoadArchiveResp{Data: data}, nil
}

func uidFromCtx(ctx context.Context) int64 {
	if current := session.FromContext(ctx); current != nil {
		return current.UID()
	}
	return 0
}
