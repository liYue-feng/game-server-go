package login

import (
	"errors"
	"testing"

	"game-server/internal/protocol"
)

func TestClassifyLoginError(t *testing.T) {
	upstreamErr := errors.New("wechat unavailable")
	storeErr := errors.New("player store unavailable")

	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{
			name: "invalid credential",
			err:  &exchangeError{err: ErrDevelopmentCodeRejected},
			want: protocol.ErrLoginInvalidCode,
		},
		{
			name: "upstream exchange failure",
			err:  &exchangeError{err: upstreamErr},
			want: protocol.ErrLoginWechatFailed,
		},
		{
			name: "internal service failure",
			err:  storeErr,
			want: protocol.ErrInternal,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := classifyLoginError(tt.err)
			if got != tt.want {
				t.Fatalf("classifyLoginError() code = %d, want %d", got, tt.want)
			}
		})
	}
}
