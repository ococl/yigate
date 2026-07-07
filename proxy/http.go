package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

// HandleHTTP 处理 HTTP/HTTPS 代理请求
// 该方法会复用 clientReader 中已经预读的字节
func HandleHTTP(client net.Conn, clientReader *bufio.Reader, users map[string]string, mgr *UpstreamManager) {
	defer client.Close()

	req, err := http.ReadRequest(clientReader)
	if err != nil {
		log.Printf("http read request: %v", err)
		return
	}
	defer req.Body.Close()

	if !HTTPAuthOK(req.Header, users) {
		writeResponse(client, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic\r\n\r\n")
		return
	}

	if req.Method == "CONNECT" {
		handleConnect(client, clientReader, req.Host, users, mgr)
		return
	}

	handleHTTPRequest(client, req, mgr)
}

func handleConnect(client net.Conn, clientReader *bufio.Reader, target string, users map[string]string, mgr *UpstreamManager) {
	targetConn, err := mgr.Dial(target)
	if err != nil {
		log.Printf("connect to %s failed: %v", target, err)
		writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	writeResponse(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	Tunnel(client, clientReader, targetConn)
}

func handleHTTPRequest(client net.Conn, req *http.Request, mgr *UpstreamManager) {
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")

	// 普通 HTTP 请求可能通过上游转发，失败时尝试其他上游或回退直连。
	// 为支持重试，先把请求体读到内存并允许重复设置。
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			log.Printf("read request body: %v", err)
			writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
	}
	resetBody := func() { req.Body = io.NopCloser(bytes.NewReader(bodyBytes)) }

	var resp *http.Response
	var err error
	for i := 0; i <= len(mgr.upstreams); i++ {
		up := mgr.Select()
		if up == nil {
			resetBody()
			resp, err = directRequest(req)
			break
		}
		resetBody()
		resp, err = forwardRequest(req, up)
		if err == nil {
			break
		}
		log.Printf("forward via upstream %s failed: %v", up.URL.Host, err)
		mgr.mu.Lock()
		up.dead = true
		mgr.mu.Unlock()
	}
	if err != nil {
		log.Printf("forward request: %v", err)
		writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	resp.Write(client)
}

func forwardRequest(req *http.Request, up *Upstream) (*http.Response, error) {
	if up.URL.Scheme == "http" || up.URL.Scheme == "https" {
		return viaHTTPUpstream(req, up)
	}
	return viaSOCKS5Upstream(req, up)
}

func viaHTTPUpstream(req *http.Request, up *Upstream) (*http.Response, error) {
	conn, err := dialRaw(up)
	if err != nil {
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, true); err != nil {
		conn.Close()
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(conn), req)
}

func viaSOCKS5Upstream(req *http.Request, up *Upstream) (*http.Response, error) {
	target := targetHost(req)
	if target == "" {
		return nil, errors.New("missing target host")
	}
	conn, err := up.Dial(target)
	if err != nil {
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false); err != nil {
		conn.Close()
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(conn), req)
}

func directRequest(req *http.Request) (*http.Response, error) {
	target := targetHost(req)
	if target == "" {
		return nil, errors.New("missing target host")
	}
	conn, err := DialTarget(target)
	if err != nil {
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false); err != nil {
		conn.Close()
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(conn), req)
}

func targetHost(req *http.Request) string {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if host == "" {
		return ""
	}
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	return host
}

func dialRaw(up *Upstream) (net.Conn, error) {
	if up.URL.Scheme == "https" {
		return tls.Dial("tcp", up.URL.Host, &tls.Config{InsecureSkipVerify: true})
	}
	return DialTarget(up.URL.Host)
}

func writeHTTPRequest(w io.Writer, req *http.Request, viaUpstream bool) error {
	var urlStr string
	if viaUpstream {
		urlStr = req.URL.String()
	} else {
		urlStr = req.URL.RequestURI()
	}
	line := fmt.Sprintf("%s %s %s\r\n", req.Method, urlStr, req.Proto)
	if _, err := w.Write([]byte(line)); err != nil {
		return err
	}
	if host := req.Host; host != "" {
		if _, err := w.Write([]byte("Host: " + host + "\r\n")); err != nil {
			return err
		}
	}
	if err := req.Header.Write(w); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\r\n")); err != nil {
		return err
	}
	if req.Body != nil {
		if _, err := io.Copy(w, req.Body); err != nil {
			return err
		}
	}
	return nil
}

func writeResponse(w io.Writer, statusLine string) {
	w.Write([]byte(statusLine))
}
