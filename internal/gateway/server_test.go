package gateway

import (
	"sync"
	"testing"
	"time"

	"game-server/internal/protocol"
)

const shutdownTestTimeout = time.Second

func TestServerShutdownBeforeStartReturns(t *testing.T) {
	server := NewServer(nil)

	mustComplete(t, "shutdown before start", server.Shutdown)
	mustComplete(t, "hub loop shutdown before start", func() { <-server.hub.loopDone })
}

func TestServerShutdownClosesConnectionsAndIsRepeatable(t *testing.T) {
	server := NewServer(nil)
	go server.hub.Run()

	conn := newConnection(server.hub, nil)
	server.hub.Register(conn)

	mustComplete(t, "first shutdown", server.Shutdown)
	mustComplete(t, "repeated shutdown", server.Shutdown)

	mustBeClosed(t, conn)
}

func TestServerShutdownRejectsPostShutdownRegistration(t *testing.T) {
	server := NewServer(nil)
	go server.hub.Run()
	server.Shutdown()

	conn := newConnection(server.hub, nil)
	mustComplete(t, "post-shutdown registration", func() {
		server.hub.Register(conn)
	})

	mustBeClosed(t, conn)
}

func TestHubOperationsReturnAfterShutdownWithoutRestartingLoop(t *testing.T) {
	server := NewServer(nil)
	mustComplete(t, "shutdown", server.Shutdown)
	mustComplete(t, "hub loop shutdown", func() { <-server.hub.loopDone })

	conn := newConnection(server.hub, nil)
	mustComplete(t, "post-shutdown registration", func() { server.hub.Register(conn) })
	mustComplete(t, "post-shutdown unregistration", func() { server.hub.Unregister(conn) })
	mustComplete(t, "post-shutdown broadcast", func() {
		server.hub.Broadcast(protocol.MsgID_HeartbeatResp, protocol.HeartbeatResp{})
	})
	mustComplete(t, "post-shutdown online count", func() {
		if count := server.hub.OnlineCount(); count != 0 {
			t.Fatalf("OnlineCount() after shutdown = %d, want 0", count)
		}
	})
	mustComplete(t, "post-shutdown close all", server.Shutdown)
	mustBeClosed(t, conn)

	select {
	case <-server.hub.loopDone:
	default:
		t.Fatal("hub loop restarted after shutdown")
	}
}

func TestServerShutdownClosesConcurrentRegistration(t *testing.T) {
	for i := 0; i < 20; i++ {
		server := NewServer(nil)
		go server.hub.Run()
		conn := newConnection(server.hub, nil)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			server.hub.Register(conn)
		}()
		go func() {
			defer wg.Done()
			server.Shutdown()
		}()
		mustComplete(t, "concurrent registration and shutdown", wg.Wait)

		mustBeClosed(t, conn)
	}
}

func TestHubOperationsRemainSafeDuringShutdown(t *testing.T) {
	server := NewServer(nil)
	go server.hub.Run()
	server.hub.Register(newConnection(server.hub, nil))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			server.hub.Broadcast(protocol.MsgID_HeartbeatResp, protocol.HeartbeatResp{})
		}()
		go func() {
			defer wg.Done()
			_ = server.hub.OnlineCount()
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Shutdown()
	}()

	mustComplete(t, "broadcast, online count, and shutdown", wg.Wait)
}

func TestConnectionSendLockPreventsCloseRace(t *testing.T) {
	conn := newConnection(nil, nil)
	senderLocked := make(chan struct{})
	releaseSender := make(chan struct{})
	senderDone := make(chan struct{})
	go func() {
		conn.withSendLock(func() bool {
			close(senderLocked)
			<-releaseSender
			if conn.sendClosed {
				return false
			}
			select {
			case conn.send <- []byte("message"):
				return true
			default:
				return false
			}
		})
		close(senderDone)
	}()
	<-senderLocked

	closeDone := make(chan struct{})
	go func() {
		conn.closeSend()
		close(closeDone)
	}()
	mustWaitForWriter(t, &conn.sendMu)

	close(releaseSender)
	mustComplete(t, "sender during close", func() { <-senderDone })
	mustComplete(t, "close during send", func() { <-closeDone })
	mustBeClosed(t, conn)
}

func mustComplete(t *testing.T, operation string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(shutdownTestTimeout):
		t.Fatalf("%s did not return within %s", operation, shutdownTestTimeout)
	}
}

func mustBeClosed(t *testing.T, conn *Connection) {
	t.Helper()
	timer := time.NewTimer(shutdownTestTimeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-conn.send:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("send channel was not closed")
		}
	}
}

func mustWaitForWriter(t *testing.T, mu *sync.RWMutex) {
	t.Helper()
	deadline := time.Now().Add(shutdownTestTimeout)
	for time.Now().Before(deadline) {
		if mu.TryRLock() {
			mu.RUnlock()
			continue
		}
		return
	}
	t.Fatal("shutdown writer did not wait for the send lock")
}
