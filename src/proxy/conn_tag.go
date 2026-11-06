package proxy

import (
	"io"
	"net"
)

// upstreamTaggedConn 包装上游连接并携带选中的上游名称，
// 供流量审计按上游聚合，避免为所有拨号函数增加返回值。
type upstreamTaggedConn struct {
	net.Conn
	upstream string
}

// ReadFrom 透传底层 ReaderFrom 能力，保留零拷贝路径。
func (c *upstreamTaggedConn) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := c.Conn.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{c.Conn}, r)
}

// tagUpstream 标记连接使用的上游名称。
func tagUpstream(conn net.Conn, name string) net.Conn {
	if conn == nil || name == "" {
		return conn
	}
	if _, ok := conn.(*upstreamTaggedConn); ok {
		return conn
	}
	return &upstreamTaggedConn{Conn: conn, upstream: name}
}

// upstreamNameOf 返回连接携带的上游名称，直连等场景返回空字符串。
func upstreamNameOf(conn net.Conn) string {
	if conn == nil {
		return ""
	}
	if tagged, ok := conn.(*upstreamTaggedConn); ok {
		return tagged.upstream
	}
	return ""
}

// newTrafficKey 构造流量审计键。
func newTrafficKey(user string, conn net.Conn, target string) TrafficKey {
	return TrafficKey{
		User:     user,
		Upstream: upstreamNameOf(conn),
		Target:   targetHostFromAddr(target),
	}
}
