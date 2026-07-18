// Package game 游戏核心逻辑模块
//
// 负责处理游戏存档的保存和加载。
// 吸血鬼幸存者类游戏的特点：
//   - 游戏逻辑（移动、战斗、AI）主要在客户端运行
//   - 服务器负责：存档持久化、分数提交、资源校验
//   - 存档内容对服务器是不透明的 JSON，由客户端定义结构
//
// 这种"客户端权威 + 服务器存档"的模式适合单机体验的游戏。
// 如果是竞技类游戏，则需要"服务器权威"模式，逻辑由服务器驱动。
package game

import (
	"encoding/json"

	"game-server/internal/gateway"
	"game-server/internal/protocol"

	"go.uber.org/zap"
)

// Handler 游戏逻辑处理器
type Handler struct {
	service *ArchiveService
}

// NewHandler 创建游戏逻辑处理器
func NewHandler(service *ArchiveService) *Handler {
	return &Handler{service: service}
}

// HandleSaveArchive 处理保存存档请求
//
// 流程：
//  1. 校验玩家身份（从连接获取 uid）
//  2. 将存档数据写入 MySQL
//  3. 返回保存结果
//
// 注意：存档数据是客户端生成的 JSON 字符串，服务器不解析其内容。
// 如果需要防作弊，可以在服务器端校验关键字段（金币、解锁内容等）。
func (h *Handler) HandleSaveArchive(conn *gateway.Connection, body json.RawMessage) {
	var req protocol.SaveArchiveReq
	if err := json.Unmarshal(body, &req); err != nil {
		zap.L().Error("保存存档请求解析失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "请求格式错误",
		})
		return
	}

	uid := conn.GetUID()
	zap.L().Info("保存存档",
		zap.Int64("uid", uid),
		zap.Int("dataLen", len(req.Data)),
	)

	if err := h.service.Save(uid, req.Data); err != nil {
		zap.L().Error("存档保存失败",
			zap.Int64("uid", uid),
			zap.Error(err),
		)
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrArchiveSaveFailed,
			Msg:  "存档保存失败",
		})
		return
	}

	conn.SendMessage(protocol.MsgID_SaveArchiveResp, protocol.SaveArchiveResp{
		Success: true,
	})

	zap.L().Info("存档保存成功", zap.Int64("uid", uid))
}

// HandleLoadArchive 处理加载存档请求
//
// 流程：
//  1. 从 MySQL 读取存档
//  2. 存档不存在则返回空字符串（新玩家首次进入）
//  3. 返回存档数据
func (h *Handler) HandleLoadArchive(conn *gateway.Connection, body json.RawMessage) {
	uid := conn.GetUID()

	data, err := h.service.Load(uid)
	if err != nil {
		zap.L().Error("加载存档失败", zap.Int64("uid", uid), zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInternal,
			Msg:  "加载存档失败",
		})
		return
	}

	conn.SendMessage(protocol.MsgID_LoadArchiveResp, protocol.LoadArchiveResp{
		Data: data,
	})

	zap.L().Info("加载存档成功",
		zap.Int64("uid", uid),
		zap.Int("dataLen", len(data)),
	)
}
