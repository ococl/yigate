package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

const maxBufferedRequestBody = 32 << 20

type responseBody struct {
	io.ReadCloser
	conn net.Conn
}

func (b *responseBody) Close() error {
	err := b.ReadCloser.Close()
	if closeErr := b.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

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

	if err := setConnDeadline(client, httpExchangeTimeout); err != nil {
		log.Printf("set http deadline: %v", err)
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
	if err := setConnDeadline(client, tunnelIdleTimeout); err != nil {
		log.Printf("set connect client deadline: %v", err)
		targetConn.Close()
		return
	}
	if err := setConnDeadline(targetConn, tunnelIdleTimeout); err != nil {
		log.Printf("set connect target deadline: %v", err)
		targetConn.Close()
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
		bodyBytes, err = io.ReadAll(io.LimitReader(req.Body, maxBufferedRequestBody+1))
		if err != nil {
			log.Printf("read request body: %v", err)
			writeResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		if len(bodyBytes) > maxBufferedRequestBody {
			log.Printf("request body too large: %d bytes", len(bodyBytes))
			writeResponse(client, "HTTP/1.1 413 Payload Too Large\r\n\r\n")
			return
		}
	}
	resetBody := func() {
		req.TransferEncoding = nil
		req.ContentLength = int64(len(bodyBytes))
		if len(bodyBytes) == 0 {
			req.Body = http.NoBody
			req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

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
	if err := setConnDeadline(client, httpExchangeTimeout); err != nil {
		log.Printf("set response deadline: %v", err)
		return
	}
	if err := resp.Write(client); err != nil {
		log.Printf("write response to client: %v", err)
	}
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
	if err := setConnDeadline(conn, httpExchangeTimeout); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, true, up); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
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
	if err := setConnDeadline(conn, httpExchangeTimeout); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
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
	if err := setConnDeadline(conn, httpExchangeTimeout); err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeHTTPRequest(conn, req, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

func targetHost(req *http.Request) string {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	port := "80"
	if req.URL != nil && req.URL.Scheme == "https" {
		port = "443"
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.JoinHostPort(host, port)
}

func dialRaw(up *Upstream) (net.Conn, error) {
	return dialProxyConn(up.URL, proxyDialTimeout)
}

func writeHTTPRequest(w io.Writer, req *http.Request, viaUpstream bool, up *Upstream) error {
	out := req.Clone(req.Context())
	out.RequestURI = ""
	out.Header = req.Header.Clone()
	out.Body = req.Body
	out.GetBody = req.GetBody
	if viaUpstream && up != nil {
		setProxyAuthorization(out.Header, up)
		return out.WriteProxy(w)
	}
	return out.Write(w)
}

func writeResponse(w io.Writer, statusLine string) {
	if _, err := w.Write([]byte(statusLine)); err != nil {
		log.Printf("write response: %v", err)
	}
}
