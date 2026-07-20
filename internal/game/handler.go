// Package game 游戏核心逻辑模块（pitaya 风格组件）
//
// 负责游戏存档的保存和加载。吸血鬼幸存者类游戏特点：
//   - 游戏逻辑（移动、战斗、AI）主要在客户端运行
//   - 服务器负责：存档持久化、分数提交、资源校验
//   - 存档内容对服务器是不透明的 JSON，由客户端定义结构
//
// 改造要点：handler 签名统一为 func(ctx, *Req) (*Resp, error)，
// 玩家身份通过 session.FromContext(ctx).UID() 获取。
package game

import (
	"context"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler 游戏逻辑处理器（组件）。
type Handler struct {
	mysql *store.MySQLStore
	redis *store.RedisStore
}

// NewHandler 创建游戏逻辑处理器。
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore) *Handler {
	return &Handler{mysql: mysql, redis: redis}
}

// SaveArchive 处理保存存档请求。
//
// 存档数据是客户端生成的 JSON 字符串，服务器不解析其内容。
// 如需防作弊，可在此校验关键字段（金币、解锁内容等）。
func (h *Handler) SaveArchive(ctx context.Context, req *protocol.SaveArchiveReq) (*protocol.SaveArchiveResp, error) {
	uid := uidFromCtx(ctx)
	zap.L().Info("保存存档", zap.Int64("uid", uid), zap.Int("dataLen", len(req.Data)))

	archive := &store.Archive{PlayerID: uid, Data: req.Data}
	if err := h.mysql.SaveArchive(archive); err != nil {
		zap.L().Error("存档保存失败", zap.Int64("uid", uid), zap.Error(err))
		return nil, protocol.NewBizError(protocol.ErrArchiveSaveFailed, "存档保存失败")
	}

	zap.L().Info("存档保存成功", zap.Int64("uid", uid))
	return &protocol.SaveArchiveResp{Success: true}, nil
}

// LoadArchive 处理加载存档请求。
//
// 存档不存在不是错误（新玩家首次进入），返回空字符串。
func (h *Handler) LoadArchive(ctx context.Context, req *protocol.LoadArchiveReq) (*protocol.LoadArchiveResp, error) {
	uid := uidFromCtx(ctx)

	archive, err := h.mysql.GetArchive(uid)
	if err != nil {
		zap.L().Info("存档不存在（新玩家）", zap.Int64("uid", uid))
		return &protocol.LoadArchiveResp{Data: ""}, nil
	}

	zap.L().Info("加载存档成功", zap.Int64("uid", uid), zap.Int("dataLen", len(archive.Data)))
	return &protocol.LoadArchiveResp{Data: archive.Data}, nil
}

// uidFromCtx 从会话取玩家 ID；无会话返回 0（理论上鉴权钩子已拦截未登录请求）。
func uidFromCtx(ctx context.Context) int64 {
	if s := session.FromContext(ctx); s != nil {
		return s.UID()
	}
	return 0
}
