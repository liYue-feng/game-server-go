package login

import (
	"errors"
	"testing"
)

func TestWechatAPIErrorClassifiesOnlyInvalidCredentialCode(t *testing.T) {
	invalidCodeErr := wechatAPIError(Code2SessionResult{ErrCode: 40029, ErrMsg: "invalid code"})
	if !errors.Is(invalidCodeErr, ErrInvalidLoginCode) {
		t.Fatalf("wechatAPIError(40029) = %v, want invalid login code", invalidCodeErr)
	}

	upstreamErr := wechatAPIError(Code2SessionResult{ErrCode: -1, ErrMsg: "system busy"})
	if errors.Is(upstreamErr, ErrInvalidLoginCode) {
		t.Fatalf("wechatAPIError(-1) = %v, do not want invalid login code", upstreamErr)
	}
}
