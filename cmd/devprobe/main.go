package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"game-server/internal/protocol"

	"github.com/gorilla/websocket"
)

const (
	probeAddress = "ws://127.0.0.1:8080/ws"
	archiveData  = `{"phase":"a4"}`
	probeTimeout = 5 * time.Second
)

type probe struct {
	conn *websocket.Conn
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("development session probe passed")
}

func run() error {
	conn, _, err := websocket.DefaultDialer.Dial(probeAddress, nil)
	if err != nil {
		return fmt.Errorf("dial development server: %w", err)
	}
	defer conn.Close()

	p := &probe{conn: conn}
	if err := p.send(protocol.MsgID_LoginReq, protocol.LoginReq{Code: "dev:process-probe"}); err != nil {
		return err
	}
	var loginResp protocol.LoginResp
	if err := p.read(protocol.MsgID_LoginResp, &loginResp); err != nil {
		return err
	}
	if loginResp.Uid <= 0 {
		return fmt.Errorf("login uid = %d, want positive", loginResp.Uid)
	}
	if loginResp.Token == "" {
		return fmt.Errorf("login token is empty")
	}

	if err := p.send(protocol.MsgID_SaveArchiveReq, protocol.SaveArchiveReq{Data: archiveData}); err != nil {
		return err
	}
	var saveResp protocol.SaveArchiveResp
	if err := p.read(protocol.MsgID_SaveArchiveResp, &saveResp); err != nil {
		return err
	}
	if !saveResp.Success {
		return fmt.Errorf("archive save success = false")
	}

	if err := p.send(protocol.MsgID_LoadArchiveReq, protocol.LoadArchiveReq{}); err != nil {
		return err
	}
	var loadResp protocol.LoadArchiveResp
	if err := p.read(protocol.MsgID_LoadArchiveResp, &loadResp); err != nil {
		return err
	}
	if loadResp.Data != archiveData {
		return fmt.Errorf("archive data = %q, want %q", loadResp.Data, archiveData)
	}

	return nil
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
		var errorResp protocol.ErrorResp
		if err := json.Unmarshal(message.Body, &errorResp); err != nil {
			return fmt.Errorf("decode server error response: %w", err)
		}
		return fmt.Errorf("server error %d: %s", errorResp.Code, errorResp.Msg)
	}
	if message.MsgID != expectedMsgID {
		return fmt.Errorf("response message id = %d, want %d", message.MsgID, expectedMsgID)
	}
	if err := json.Unmarshal(message.Body, destination); err != nil {
		return fmt.Errorf("decode message %d body: %w", expectedMsgID, err)
	}
	return nil
}
