package gateway

import (
	"encoding/json"
	"testing"

	"game-server/internal/protocol"
)

func TestRouterHasHandlerReportsRegisteredMessages(t *testing.T) {
	router := NewRouter()
	router.Register(protocol.MsgID_LoginReq, func(_ *Connection, _ json.RawMessage) {})

	if !router.HasHandler(protocol.MsgID_LoginReq) {
		t.Fatal("HasHandler(LoginReq) = false, want true")
	}
	if router.HasHandler(protocol.MsgID_GetRankReq) {
		t.Fatal("HasHandler(GetRankReq) = true, want false")
	}
}
