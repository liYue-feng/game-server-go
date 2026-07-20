package game

import (
	"context"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"
)

type Handler struct{ service *ArchiveService }

func NewHandler(archives *store.MySQLStore, _ *store.RedisStore) *Handler {
	return NewHandlerWithService(NewArchiveService(archives))
}
func NewHandlerWithService(service *ArchiveService) *Handler { return &Handler{service: service} }

func (h *Handler) SaveArchive(ctx context.Context, req *protocolpb.SaveArchiveReq) (*protocolpb.SaveArchiveResp, error) {
	if err := h.service.Save(uidFromCtx(ctx), req.GetArchive()); err != nil {
		return nil, protocol.NewBizError(protocol.ErrArchiveSaveFailed, "save archive failed")
	}
	return &protocolpb.SaveArchiveResp{Success: true}, nil
}
func (h *Handler) LoadArchive(ctx context.Context, _ *protocolpb.LoadArchiveReq) (*protocolpb.LoadArchiveResp, error) {
	archive, found, err := h.service.Load(uidFromCtx(ctx))
	if err != nil {
		return nil, protocol.NewBizError(protocol.ErrInternal, "load archive failed")
	}
	return &protocolpb.LoadArchiveResp{Found: found, Archive: archive}, nil
}
func uidFromCtx(ctx context.Context) int64 {
	if s := session.FromContext(ctx); s != nil {
		return s.UID()
	}
	return 0
}
