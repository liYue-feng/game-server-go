package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"game-server/internal/protocol"

	"github.com/gorilla/websocket"
)

func TestRunProbeIsRepeatableAgainstPersistentDevelopmentStore(t *testing.T) {
	const archive = `{"phase":"a4","source":"devprobe"}`
	serverErrors := make(chan error, 2)
	identities := make(chan string, 2)
	archives := make(map[string]string)
	var archivesMu sync.Mutex
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.Close()

		loginBody, err := readProbeRequest(conn, protocol.MsgID_LoginReq)
		if err != nil {
			serverErrors <- err
			return
		}
		var loginRequest protocol.LoginReq
		if err := json.Unmarshal(loginBody, &loginRequest); err != nil {
			serverErrors <- err
			return
		}
		if !strings.HasPrefix(loginRequest.Code, "dev:") || strings.TrimPrefix(loginRequest.Code, "dev:") == "" {
			serverErrors <- fmt.Errorf("login code = %q, want non-empty dev identity", loginRequest.Code)
			return
		}
		identities <- loginRequest.Code
		if err := writeProbeResponse(conn, protocol.MsgID_LoginResp, protocol.LoginResp{Uid: 42, Nickname: "probe", Token: "token"}); err != nil {
			serverErrors <- err
			return
		}

		if _, err := readProbeRequest(conn, protocol.MsgID_LoadArchiveReq); err != nil {
			serverErrors <- err
			return
		}
		archivesMu.Lock()
		initialArchive := archives[loginRequest.Code]
		archivesMu.Unlock()
		if err := writeProbeResponse(conn, protocol.MsgID_LoadArchiveResp, protocol.LoadArchiveResp{Data: initialArchive}); err != nil {
			serverErrors <- err
			return
		}

		saveBody, err := readProbeRequest(conn, protocol.MsgID_SaveArchiveReq)
		if err != nil {
			serverErrors <- err
			return
		}
		var saveRequest protocol.SaveArchiveReq
		if err := json.Unmarshal(saveBody, &saveRequest); err != nil {
			serverErrors <- err
			return
		}
		if saveRequest.Data != archive {
			serverErrors <- fmt.Errorf("archive data = %q, want %q", saveRequest.Data, archive)
			return
		}
		archivesMu.Lock()
		archives[loginRequest.Code] = saveRequest.Data
		archivesMu.Unlock()
		if err := writeProbeResponse(conn, protocol.MsgID_SaveArchiveResp, protocol.SaveArchiveResp{Success: true}); err != nil {
			serverErrors <- err
			return
		}

		if _, err := readProbeRequest(conn, protocol.MsgID_LoadArchiveReq); err != nil {
			serverErrors <- err
			return
		}
		archivesMu.Lock()
		finalArchive := archives[loginRequest.Code]
		archivesMu.Unlock()
		if err := writeProbeResponse(conn, protocol.MsgID_LoadArchiveResp, protocol.LoadArchiveResp{Data: finalArchive}); err != nil {
			serverErrors <- err
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(probeTimeout)); err != nil {
			serverErrors <- err
			return
		}
		_, _, err = conn.ReadMessage()
		if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			serverErrors <- fmt.Errorf("probe close error = %v, want normal closure", err)
			return
		}
		serverErrors <- nil
	}))
	defer server.Close()

	address := "ws" + strings.TrimPrefix(server.URL, "http")
	firstErr := runProbe(address, archive)
	secondErr := runProbe(address, archive)
	if firstErr != nil {
		t.Fatalf("first runProbe() error = %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("second runProbe() error = %v", secondErr)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := <-serverErrors; err != nil {
			t.Fatalf("probe server attempt %d error = %v", attempt+1, err)
		}
	}
	firstIdentity := <-identities
	secondIdentity := <-identities
	if firstIdentity == secondIdentity {
		t.Fatalf("probe identities are both %q, want a unique identity per run", firstIdentity)
	}
}

func readProbeRequest(conn *websocket.Conn, expectedMsgID uint16) ([]byte, error) {
	_, frame, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	message, err := protocol.Decode(frame)
	if err != nil {
		return nil, err
	}
	if message.MsgID != expectedMsgID {
		return nil, fmt.Errorf("request message id = %d, want %d", message.MsgID, expectedMsgID)
	}
	return message.Body, nil
}

func writeProbeResponse(conn *websocket.Conn, msgID uint16, payload interface{}) error {
	frame, err := protocol.Encode(msgID, payload)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}
