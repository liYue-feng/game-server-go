package hooks

import (
	"context"
	"errors"
	"testing"

	"game-server/internal/kernel"
	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"game-server/internal/store"
)

func TestAuthAllowsAuthFreeMessagesWithoutSession(t *testing.T) {
	k := kernel.New(nil)
	k.Register(protocol.MsgID_HeartbeatReq, protocol.MsgID_HeartbeatResp, func(context.Context, *protocolpb.HeartbeatReq) (*protocolpb.HeartbeatResp, error) {
		return &protocolpb.HeartbeatResp{}, nil
	}, kernel.AuthFree())
	ctx, in, err := Auth(store.NewMemoryDevelopmentStore(), k)(kernel.WithMsgID(context.Background(), protocol.MsgID_HeartbeatReq), "request")
	if err != nil || ctx == nil || in != "request" {
		t.Fatalf("Auth(auth-free) = %v, %v, %v; want unchanged context/input", ctx, in, err)
	}
}

func TestAuthRejectsUnboundSession(t *testing.T) {
	k := kernel.New(nil)
	hook := Auth(store.NewMemoryDevelopmentStore(), k)
	_, _, err := hook(session.WithSession(context.Background(), session.New(nil)), "request")
	assertBizErrorCode(t, err, protocol.ErrUnauthorized)
}

func TestAuthRejectsMissingStoredSession(t *testing.T) {
	k := kernel.New(nil)
	bound := session.New(nil)
	bound.Bind(7)
	_, _, err := Auth(store.NewMemoryDevelopmentStore(), k)(session.WithSession(context.Background(), bound), "request")
	assertBizErrorCode(t, err, protocol.ErrLoginTokenExpired)
}

func TestAuthAllowsValidStoredSession(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	if err := developmentStore.SetSession(7, &store.SessionData{Uid: 7, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	k := kernel.New(nil)
	bound := session.New(nil)
	bound.Bind(7)
	ctx, in, err := Auth(developmentStore, k)(session.WithSession(context.Background(), bound), "request")
	if err != nil || ctx == nil || in != "request" {
		t.Fatalf("Auth(valid session) = %v, %v, %v; want unchanged context/input", ctx, in, err)
	}
}

func assertBizErrorCode(t *testing.T, err error, want int) {
	t.Helper()
	var bizErr *protocol.BizError
	if !errors.As(err, &bizErr) || bizErr.Code != want {
		t.Fatalf("error = %T %v, want BizError code %d", err, err, want)
	}
}
