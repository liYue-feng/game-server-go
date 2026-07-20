package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"game-server/internal/protocol"

	"github.com/gorilla/websocket"
)

const (
	probeAddress = "ws://127.0.0.1:8080/ws"
	archiveData  = `{"phase":"a4","source":"devprobe"}`
	probeTimeout = 5 * time.Second
)

type probe struct {
	conn *websocket.Conn
}

func main() {
	if err := runProbe(probeAddress, archiveData); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("development session probe passed")
}

func runProbe(address, expectedArchive string) error {
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
	if err := p.send(protocol.MsgID_LoginReq, protocol.LoginReq{Code: loginCode}); err != nil {
		return err
	}
	var loginResponse protocol.LoginResp
	if err := p.read(protocol.MsgID_LoginResp, &loginResponse); err != nil {
		return err
	}
	if loginResponse.Uid <= 0 {
		return fmt.Errorf("login uid = %d, want positive", loginResponse.Uid)
	}
	if loginResponse.Token == "" {
		return fmt.Errorf("login token is empty")
	}

	if err := p.send(protocol.MsgID_LoadArchiveReq, protocol.LoadArchiveReq{}); err != nil {
		return err
	}
	var initialLoad protocol.LoadArchiveResp
	if err := p.read(protocol.MsgID_LoadArchiveResp, &initialLoad); err != nil {
		return err
	}
	if initialLoad.Data != "" {
		return fmt.Errorf("initial archive data = %q, want empty", initialLoad.Data)
	}

	if err := p.send(protocol.MsgID_SaveArchiveReq, protocol.SaveArchiveReq{Data: expectedArchive}); err != nil {
		return err
	}
	var saveResponse protocol.SaveArchiveResp
	if err := p.read(protocol.MsgID_SaveArchiveResp, &saveResponse); err != nil {
		return err
	}
	if !saveResponse.Success {
		return fmt.Errorf("archive save success = false")
	}

	if err := p.send(protocol.MsgID_LoadArchiveReq, protocol.LoadArchiveReq{}); err != nil {
		return err
	}
	var finalLoad protocol.LoadArchiveResp
	if err := p.read(protocol.MsgID_LoadArchiveResp, &finalLoad); err != nil {
		return err
	}
	if finalLoad.Data != expectedArchive {
		return fmt.Errorf("archive data = %q, want %q", finalLoad.Data, expectedArchive)
	}

	return nil
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

func (p *probe) send(msgID uint16, payload interface{}) error {
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

func (p *probe) read(expectedMsgID uint16, destination interface{}) error {
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
		var errorResponse protocol.ErrorResp
		if err := json.Unmarshal(message.Body, &errorResponse); err != nil {
			return fmt.Errorf("decode server error response: %w", err)
		}
		return fmt.Errorf("server error %d: %s", errorResponse.Code, errorResponse.Msg)
	}
	if message.MsgID != expectedMsgID {
		return fmt.Errorf("response message id = %d, want %d", message.MsgID, expectedMsgID)
	}
	if err := json.Unmarshal(message.Body, destination); err != nil {
		return fmt.Errorf("decode message %d body: %w", expectedMsgID, err)
	}
	return nil
}
