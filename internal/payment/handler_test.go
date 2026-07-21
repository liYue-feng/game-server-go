package payment

import (
	"errors"
	"testing"

	"game-server/internal/model"
	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/store"

	"google.golang.org/protobuf/proto"
)

type fakeOrderStore struct {
	order        *model.PaymentOrder
	deliveredErr error
	statuses     []int
}

func (s *fakeOrderStore) CreateOrder(*model.PaymentOrder) error                 { return nil }
func (s *fakeOrderStore) GetOrderByOrderNo(string) (*model.PaymentOrder, error) { return s.order, nil }
func (s *fakeOrderStore) UpdateOrderStatus(_ string, status int) error {
	s.statuses = append(s.statuses, status)
	if status == store.OrderStatusDelivered {
		return s.deliveredErr
	}
	return nil
}

type fakePusher struct {
	pushed  bool
	uid     int64
	msgID   uint16
	payload proto.Message
	online  bool
}

func (p *fakePusher) PushToUID(uid int64, msgID uint16, payload proto.Message) bool {
	p.pushed = true
	p.uid = uid
	p.msgID = msgID
	p.payload = payload
	return p.online
}

func callbackHandlerForTest(store OrderStore, pusher UIDPusher) *Handler {
	return &Handler{orders: store, pusher: pusher, verifier: &CallbackVerifier{}}
}

func TestPaymentDeliveryFailureReturnsErrorAndDoesNotPush(t *testing.T) {
	pusher := &fakePusher{online: true}
	h := callbackHandlerForTest(&fakeOrderStore{order: &model.PaymentOrder{OrderNo: "o", PlayerID: 7, ProductID: 1}, deliveredErr: errors.New("write failed")}, pusher)
	if _, err := h.HandlePayCallback([]byte(`{"order_no":"o","status":1}`)); err == nil {
		t.Fatal("callback succeeded after delivered-state failure")
	}
	if pusher.pushed {
		t.Fatal("callback pushed before the delivered state was stored")
	}
}

func TestPaymentOfflineDeliveryStaysDeliveredAndReturnsSuccess(t *testing.T) {
	pusher := &fakePusher{}
	orders := &fakeOrderStore{order: &model.PaymentOrder{OrderNo: "o", PlayerID: 7, ProductID: 1}}
	response, err := callbackHandlerForTest(orders, pusher).HandlePayCallback([]byte(`{"order_no":"o","status":1}`))
	if err != nil || response.Code != 0 {
		t.Fatalf("callback response=%v err=%v", response, err)
	}
	if !pusher.pushed || pusher.uid != 7 || pusher.msgID != protocol.MsgID_PayResultNotify {
		t.Fatalf("push=%+v", pusher)
	}
	notify := pusher.payload.(*protocolpb.PayResultNotify)
	if notify.OrderNo != "o" || notify.Status != "success" || notify.ProductId != 1 {
		t.Fatalf("notify=%+v", notify)
	}
	if got := orders.statuses[len(orders.statuses)-1]; got != store.OrderStatusDelivered {
		t.Fatalf("last status=%d want delivered", got)
	}
}
