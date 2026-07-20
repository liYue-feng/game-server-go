package login

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidLoginCode        = errors.New("invalid login code")
	ErrDevelopmentCodeRejected = fmt.Errorf("%w: development login code rejected", ErrInvalidLoginCode)
	ErrWechatUpstreamResponse  = errors.New("wechat upstream response invalid")
	ErrWechatIdentityMissing   = fmt.Errorf("%w: openid missing", ErrWechatUpstreamResponse)
)

type LoginIdentity struct {
	OpenID string
}

type CodeExchanger interface {
	Exchange(code string) (LoginIdentity, error)
}

type DevelopmentCodeExchanger struct {
	enabled bool
}

func NewDevelopmentCodeExchanger(enabled bool) *DevelopmentCodeExchanger {
	return &DevelopmentCodeExchanger{enabled: enabled}
}

func (e *DevelopmentCodeExchanger) Exchange(code string) (LoginIdentity, error) {
	identity, isDevelopmentCode := strings.CutPrefix(code, "dev:")
	if !e.enabled || !isDevelopmentCode || strings.TrimSpace(identity) == "" {
		return LoginIdentity{}, ErrDevelopmentCodeRejected
	}
	return LoginIdentity{OpenID: code}, nil
}

type wechatCode2SessionClient interface {
	Code2Session(code string) (*Code2SessionResult, error)
}

type WechatCodeExchanger struct {
	client wechatCode2SessionClient
}

func NewWechatCodeExchanger(client *WechatClient) *WechatCodeExchanger {
	var sessionClient wechatCode2SessionClient
	if client != nil {
		sessionClient = client
	}
	return &WechatCodeExchanger{client: sessionClient}
}

func (e *WechatCodeExchanger) Exchange(code string) (LoginIdentity, error) {
	if strings.HasPrefix(code, "dev:") {
		return LoginIdentity{}, ErrDevelopmentCodeRejected
	}
	if strings.TrimSpace(code) == "" {
		return LoginIdentity{}, ErrInvalidLoginCode
	}
	if e.client == nil {
		return LoginIdentity{}, errors.New("wechat client unavailable")
	}

	result, err := e.client.Code2Session(code)
	if err != nil {
		return LoginIdentity{}, err
	}
	if result == nil || strings.TrimSpace(result.OpenID) == "" {
		return LoginIdentity{}, ErrWechatIdentityMissing
	}
	return LoginIdentity{OpenID: result.OpenID}, nil
}
