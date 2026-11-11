package proxy

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/emmansun/gmsm/smx509"
)

// 本文件覆盖 auto 出站在"多上游 + 并发"下的行为：
// 协商结果按上游实例记忆，一个上游的降级不能带偏另一个上游的协议选择；
// 健康检查探测与业务拨号共用同一个协商器，避免探测结论与数据面互相矛盾。

// writeCAPEM 把 CA 证书写到临时目录，供上游校验使用。
func writeCAPEM(t testing.TB, dir, name string, certPEM []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return path
}

// newAutoTestUpstream 构造 auto 协议、指向 127.0.0.1 的 https 上游。
func newAutoTestUpstream(t testing.TB, name, addr, caFile string) *Upstream {
	t.Helper()
	u, err := url.Parse("https://" + addr)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	return &Upstream{
		Name:          name,
		URL:           u,
		Types:         []string{"https"},
		Priority:      1,
		Weight:        1,
		TLSProtocol:   TLSProtocolAuto,
		TLSRootCAFile: caFile,
		TLSServerName: "localhost",
		tlsNeg:        &tlsNegotiator{},
	}
}

// startGMServerFor 启动只接受 TLCP 的服务端，返回地址与用于校验的 CA 文件。
func startGMServerFor(t testing.TB, dir, prefix string) (addr, caFile string) {
	t.Helper()
	ca := newGMTestCA(t)
	signCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature)
	encCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageKeyEncipherment)
	signCertFile, signKeyFile := writeGMTestPair(t, dir, prefix+"-sign", signCert)
	encCertFile, encKeyFile := writeGMTestPair(t, dir, prefix+"-enc", encCert)

	_, addr = startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolGM,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
	})
	return addr, writeCAPEM(t, dir, prefix+"-ca.crt", ca.certPEM)
}

// startTLSServerFor 启动只接受标准 TLS 的服务端，用于验证降级路径。
func startTLSServerFor(t testing.TB, dir, prefix string) (addr, caFile string) {
	t.Helper()
	ca := newStandardTestCA(t)
	certPEM, keyPEM := ca.issue(t, "localhost", x509.ExtKeyUsageServerAuth)
	certFile, keyFile := writePEMPair(t, dir, prefix, certPEM, keyPEM)

	_, addr = startGMTestServer(t, ServerTLSOptions{
		Protocol: TLSProtocolTLS,
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	return addr, writeCAPEM(t, dir, prefix+"-ca.crt", ca.certPEM)
}

// TestHealthCheckTLSUpstreamFollowsNegotiatedProtocol 锁定健康检查走与数据面相同的协商器。
// 若探测仍用标准 TLS，国密上游会被稳定判定为不可用并被摘除。
func TestHealthCheckTLSUpstreamFollowsNegotiatedProtocol(t *testing.T) {
	dir := t.TempDir()

	gmAddr, gmCA := startGMServerFor(t, dir, "gm")
	gmUpstream := newAutoTestUpstream(t, "gm-health", gmAddr, gmCA)
	if !checkTLSUpstream(gmUpstream, gmAddr, 5*time.Second) {
		t.Fatal("TLCP upstream must be healthy when probed with the negotiated protocol")
	}
	if got, _ := gmUpstream.tlsNeg.state(); got != TLSProtocolGM {
		t.Fatalf("TLCP upstream negotiated %q, want %q", got, TLSProtocolGM)
	}

	tlsAddr, tlsCA := startTLSServerFor(t, dir, "std")
	tlsUpstream := newAutoTestUpstream(t, "tls-health", tlsAddr, tlsCA)
	if !checkTLSUpstream(tlsUpstream, tlsAddr, 5*time.Second) {
		t.Fatal("standard TLS upstream must be healthy after falling back")
	}
	if got, _ := tlsUpstream.tlsNeg.state(); got != TLSProtocolTLS {
		t.Fatalf("standard TLS upstream negotiated %q, want %q", got, TLSProtocolTLS)
	}
}

// TestOutboundAutoConcurrentMixedUpstreams 让国密与标准 TLS 上游并存并接受并发拨号。
// 每轮都会走一遍"先试探 TLCP、失败再降级"的完整路径，可以同时暴露
// 协商结果串扰与协商器数据竞争（配合 -race）。
func TestOutboundAutoConcurrentMixedUpstreams(t *testing.T) {
	dir := t.TempDir()

	type target struct {
		name string
		up   *Upstream
		want string
	}
	var targets []target
	for i := 0; i < 2; i++ {
		prefix := fmt.Sprintf("gm%d", i)
		addr, caFile := startGMServerFor(t, dir, prefix)
		targets = append(targets, target{prefix, newAutoTestUpstream(t, prefix, addr, caFile), TLSProtocolGM})
	}
	for i := 0; i < 2; i++ {
		prefix := fmt.Sprintf("std%d", i)
		addr, caFile := startTLSServerFor(t, dir, prefix)
		targets = append(targets, target{prefix, newAutoTestUpstream(t, prefix, addr, caFile), TLSProtocolTLS})
	}

	const (
		workers = 8
		rounds  = 6
	)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		errs  []string
		total int
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				for _, tg := range targets {
					conn, err := dialProxyConnWith(tg.up, tg.up.URL, 5*time.Second)
					if err != nil {
						mu.Lock()
						if len(errs) < 5 {
							errs = append(errs, fmt.Sprintf("%s: %v", tg.name, err))
						}
						mu.Unlock()
						continue
					}
					_ = conn.Close()
					mu.Lock()
					total++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("concurrent dial failures: %v", errs)
	}
	if want := workers * rounds * len(targets); total != want {
		t.Fatalf("successful dials = %d, want %d", total, want)
	}
	// 协议记忆必须按上游隔离：标准 TLS 上游的降级不能让国密上游跟着降级。
	for _, tg := range targets {
		if got, _ := tg.up.tlsNeg.state(); got != tg.want {
			t.Errorf("%s negotiated %q, want %q", tg.name, got, tg.want)
		}
	}
}
