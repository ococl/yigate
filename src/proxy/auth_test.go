package proxy

import (
	"encoding/base64"
	"net/http"
	"testing"
)

func TestAuthRequired(t *testing.T) {
	if AuthRequired(nil) {
		t.Fatal("expected no auth required for nil users")
	}
	if !AuthRequired(map[string]string{"u": "p"}) {
		t.Fatal("expected auth required for non-empty users")
	}
}

func TestAuthenticate(t *testing.T) {
	users := map[string]string{"alice": "secret", "bob": "pass"}
	if !Authenticate("alice", "secret", users) {
		t.Fatal("expected valid credentials to pass")
	}
	if Authenticate("alice", "wrong", users) {
		t.Fatal("expected wrong password to fail")
	}
	if Authenticate("eve", "secret", users) {
		t.Fatal("expected unknown user to fail")
	}
	if !Authenticate("any", "any", nil) {
		t.Fatal("expected any auth when users empty")
	}
}

func TestHTTPAuthOK(t *testing.T) {
	users := map[string]string{"alice": "secret"}
	h := http.Header{}
	user, ok := HTTPAuthOK(h, nil)
	if !ok || user != "" {
		t.Fatal("expected no-auth mode to pass with empty user")
	}

	_, ok = HTTPAuthOK(h, users)
	if ok {
		t.Fatal("expected missing header to fail")
	}

	h.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")))
	user, ok = HTTPAuthOK(h, users)
	if !ok || user != "alice" {
		t.Fatal("expected valid basic auth to pass")
	}

	h.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:wrong")))
	_, ok = HTTPAuthOK(h, users)
	if ok {
		t.Fatal("expected wrong password to fail")
	}

	h.Set("Proxy-Authorization", "invalid")
	_, ok = HTTPAuthOK(h, users)
	if ok {
		t.Fatal("expected malformed header to fail")
	}

	h.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("malformed")))
	_, ok = HTTPAuthOK(h, users)
	if ok {
		t.Fatal("expected missing colon to fail")
	}
}
