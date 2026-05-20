// Package payment — 支付相关数据模型
//
// 订单是支付系统的核心实体，记录每笔支付的全生命周期：
//   待支付(pending) → 已支付(paid) → 已发货(delivered)
//                    ↘ 已取消(canceled)
//                    ↘ 已退款(refunded)
package payment

// 以下类型定义在 store 包中，通过 store.PaymentOrder 使用
// 这里仅列出常量和辅助函数

// OrderStatus 订单状态常量
const (
	OrderStatusPending   = 0 // 待支付：订单已创建，等待用户付款
	OrderStatusPaid      = 1 // 已支付：微信回调确认收款成功
	OrderStatusDelivered = 2 // 已发货：道具已发放到玩家存档
	OrderStatusCanceled  = 3 // 已取消：用户取消或超时未支付
	OrderStatusRefunded  = 4 // 已退款：用户申请退款成功
)
