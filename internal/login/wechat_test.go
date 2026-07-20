package login

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestWechatCode2SessionDebugLogContainsOnlySafeResponseMetadata(t *testing.T) {
	logs := observeDebugLogs(t)
	client := &WechatClient{
		appID:     "app-id",
		appSecret: "app-secret",
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"openid":"secret-openid","session_key":"secret-session-key","unionid":"secret-unionid","errcode":0,"errmsg":"secret-error-message"}`,
				)),
			}, nil
		})},
	}

	result, err := client.Code2Session("prod-code")
	if err != nil || result.SessionKey != "secret-session-key" {
		t.Fatalf("Code2Session() = %#v, %v; want parsed response", result, err)
	}

	entries := logs.FilterMessage("微信 code2session 响应").All()
	if len(entries) != 1 {
		t.Fatalf("response log count = %d, want 1; logs = %#v", len(entries), logs.AllUntimed())
	}
	fields := entries[0].ContextMap()
	if fields["status"] != int64(http.StatusOK) || fields["errCode"] != int64(0) || fields["hasOpenID"] != true {
		t.Fatalf("response metadata = %#v, want status=200 errCode=0 hasOpenID=true", fields)
	}
	for _, forbiddenField := range []string{"body", "session_key", "openid", "unionid", "errmsg"} {
		if _, exists := fields[forbiddenField]; exists {
			t.Fatalf("response log contains forbidden field %q: %#v", forbiddenField, fields)
		}
	}
	for _, secret := range []string{"secret-openid", "secret-session-key", "secret-unionid", "secret-error-message"} {
		assertObservedLogsExclude(t, logs, secret)
	}
}

func TestWechatCode2SessionTransportErrorIsSafeAndUnwrapsCause(t *testing.T) {
	cause := errors.New("dial unavailable")
	client := &WechatClient{
		appID:     "secret-app-id",
		appSecret: "secret-app-secret",
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("transport failed for %s: %w", req.URL.String(), cause)
		})},
	}

	_, err := client.Code2Session("prod-secret-code")
	if !errors.Is(err, cause) {
		t.Fatalf("Code2Session() error = %v, want wrapped transport cause", err)
	}
	for _, secret := range []string{"secret-app-id", "secret-app-secret", "prod-secret-code"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("transport error leaked %q: %v", secret, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func observeDebugLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)
	return logs
}
