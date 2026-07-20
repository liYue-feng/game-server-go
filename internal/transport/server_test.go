package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"game-server/internal/kernel"
	"game-server/internal/protocol"

	"github.com/gorilla/websocket"
)

const lifecycleTestTimeout = 3 * time.Second

func TestServerShutdownBeforeStartReturns(t *testing.T) {
	server := NewServer(kernel.New(nil))

	shutdownDone := make(chan struct{})
	go func() {
		server.Shutdown()
		close(shutdownDone)
	}()

	waitForSignal(t, shutdownDone, "Shutdown before Start")
	if err := server.Start(freeAddress(t)); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Start after Shutdown error = %v, want %v", err, http.ErrServerClosed)
	}
}

func TestHubShutdownBeforeRunReturns(t *testing.T) {
	hub := NewHub()

	shutdownDone := make(chan struct{})
	go func() {
		hub.Shutdown()
		close(shutdownDone)
	}()

	waitForSignal(t, shutdownDone, "Hub.Shutdown before Run")
}

func TestServerStartShutdownRaceStopsListener(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		server := NewServer(kernel.New(nil))
		addr := freeAddress(t)
		startDone := make(chan error, 1)
		shutdownDone := make(chan struct{})
		start := make(chan struct{})

		go func() {
			<-start
			startDone <- server.Start(addr)
		}()
		go func() {
			<-start
			server.Shutdown()
			close(shutdownDone)
		}()

		close(start)
		waitForSignal(t, shutdownDone, fmt.Sprintf("Shutdown iteration %d", iteration))
		if err := waitForError(t, startDone, fmt.Sprintf("Start iteration %d", iteration)); !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("iteration %d: Start error = %v, want %v", iteration, err, http.ErrServerClosed)
		}
		assertAddressReusable(t, addr)
	}
}

func TestServerRepeatedShutdownReturns(t *testing.T) {
	server := NewServer(kernel.New(nil))
	addr := freeAddress(t)
	startDone := startServer(t, server, addr)

	for iteration := 0; iteration < 3; iteration++ {
		shutdownDone := make(chan struct{})
		go func() {
			server.Shutdown()
			close(shutdownDone)
		}()
		waitForSignal(t, shutdownDone, fmt.Sprintf("Shutdown call %d", iteration+1))
	}

	if err := waitForError(t, startDone, "Start after repeated Shutdown"); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Start error = %v, want %v", err, http.ErrServerClosed)
	}
	assertAddressReusable(t, addr)
}

func TestServerShutdownWaitsForActiveHandlerAndClosesConnection(t *testing.T) {
	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	handlerReturned := make(chan struct{})

	k := kernel.New(nil)
	k.Register(protocol.MsgID_HeartbeatReq, protocol.MsgID_HeartbeatResp,
		func(context.Context, *protocol.HeartbeatReq) (*protocol.HeartbeatResp, error) {
			close(handlerEntered)
			<-releaseHandler
			close(handlerReturned)
			return &protocol.HeartbeatResp{Timestamp: 42}, nil
		}, kernel.AuthFree())

	server := NewServer(k)
	addr := freeAddress(t)
	startDone := startServer(t, server, addr)
	ws := dialWebSocket(t, addr)
	defer ws.Close()

	request, err := protocol.Encode(protocol.MsgID_HeartbeatReq, protocol.HeartbeatReq{Timestamp: 1})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, request); err != nil {
		t.Fatalf("write request: %v", err)
	}
	waitForSignal(t, handlerEntered, "handler entry")

	shutdownDone := make(chan struct{})
	go func() {
		server.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		close(releaseHandler)
		t.Fatal("Shutdown returned while Kernel.Dispatch handler was still active")
	case <-time.After(100 * time.Millisecond):
	}

	if err := ws.SetReadDeadline(time.Now().Add(lifecycleTestTimeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := ws.ReadMessage(); err == nil {
		close(releaseHandler)
		t.Fatal("WebSocket connection remained readable after Shutdown began")
	}

	close(releaseHandler)
	waitForSignal(t, handlerReturned, "handler return")
	waitForSignal(t, shutdownDone, "Shutdown after handler release")
	if err := waitForError(t, startDone, "Start after active connection Shutdown"); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Start error = %v, want %v", err, http.ErrServerClosed)
	}
	assertAddressReusable(t, addr)
}

func startServer(t *testing.T, server *Server, addr string) <-chan error {
	t.Helper()

	startDone := make(chan error, 1)
	go func() {
		startDone <- server.Start(addr)
	}()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(lifecycleTestTimeout)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + addr + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return startDone
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	server.Shutdown()
	t.Fatalf("server did not become healthy at %s", addr)
	return nil
}

func dialWebSocket(t *testing.T, addr string) *websocket.Conn {
	t.Helper()

	ws, response, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	return ws
}

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address: %v", err)
	}
	return addr
}

func assertAddressReusable(t *testing.T, addr string) {
	t.Helper()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen on released address %s: %v", addr, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close reused address %s: %v", addr, err)
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(lifecycleTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func waitForError(t *testing.T, result <-chan error, operation string) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(lifecycleTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}
