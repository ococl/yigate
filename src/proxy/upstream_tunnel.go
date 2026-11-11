package proxy

import (
	"bufio"
	"io"
	"net"
	"sync"
)

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Close()
}

func (c *bufferedConn) CloseRead() error {
	if cr, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return cr.CloseRead()
	}
	return nil
}

// Tunnel 在客户端与目标连接之间双向转发数据
// clientReader 使用 bufio.Reader 是为了保留已经预读的字节
// trafficKey 用于流量审计统计（可选，为零值时不统计）
// accounter 用于流量审计（可选，为 nil 时不统计）
func Tunnel(clientConn net.Conn, clientReader *bufio.Reader, targetConn net.Conn, trafficKey TrafficKey, accounter *TrafficAccounter) {
	defer targetConn.Close()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var src io.Reader
		if clientReader != nil {
			src = clientReader
		} else {
			src = clientConn
		}
		dr := &deadlineReader{r: src, conn: clientConn, timeout: TunnelIdleTimeout(), metricDirection: "client_to_target", trafficKey: trafficKey, accounter: accounter, auditDirection: "in"}
		dst := &deadlineWriter{w: targetConn, conn: targetConn, timeout: TunnelIdleTimeout()}
		_, _ = io.Copy(dst, dr)
		closeWrite(targetConn)
		closeRead(clientConn)
	}()

	go func() {
		defer wg.Done()
		src := &deadlineReader{r: targetConn, conn: targetConn, timeout: TunnelIdleTimeout(), metricDirection: "target_to_client", trafficKey: trafficKey, accounter: accounter, auditDirection: "out"}
		dst := &deadlineWriter{w: clientConn, conn: clientConn, timeout: TunnelIdleTimeout()}
		_, _ = io.Copy(dst, src)
		closeWrite(clientConn)
		closeRead(targetConn)
	}()

	wg.Wait()
}

func closeWrite(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = c.CloseWrite()
		return
	}
	_ = conn.Close()
}

// closeRead 在半关闭语义下关闭读方向。
// 注意不能退化为整体 Close：TLS 等包装连接不支持 CloseRead，直接 Close 会把
// 另一方向尚未传完的数据一并切断（例如对端读到 EOF 后才回写响应的场景）。
// 此时保持连接打开，由对端结束或空闲超时收尾，Tunnel 返回前会统一 Close。
func closeRead(conn net.Conn) {
	if c, ok := conn.(interface{ CloseRead() error }); ok {
		_ = c.CloseRead()
	}
}
