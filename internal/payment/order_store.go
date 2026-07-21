package payment

import "game-server/internal/model"

type OrderStore interface {
	CreateOrder(order *model.PaymentOrder) error
	GetOrderByOrderNo(orderNo string) (*model.PaymentOrder, error)
	UpdateOrderStatus(orderNo string, status int) error
}
