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
	combatRuns := map[string]bool{}
	combatRequests := 0
	var savedArchives []*protocolpb.PlayerArchive
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		login := &protocolpb.LoginReq{}
		seq, err := readProbeMessage(conn, protocol.MsgID_LoginReq, login)
		if err != nil {
			return
		}
		_ = writeProbeMessage(conn, protocol.MsgID_LoginResp, seq, &protocolpb.LoginResp{Uid: 42, Token: "token"})
		seq, err = readProbeMessage(conn, protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{})
		if err != nil {
			return
		}
		mu.Lock()
		initial := archives[login.Code]
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_LoadArchiveResp, seq, &protocolpb.LoadArchiveResp{Found: initial != nil, Archive: initial})
		save := &protocolpb.SaveArchiveReq{}
		seq, err = readProbeMessage(conn, protocol.MsgID_SaveArchiveReq, save)
		if err != nil {
			return
		}
		mu.Lock()
		archives[login.Code] = save.Archive
		savedArchives = append(savedArchives, proto.Clone(save.Archive).(*protocolpb.PlayerArchive))
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_SaveArchiveResp, seq, &protocolpb.SaveArchiveResp{Success: true})
		seq, err = readProbeMessage(conn, protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{})
		if err != nil {
			return
		}
		mu.Lock()
		final := archives[login.Code]
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_LoadArchiveResp, seq, &protocolpb.LoadArchiveResp{Found: final != nil, Archive: final})
		combat := &protocolpb.CombatResultReq{}
		seq, err = readProbeMessage(conn, protocol.MsgID_CombatResultReq, combat)
		if err != nil {
			return
		}
		mu.Lock()
		duplicate := combatRuns[combat.RunId]
		combatRuns[combat.RunId] = true
		combatRequests++
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_CombatResultResp, seq, &protocolpb.CombatResultResp{
			Success: true, Duplicate: duplicate, RunId: combat.RunId, RewardGold: 10, RewardExp: 20, BestScore: 100, Archive: newProbeArchive(),
		})
		seq, err = readProbeMessage(conn, protocol.MsgID_CombatResultReq, combat)
		if err != nil {
			return
		}
		mu.Lock()
		combatRequests++
		mu.Unlock()
		_ = writeProbeMessage(conn, protocol.MsgID_CombatResultResp, seq, &protocolpb.CombatResultResp{
			Success: true, Duplicate: true, RunId: combat.RunId, RewardGold: 10, RewardExp: 20, BestScore: 100, Archive: newProbeArchive(),
		})
	}))
	defer server.Close()
	address := "ws" + strings.TrimPrefix(server.URL, "http")
	if err := runProbe(address); err != nil {
		t.Fatal(err)
	}
	if err := runProbe(address); err != nil {
		t.Fatal(err)
	}
	if len(savedArchives) != 2 {
		t.Fatalf("saved archive count = %d, want 2", len(savedArchives))
	}
	if combatRequests != 4 {
		t.Fatalf("combat settlement requests = %d, want 4", combatRequests)
	}
	for _, archive := range savedArchives {
		if !proto.Equal(archive, newProbeArchive()) {
			t.Fatalf("saved archive = %v, want typed probe archive %v", archive, newProbeArchive())
		}
	}
}

func TestProbeSuccessEvidenceDescribesTheTypedArchiveContract(t *testing.T) {
	const want = "development session probe passed: protobuf login found=false typed save typed reload combat duplicate"
	if probeSuccessEvidence != want {
		t.Fatalf("probeSuccessEvidence = %q, want %q", probeSuccessEvidence, want)
	}
}

func readProbeMessage(conn *websocket.Conn, id uint16, message proto.Message) (uint32, error) {
	_, frame, err := conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	decoded, err := protocol.Decode(frame)
	if err != nil {
		return 0, err
	}
	if decoded.MsgID != id {
		return 0, fmt.Errorf("message ID = %d, want %d", decoded.MsgID, id)
	}
	return decoded.Seq, proto.Unmarshal(decoded.Body, message)
}
func writeProbeMessage(conn *websocket.Conn, id uint16, seq uint32, message proto.Message) error {
	frame, err := protocol.Encode(id, seq, message)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}
