package login

import (
	"errors"
	"testing"

	"game-server/internal/protocol"
)

func TestDevelopmentCodeExchangerHonorsEnablementAndIdentity(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		code    string
		wantID  string
		wantErr error
	}{
		{name: "enabled", enabled: true, code: "dev:editor-001", wantID: "dev:editor-001"},
		{name: "disabled", code: "dev:editor-001", wantErr: ErrDevelopmentCodeRejected},
		{name: "missing prefix", enabled: true, code: "wechat-code", wantErr: ErrDevelopmentCodeRejected},
		{name: "empty identity", enabled: true, code: "dev:", wantErr: ErrDevelopmentCodeRejected},
		{name: "blank identity", enabled: true, code: "dev:   ", wantErr: ErrDevelopmentCodeRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity, err := NewDevelopmentCodeExchanger(tt.enabled).Exchange(tt.code)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || !errors.Is(err, ErrInvalidLoginCode) {
					t.Fatalf("Exchange(%q) error = %v, want %v classified invalid", tt.code, err, tt.wantErr)
				}
				return
			}
			if err != nil || identity.OpenID != tt.wantID {
				t.Fatalf("Exchange(%q) = %#v, %v; want OpenID %q", tt.code, identity, err, tt.wantID)
			}
		})
	}
}

func TestWechatCodeExchangerRejectsDevelopmentCodeWithoutCallingClient(t *testing.T) {
	_, err := NewWechatCodeExchanger(nil).Exchange("dev:editor-001")
	if !errors.Is(err, ErrDevelopmentCodeRejected) || !errors.Is(err, ErrInvalidLoginCode) {
		t.Fatalf("Exchange() error = %v, want development rejection classified invalid", err)
	}
}

func TestWechatCodeExchangerRejectsEmptyIdentityFromClient(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result *Code2SessionResult
	}{
		{name: "nil result", result: nil},
		{name: "missing openid", result: &Code2SessionResult{}},
		{name: "whitespace openid", result: &Code2SessionResult{OpenID: " \t "}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			exchanger := &WechatCodeExchanger{client: code2SessionStub{result: tt.result}}
			identity, err := exchanger.Exchange("prod-code")
			if identity.OpenID != "" || !errors.Is(err, ErrWechatIdentityMissing) || !errors.Is(err, ErrWechatUpstreamResponse) {
				t.Fatalf("Exchange() = %#v, %v; want missing upstream identity error", identity, err)
			}
			if errors.Is(err, ErrInvalidLoginCode) {
				t.Fatalf("Exchange() error = %v, empty upstream identity is not invalid client code", err)
			}
		})
	}
}

func TestWechatAPIErrorsClassifyOnlyInvalidCredential(t *testing.T) {
	invalid := wechatAPIError(Code2SessionResult{ErrCode: 40029, ErrMsg: "invalid code"})
	if !errors.Is(invalid, ErrInvalidLoginCode) {
		t.Fatalf("wechatAPIError(40029) = %v, want invalid login code", invalid)
	}

	upstream := wechatAPIError(Code2SessionResult{ErrCode: -1, ErrMsg: "system busy"})
	if errors.Is(upstream, ErrInvalidLoginCode) {
		t.Fatalf("wechatAPIError(-1) = %v, do not want invalid login code", upstream)
	}
}

func TestWechatIdentityFailuresMapToUpstreamBusinessError(t *testing.T) {
	for _, cause := range []error{ErrWechatUpstreamResponse, ErrWechatIdentityMissing} {
		err := wrapLoginOperation(LoginStageExchangeCode, &exchangeError{err: cause})
		code, _ := classifyLoginError(err)
		if code != protocol.ErrLoginWechatFailed {
			t.Fatalf("classifyLoginError(%v) = %d, want WeChat upstream code", cause, code)
		}
	}
}

type code2SessionStub struct {
	result *Code2SessionResult
	err    error
}

func (s code2SessionStub) Code2Session(string) (*Code2SessionResult, error) {
	return s.result, s.err
}
