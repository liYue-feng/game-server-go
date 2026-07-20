package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const (
	probeAddress         = "ws://127.0.0.1:8080/ws"
	probeTimeout         = 5 * time.Second
	probeSuccessEvidence = "development session probe passed: protobuf login found=false typed save typed reload combat duplicate"
)

type probe struct {
	conn *websocket.Conn
}

func main() {
	if err := runProbe(probeAddress); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(probeSuccessEvidence)
}

func runProbe(address string) error {
	conn, _, err := websocket.DefaultDialer.Dial(address, nil)
	if err != nil {
		return fmt.Errorf("dial development server: %w", err)
	}

	p := &probe{conn: conn}
	defer p.close()
	loginCode, err := newProbeLoginCode()
	if err != nil {
		return err
	}
	if err := p.send(protocol.MsgID_LoginReq, &protocolpb.LoginReq{Code: loginCode}); err != nil {
		return err
	}
	var loginResponse protocolpb.LoginResp
	if err := p.read(protocol.MsgID_LoginResp, &loginResponse); err != nil {
		return err
	}
	if loginResponse.Uid <= 0 {
		return fmt.Errorf("login uid = %d, want positive", loginResponse.Uid)
	}
	if loginResponse.Token == "" {
		return fmt.Errorf("login token is empty")
	}

	if err := p.send(protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{}); err != nil {
		return err
	}
	var initialLoad protocolpb.LoadArchiveResp
	if err := p.read(protocol.MsgID_LoadArchiveResp, &initialLoad); err != nil {
		return err
	}
	if initialLoad.Found {
		return fmt.Errorf("initial archive unexpectedly found")
	}

	expectedArchive := newProbeArchive()
	if err := p.send(protocol.MsgID_SaveArchiveReq, &protocolpb.SaveArchiveReq{Archive: expectedArchive}); err != nil {
		return err
	}
	var saveResponse protocolpb.SaveArchiveResp
	if err := p.read(protocol.MsgID_SaveArchiveResp, &saveResponse); err != nil {
		return err
	}
	if !saveResponse.Success {
		return fmt.Errorf("archive save success = false")
	}

	if err := p.send(protocol.MsgID_LoadArchiveReq, &protocolpb.LoadArchiveReq{}); err != nil {
		return err
	}
	var finalLoad protocolpb.LoadArchiveResp
	if err := p.read(protocol.MsgID_LoadArchiveResp, &finalLoad); err != nil {
		return err
	}
	if !finalLoad.Found || !proto.Equal(finalLoad.Archive, expectedArchive) {
		return fmt.Errorf("archive did not round trip: got %v, want %v", finalLoad.Archive, expectedArchive)
	}

	combatRequest := newProbeCombatResult(loginCode)
	if err := p.send(protocol.MsgID_CombatResultReq, combatRequest); err != nil {
		return err
	}
	var firstSettlement protocolpb.CombatResultResp
	if err := p.read(protocol.MsgID_CombatResultResp, &firstSettlement); err != nil {
		return err
	}
	if !firstSettlement.Success || firstSettlement.Duplicate || firstSettlement.Archive == nil {
		return fmt.Errorf("first combat settlement = %v, want successful non-duplicate archive response", &firstSettlement)
	}

	if err := p.send(protocol.MsgID_CombatResultReq, combatRequest); err != nil {
		return err
	}
	var duplicateSettlement protocolpb.CombatResultResp
	if err := p.read(protocol.MsgID_CombatResultResp, &duplicateSettlement); err != nil {
		return err
	}
	if !duplicateSettlement.Success || !duplicateSettlement.Duplicate || !proto.Equal(duplicateSettlement.Archive, firstSettlement.Archive) || duplicateSettlement.RewardGold != firstSettlement.RewardGold || duplicateSettlement.RewardExp != firstSettlement.RewardExp || duplicateSettlement.BestScore != firstSettlement.BestScore {
		return fmt.Errorf("duplicate combat settlement = %v, want stored first settlement snapshot", &duplicateSettlement)
	}

	return nil
}

func newProbeArchive() *protocolpb.PlayerArchive {
	return &protocolpb.PlayerArchive{
		SchemaVersion:         1,
		Gold:                  7,
		Exp:                   11,
		BestScore:             123,
		TotalKills:            17,
		TotalGames:            2,
		HighestClearedDungeon: 4,
		TalentPoints:          5,
		UnlockedStyles:        []int32{1, 3},
		LastStyleId:           3,
	}
}

func newProbeCombatResult(runID string) *protocolpb.CombatResultReq {
	return &protocolpb.CombatResultReq{
		RunId:        "probe-" + runID,
		DungeonLevel: 2,
		Score:        100,
		Kills:        2,
		DurationMs:   1_000,
		StyleId:      1,
		Outcome:      protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY,
		PlayerLevel:  1,
	}
}

func newProbeLoginCode() (string, error) {
	identity := make([]byte, 16)
	if _, err := rand.Read(identity); err != nil {
		return "", fmt.Errorf("generate development probe identity: %w", err)
	}
	return "dev:process-probe-" + hex.EncodeToString(identity), nil
}

func (p *probe) close() {
	_ = p.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe complete"),
		time.Now().Add(probeTimeout),
	)
	_ = p.conn.Close()
}

func (p *probe) send(msgID uint16, payload proto.Message) error {
	frame, err := protocol.Encode(msgID, payload)
	if err != nil {
		return fmt.Errorf("encode message %d: %w", msgID, err)
	}
	if err := p.conn.SetWriteDeadline(time.Now().Add(probeTimeout)); err != nil {
		return fmt.Errorf("set write deadline for message %d: %w", msgID, err)
	}
	if err := p.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return fmt.Errorf("write message %d: %w", msgID, err)
	}
	return nil
}

func (p *probe) read(expectedMsgID uint16, destination proto.Message) error {
	if err := p.conn.SetReadDeadline(time.Now().Add(probeTimeout)); err != nil {
		return fmt.Errorf("set read deadline for message %d: %w", expectedMsgID, err)
	}
	messageType, frame, err := p.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read message %d: %w", expectedMsgID, err)
	}
	if messageType != websocket.BinaryMessage {
		return fmt.Errorf("received WebSocket message type %d, want binary", messageType)
	}

	message, err := protocol.Decode(frame)
	if err != nil {
		return fmt.Errorf("decode response for message %d: %w", expectedMsgID, err)
	}
	if message.MsgID == protocol.MsgID_Error {
		var errorResponse protocolpb.ErrorResp
		if err := proto.Unmarshal(message.Body, &errorResponse); err != nil {
			return fmt.Errorf("decode server error response: %w", err)
		}
		return fmt.Errorf("server error %d: %s", errorResponse.Code, errorResponse.Msg)
	}
	if message.MsgID != expectedMsgID {
		return fmt.Errorf("response message id = %d, want %d", message.MsgID, expectedMsgID)
	}
	if err := proto.Unmarshal(message.Body, destination); err != nil {
		return fmt.Errorf("decode message %d body: %w", expectedMsgID, err)
	}
	return nil
}
