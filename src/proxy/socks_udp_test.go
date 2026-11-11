package proxy

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func newTestUDPRelay() *udpRelay {
	return &udpRelay{sessions: make(map[string]*udpSession), quit: make(chan struct{})}
}

// TestUDPRelaySessionCap 验证目标会话数有硬上限，且淘汰的是最久未使用的会话。
func TestUDPRelaySessionCap(t *testing.T) {
	r := newTestUDPRelay()
	defer r.closeSessions()

	for i := 0; i < maxUDPSessions+32; i++ {
		target := fmt.Sprintf("127.0.0.1:%d", 20000+i)
		if err := r.forwardToTarget(target, []byte("x")); err != nil {
			t.Fatalf("forward to %s: %v", target, err)
		}
		if got := len(r.sessions); got > maxUDPSessions {
			t.Fatalf("session count %d exceeds cap %d", got, maxUDPSessions)
		}
	}
	if got := len(r.sessions); got != maxUDPSessions {
		t.Fatalf("expected exactly %d sessions, got %d", maxUDPSessions, got)
	}
	r.mu.Lock()
	_, firstKept := r.sessions["127.0.0.1:20000"]
	_, lastKept := r.sessions[fmt.Sprintf("127.0.0.1:%d", 20000+maxUDPSessions+31)]
	r.mu.Unlock()
	if firstKept {
		t.Fatal("oldest session should have been evicted")
	}
	if !lastKept {
		t.Fatal("newest session should be present")
	}
}

// TestUDPRelayTouchSessionByKey 验证活跃时间刷新按 key 定位，
// 被淘汰会话的迟到回程包不会误刷新同名新会话。
func TestUDPRelayTouchSessionByKey(t *testing.T) {
	r := newTestUDPRelay()
	old := &udpSession{key: "127.0.0.1:20000", lastUsed: time.Now().Add(-time.Hour)}
	fresh := &udpSession{key: "127.0.0.1:20000", lastUsed: time.Now().Add(-time.Hour)}
	r.sessions[old.key] = fresh

	before := fresh.lastUsed
	r.touchSession(old)
	if !fresh.lastUsed.Equal(before) {
		t.Fatal("evicted session must not touch the replacement session")
	}
	r.touchSession(fresh)
	if !fresh.lastUsed.After(before) {
		t.Fatal("active session should have its lastUsed refreshed")
	}
	if r.sessionActive(old) {
		t.Fatal("evicted session must not be reported active")
	}
	if !r.sessionActive(fresh) {
		t.Fatal("current session should be active")
	}
}

// TestUDPRelayEvictedSessionStopsReading 验证被淘汰会话的读循环静默退出。
func TestUDPRelayEvictedSessionStopsReading(t *testing.T) {
	r := newTestUDPRelay()
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r.ln = ln
	defer ln.Close()
	if err := r.forwardToTarget("127.0.0.1:20000", []byte("x")); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	sess := r.sessions["127.0.0.1:20000"]
	r.evictOldestSessionLocked()
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.readFromTarget(sess)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("evicted session read loop did not exit")
	}
}
