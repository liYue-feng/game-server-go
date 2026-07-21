// Package payment exposes the reserved payment protocol boundary.
package payment

import (
	"context"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
)

// Handler rejects payment requests until a complete secure provider exists.
type Handler struct{}

func NewDisabledHandler() *Handler {
	return &Handler{}
}

func (h *Handler) CreateOrder(context.Context, *protocolpb.CreateOrderReq) (*protocolpb.CreateOrderResp, error) {
	return nil, protocol.NewBizError(protocol.ErrPaymentUnavailable, "payment is disabled")
}
