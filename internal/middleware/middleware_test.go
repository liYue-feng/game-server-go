package middleware

import (
	"encoding/json"
	"testing"

	"game-server/internal/gateway"
)

func TestAuthMiddlewarePassesUnauthenticatedConnectionToRouterApprovedHandler(t *testing.T) {
	conn := &gateway.Connection{}
	called := 0

	AuthMiddleware(nil)(conn, json.RawMessage(`{"code":"login-code"}`), func(*gateway.Connection, json.RawMessage) {
		called++
	})

	if called != 1 {
		t.Fatalf("next call count = %d, want 1", called)
	}
}
