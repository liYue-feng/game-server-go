// Package payment 微信支付模块
//
// 处理微信小游戏的支付流程：
//  1. 客户端发起支付请求 → 服务器创建预支付订单
//  2. 客户端调用 wx.requestPayment 拉起微信支付
//  3. 支付完成后，微信服务器向我们的服务器发送支付回调通知
//  4. 我们验证回调签名 → 更新订单状态 → 发放道具
//
// 安全要点：
//   - 回调通知必须验证签名，防止伪造支付通知
//   - 订单号必须全局唯一，防止重复发放
//   - 发放道具必须幂等（同一订单多次回调只发放一次）
//
// 微信支付 V3 API 文档：
// https://pay.weixin.qq.com/wiki/doc/apiv3/wxpay/pages/index.shtml
package payment

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"time"

	"game-server/internal/config"
	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Handler 支付处理器
type Handler struct {
	mysql    *store.MySQLStore
	redis    *store.RedisStore
	verifier *CallbackVerifier // 回调签名验证器
	wxPay    *WechatPayClient  // 微信支付 API 客户端
}

// NewHandler 创建支付处理器
func NewHandler(mysql *store.MySQLStore, redis *store.RedisStore, cfg *config.WechatConfig) *Handler {
	return &Handler{
		mysql:    mysql,
		redis:    redis,
		verifier: NewCallbackVerifier(cfg),
		wxPay:    NewWechatPayClient(cfg),
	}
}

// HandleCreateOrder 处理创建订单请求
//
// 流程：
//  1. 客户端选择商品后，向服务器请求创建订单
//  2. 服务器生成唯一订单号，写入数据库（状态=待支付）
//  3. 调用微信支付统一下单 API，获取预支付参数
//  4. 返回预支付参数给客户端，客户端用此参数拉起支付
//
// 为什么不客户端直接下单？
//   - 安全：价格由服务器控制，客户端无法篡改
//   - 审计：所有订单在服务端有记录
func (h *Handler) HandleCreateOrder(conn *gateway.Connection, body json.RawMessage) {
	var req CreateOrderReq
	if err := json.Unmarshal(body, &req); err != nil {
		zap.L().Error("创建订单请求解析失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "请求格式错误",
		})
		return
	}

	uid := conn.GetUID()

	// 查找商品信息
	product, ok := ProductMap[req.ProductID]
	if !ok {
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "商品不存在",
		})
		return
	}

	// 生成唯一订单号：时间戳 + 用户ID + 商品ID
	orderNo := generateOrderNo(uid, req.ProductID)

	// 创建订单记录
	order := &store.PaymentOrder{
		OrderNo:   orderNo,
		PlayerID:  uid,
		ProductID: req.ProductID,
		Amount:    product.Price, // 价格由服务器决定，不信任客户端
		Status:    store.OrderStatusPending,
	}

	if err := h.mysql.CreateOrder(order); err != nil {
		zap.L().Error("创建订单失败", zap.Error(err))
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInternal,
			Msg:  "创建订单失败",
		})
		return
	}

	// TODO: 调用微信统一下单 API（需要商户号、证书等配置）
	// 这里先返回订单号，实际预支付参数需要对接微信后生成

	conn.SendMessage(protocol.MsgID_CreateOrderResp, CreateOrderResp{
		OrderNo: orderNo,
	})

	zap.L().Info("创建订单成功",
		zap.Int64("uid", uid),
		zap.String("orderNo", orderNo),
		zap.Int("productID", req.ProductID),
	)
}

