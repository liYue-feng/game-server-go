package login

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidLoginCode        = errors.New("invalid login code")
	ErrDevelopmentCodeRejected = fmt.Errorf("%w: development login code rejected", ErrInvalidLoginCode)
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

type WechatCodeExchanger struct {
	client *WechatClient
}

func NewWechatCodeExchanger(client *WechatClient) *WechatCodeExchanger {
	return &WechatCodeExchanger{client: client}
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
	return LoginIdentity{OpenID: result.OpenID}, nil
}
