package payment

import (
	"context"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
)

func TestDisabledHandlerRejectsCreateOrder(t *testing.T) {
	handler := NewDisabledHandler()

	response, err := handler.CreateOrder(context.Background(), &protocolpb.CreateOrderReq{ProductId: 1})
	if response != nil {
		t.Fatalf("CreateOrder() response = %#v, want nil", response)
	}
	business, ok := err.(*protocol.BizError)
	if !ok {
		t.Fatalf("CreateOrder() error = %T %v, want *protocol.BizError", err, err)
	}
	if business.Code != protocol.ErrPaymentUnavailable {
		t.Fatalf("CreateOrder() error code = %d, want %d", business.Code, protocol.ErrPaymentUnavailable)
	}
	if business.Msg != "payment is disabled" {
		t.Fatalf("CreateOrder() error message = %q, want %q", business.Msg, "payment is disabled")
	}
}
