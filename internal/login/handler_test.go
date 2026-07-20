package login

import (
	"context"
	"errors"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"
)

func TestHandlerLoginBindsKernelSessionAndSetsIdentity(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	handler := NewHandlerWithService(NewLoginService(
		developmentStore,
		developmentStore,
		NewDevelopmentCodeExchanger(true),
		func() (string, error) { return "fixed-token", nil },
	))
	kernelSession := session.New(nil)
	ctx := session.WithSession(context.Background(), kernelSession)

	resp, err := handler.Login(ctx, &protocol.LoginReq{Code: "dev:editor-001"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp.Uid != 1 || resp.Nickname == "" || resp.Token != "fixed-token" {
		t.Fatalf("Login() response = %#v, want bound identity", resp)
	}
	if kernelSession.UID() != resp.Uid {
		t.Fatalf("session UID = %d, want %d", kernelSession.UID(), resp.Uid)
	}
	if got := kernelSession.GetString(SessionKeyNickname); got != resp.Nickname {
		t.Fatalf("session nickname = %q, want %q", got, resp.Nickname)
	}
	if got := kernelSession.GetString(SessionKeyToken); got != resp.Token {
		t.Fatalf("session token = %q, want %q", got, resp.Token)
	}
}

func TestHandlerLoginMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid code", err: &exchangeError{err: ErrDevelopmentCodeRejected}, want: protocol.ErrLoginInvalidCode},
		{name: "wechat upstream", err: &exchangeError{err: errors.New("wechat unavailable")}, want: protocol.ErrLoginWechatFailed},
		{name: "internal", err: errors.New("player repository unavailable"), want: protocol.ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandlerWithService(&LoginService{
				exchanger: codeExchangerFunc(func(string) (LoginIdentity, error) {
					var exchange *exchangeError
					if errors.As(tt.err, &exchange) {
						return LoginIdentity{}, exchange.err
					}
					return LoginIdentity{OpenID: "openid-1"}, nil
				}),
				players:  &playerRepositoryStub{getErr: tt.err},
				sessions: &sessionRepositoryStub{},
				generate: func() (string, error) { return "token", nil },
			})

			_, err := handler.Login(context.Background(), &protocol.LoginReq{Code: "code"})
			var bizErr *protocol.BizError
			if !errors.As(err, &bizErr) || bizErr.Code != tt.want {
				t.Fatalf("Login() error = %T %v, want BizError code %d", err, err, tt.want)
			}
		})
	}
}

func TestHandlerHeartbeatRefreshesBoundSessionAndEchoesTimestamp(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	handler := NewHandlerWithService(NewLoginService(developmentStore, developmentStore, nil, nil))
	kernelSession := session.New(nil)
	kernelSession.Bind(9)
	ctx := session.WithSession(context.Background(), kernelSession)

	resp, err := handler.Heartbeat(ctx, &protocol.HeartbeatReq{Timestamp: 12345})
	if err != nil || resp.Timestamp != 12345 {
		t.Fatalf("Heartbeat() = %#v, %v; want echoed timestamp", resp, err)
	}
	stored, err := developmentStore.GetSession(9)
	if err != nil || stored == nil || stored.Uid != 9 {
		t.Fatalf("GetSession(9) = %#v, %v; want refreshed session", stored, err)
	}
}