// HandlePayCallback 处理微信支付回调通知
//
// 这是微信服务器主动调用我们的 HTTP 接口，不是 WebSocket 消息。
// 微信会在支付成功后反复回调，直到我们返回成功响应。
//
// 必须做：
//   1. 验证签名（防止伪造通知）
//   2. 幂等处理（同一订单只发放一次道具）
//   3. 返回成功响应给微信（否则微信会持续重试）
func (h *Handler) HandlePayCallback(body []byte) (*CallbackResp, error) {
	// 1. 验证签名
	if !h.verifier.Verify(body) {
		zap.L().Warn("支付回调签名验证失败")
		return nil, fmt.Errorf("签名验证失败")
	}

	// 2. 解析回调数据
	var notify CallbackNotify
	if err := json.Unmarshal(body, &notify); err != nil {
		return nil, fmt.Errorf("解析回调数据失败: %w", err)
	}

	zap.L().Info("收到支付回调",
		zap.String("orderNo", notify.OrderNo),
		zap.Int("status", notify.Status),
	)

	// 3. 幂等检查：查询订单状态
	order, err := h.mysql.GetOrderByOrderNo(notify.OrderNo)
	if err != nil {
		return nil, fmt.Errorf("查询订单失败: %w", err)
	}

	// 订单已处理过，直接返回成功
	if order.Status == store.OrderStatusDelivered {
		zap.L().Info("订单已处理，跳过", zap.String("orderNo", notify.OrderNo))
		return &CallbackResp{Code: 0, Message: "成功"}, nil
	}

	// 4. 更新订单状态为已支付
	if err := h.mysql.UpdateOrderStatus(notify.OrderNo, store.OrderStatusPaid); err != nil {
		return nil, fmt.Errorf("更新订单状态失败: %w", err)
	}

	// 5. 发放道具（核心业务逻辑）
	if err := h.deliverProduct(order.PlayerID, order.ProductID); err != nil {
		zap.L().Error("发放道具失败",
			zap.Int64("uid", order.PlayerID),
			zap.Int("productID", order.ProductID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("发放道具失败: %w", err)
	}

	// 6. 更新订单状态为已发货
	if err := h.mysql.UpdateOrderStatus(notify.OrderNo, store.OrderStatusDelivered); err != nil {
		zap.L().Error("更新发货状态失败", zap.Error(err))
	}

	zap.L().Info("支付完成，道具已发放",
		zap.String("orderNo", notify.OrderNo),
		zap.Int64("uid", order.PlayerID),
	)

	return &CallbackResp{Code: 0, Message: "成功"}, nil
}

// deliverProduct 发放道具
// 根据商品ID，在玩家存档中添加对应的道具/货币
// TODO: 实际实现需要根据游戏业务逻辑修改
func (h *Handler) deliverProduct(uid int64, productID int) error {
	product, ok := ProductMap[productID]
	if !ok {
		return fmt.Errorf("未知商品: %d", productID)
	}

	zap.L().Info("发放道具",
		zap.Int64("uid", uid),
		zap.String("product", product.Name),
	)

	// TODO: 修改玩家存档，添加道具
	// 这里需要根据实际游戏逻辑实现，例如：
	// - 解锁角色：修改存档中的 unlockedHeroes 字段
	// - 增加金币：修改存档中的 coins 字段
	// - 购买皮肤：修改存档中的 skins 字段

	return nil
}

// generateOrderNo 生成唯一订单号
// 格式：yyyyMMddHHmmss + uid后6位 + productID后3位
// 实际生产环境建议使用 snowflake 等分布式 ID 生成器
func generateOrderNo(uid int64, productID int) string {
	now := time.Now().Format("20060102150405")
	return fmt.Sprintf("%s%06d%03d", now, uid%1000000, productID%1000)
}

// ========== 消息ID扩展 ==========

const (
	MsgID_CreateOrderReq  uint16 = 5001 // 创建订单请求
	MsgID_CreateOrderResp uint16 = 5002 // 创建订单响应
	MsgID_PayResultNotify uint16 = 5003 // 支付结果通知（服务器主动推送）
)

// ========== 请求/响应结构体 ==========

// CreateOrderReq 创建订单请求
type CreateOrderReq struct {
	ProductID int `json:"product_id"` // 商品ID，见 ProductMap
}

// CreateOrderResp 创建订单响应
type CreateOrderResp struct {
	OrderNo string `json:"order_no"` // 订单号，用于后续支付和查询
}

// CallbackNotify 微信支付回调通知结构（简化版）
// 实际微信 V3 API 的回调结构更复杂，包含加密数据
type CallbackNotify struct {
	OrderNo string `json:"order_no"` // 商户订单号
	Status  int    `json:"status"`   // 支付状态：1=成功
	Amount  int64  `json:"amount"`   // 实际支付金额（分）
}

// CallbackResp 给微信的回调响应
type CallbackResp struct {
	Code    int    `json:"code"`    // 0=成功，非0=失败（微信会重试）
	Message string `json:"message"` // 错误描述
}

// ========== 商品定义 ==========
// 商品表：定义游戏内可购买的商品
// 生产环境应该存在数据库或配置表中，这里简化为代码常量

// Product 商品定义
type Product struct {
	ID    int    // 商品ID
	Name  string // 商品名称
	Price int64  // 价格（单位：分），1元=100分
	Desc  string // 商品描述
}

// ProductMap 商品映射表
var ProductMap = map[int]Product{
	1: {ID: 1, Name: "60钻石", Price: 600, Desc: "60钻石包"},
	2: {ID: 2, Name: "300钻石", Price: 3000, Desc: "300钻石包"},
	3: {ID: 3, Name: "月卡", Price: 2500, Desc: "30天每日领取60钻石"},
	4: {ID: 4, Name: "英雄礼包", Price: 1200, Desc: "解锁随机SSR英雄"},
}

// ========== 签名验证器（存根） ==========
// 实际微信支付 V3 需要使用微信平台证书进行 RSA 签名验证
// 这里提供接口定义，具体实现需要对接微信支付 SDK

// CallbackVerifier 支付回调签名验证器
type CallbackVerifier struct {
	publicKey *rsa.PublicKey // 微信平台公钥，用于验证签名
}

// NewCallbackVerifier 创建签名验证器
func NewCallbackVerifier(cfg *config.WechatConfig) *CallbackVerifier {
	// TODO: 从微信 API 下载平台证书，解析出公钥
	// 实际实现可使用 github.com/wechatpay-apiv3/wechatpay-go 库
	return &CallbackVerifier{}
}

// Verify 验证回调签名
// 返回 true 表示签名合法，false 表示可能是伪造请求
func (v *CallbackVerifier) Verify(body []byte) bool {
	// TODO: 实现微信支付 V3 签名验证
	// 1. 从 HTTP Header 中取出签名和证书序列号
	// 2. 构造验签串：时间戳\n随机串\n请求体\n
	// 3. 用平台公钥验证 SHA256-RSA 签名
	// 临时实现：开发阶段先返回 true
	zap.L().Warn("签名验证未实现，开发阶段跳过验证")
	return true
}

// WechatPayClient 微信支付 API 客户端（存根）
type WechatPayClient struct {
	mchID string // 商户号
}

// NewWechatPayClient 创建微信支付客户端
func NewWechatPayClient(cfg *config.WechatConfig) *WechatPayClient {
	return &WechatPayClient{}
}
