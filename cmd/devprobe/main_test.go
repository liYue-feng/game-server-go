package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

func TestRunProbeIsRepeatableAgainstPersistentDevelopmentStore(t *testing.T) {
	var mu sync.Mutex
	archives := map[string]*protocolpb.PlayerArchive{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		login := &protocolpb.LoginReq{}
		if err := readProbeMessage(conn, protocol.MsgID_LoginReq, login); err != nil {
			return
		}
		_ = writeProbeMessage(conn, protocol.MsgID_LoginResp, &protocolpb.LoginResp{Uid: 42, Token: "token"})
		if err := readProbeMessage(conn, protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{}); err != nil {
			return
		}
		mu.Lock()
		initial := archives[login.Code]
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_LoadArchiveResp, &protocolpb.LoadArchiveResp{Found: initial != nil, Archive: initial})
		save := &protocolpb.SaveArchiveReq{}
		if err := readProbeMessage(conn, protocol.MsgID_SaveArchiveReq, save); err != nil {
			return
		}
		mu.Lock()
		archives[login.Code] = save.Archive
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_SaveArchiveResp, &protocolpb.SaveArchiveResp{Success: true})
		if err := readProbeMessage(conn, protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{}); err != nil {
			return
		}
		mu.Lock()
		final := archives[login.Code]
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_LoadArchiveResp, &protocolpb.LoadArchiveResp{Found: final != nil, Archive: final})
	}))
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http")
	if err := runProbe(address, "first"); err != nil {
		t.Fatal(err)
	}
	if err := runProbe(address, "second"); err != nil {
		t.Fatal(err)
	}
}

func readProbeMessage(conn *websocket.Conn, id uint16, message proto.Message) error {
	_, frame, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	decoded, err := protocol.Decode(frame)
	if err != nil {
		return err
	}
	if decoded.MsgID != id {
		return fmt.Errorf("message ID = %d, want %d", decoded.MsgID, id)
	}
	return proto.Unmarshal(decoded.Body, message)
}
func writeProbeMessage(conn *websocket.Conn, id uint16, message proto.Message) error {
	frame, err := protocol.Encode(id, message)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}
