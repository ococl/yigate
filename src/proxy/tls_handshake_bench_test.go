package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"gitee.com/Trisia/gotlcp/tlcp"
	"github.com/emmansun/gmsm/smx509"
)

// 本文件提供国密适配的握手性能基线。
//
// 密评现场关心的是"新建连接能力"：TLCP 使用 SM2 签名，和 RSA 不是同一量级，
// 和 ECDSA P-256 才是可比对象，因此基准里两组标准 TLS 对照都保留。
// SM4 是否走硬件指令取决于 CPU，所以数字必须可复现，不能靠某台机器的偶然表现。
//
// 运行方式：
//   go test ./proxy -run '^$' -bench 'BenchmarkHandshake' -benchtime=3s -count=1
//
// 注意：默认 tlcp/tls Config 的会话缓存为 nil，因此每次迭代都是完整握手，
// 不含 session resumption 的加速效果。

const benchEchoPayload = 32 * 1024

func benchGMClientPool(b testing.TB, ca gmTestCert) *smx509.CertPool {
	b.Helper()
	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.certPEM) {
		b.Fatal("append SM2 CA failed")
	}
	return pool
}

func benchDialTLCP(b testing.TB, addr string, cfg *tlcp.Config) {
	b.Helper()
	conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, cfg)
	if err != nil {
		b.Fatalf("tlcp dial: %v", err)
	}
	_ = conn.Close()
}

func benchDialTLS(b testing.TB, addr string, cfg *tls.Config) {
	b.Helper()
	conn, err := tls.DialWithDialer(gmClientDialer(), "tcp", addr, cfg)
	if err != nil {
		b.Fatalf("tls dial: %v", err)
	}
	_ = conn.Close()
}

// benchGMTLSServer 用给定证书启动一个监听：protocol 决定是否同时支持 TLCP。
// 返回联调地址与 SM2 CA，便于同一端口上对比两类客户端。
func benchGMTLSServer(b testing.TB, protocol string, certPEM, keyPEM []byte) (string, gmTestCert) {
	b.Helper()
	dir := b.TempDir()
	certFile, keyFile := writePEMPair(b, dir, "server", certPEM, keyPEM)

	gmCA := newGMTestCA(b)
	signCertFile, signKeyFile := writeGMTestPair(b, dir, "sign",
		issueGMTestCert(b, gmCA, "localhost", smx509.KeyUsageDigitalSignature))
	encCertFile, encKeyFile := writeGMTestPair(b, dir, "enc",
		issueGMTestCert(b, gmCA, "localhost", smx509.KeyUsageKeyEncipherment))

	_, addr := startGMTestServer(b, ServerTLSOptions{
		Protocol:       protocol,
		CertFile:       certFile,
		KeyFile:        keyFile,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
	})
	return addr, gmCA
}

func benchStandardClientConfig(b testing.TB, caPEM []byte) *tls.Config {
	b.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		b.Fatal("append TLS CA failed")
	}
	return &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12}
}

// ecdsaTestCA 提供 ECDSA P-256 对照：与 SM2 同为 256 位椭圆曲线，
// 二者对比才能反映国密套件本身的开销，而不是 RSA 与 ECC 的差异。
type ecdsaTestCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newECDSATestCA(t testing.TB) ecdsaTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "yigate test ECDSA CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ECDSA CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ECDSA CA certificate: %v", err)
	}
	return ecdsaTestCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (ca ecdsaTestCA) issue(t testing.TB, cn string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create ECDSA leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ECDSA leaf key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func BenchmarkHandshake(b *testing.B) {
	cases := []struct {
		name     string
		protocol string
		gm       bool
		ecdsa    bool
	}{
		{"StandardTLS_RSA2048", TLSProtocolTLS, false, false},
		{"StandardTLS_ECDSA_P256", TLSProtocolTLS, false, true},
		{"TLCP_SM2", TLSProtocolGM, true, false},
		{"AutoListener_StandardTLS_RSA2048", TLSProtocolAuto, false, false},
		{"AutoListener_TLCP_SM2", TLSProtocolAuto, true, false},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			var (
				addr        string
				gmCA        gmTestCert
				stdClientCf *tls.Config
			)
			if tc.ecdsa {
				eca := newECDSATestCA(b)
				certPEM, keyPEM := eca.issue(b, "localhost", x509.ExtKeyUsageServerAuth)
				addr, gmCA = benchGMTLSServer(b, tc.protocol, certPEM, keyPEM)
				stdClientCf = benchStandardClientConfig(b, eca.certPEM)
			} else {
				stdCA := newStandardTestCA(b)
				certPEM, keyPEM := stdCA.issue(b, "localhost", x509.ExtKeyUsageServerAuth)
				addr, gmCA = benchGMTLSServer(b, tc.protocol, certPEM, keyPEM)
				stdClientCf = benchStandardClientConfig(b, stdCA.certPEM)
			}
			if tc.gm {
				cfg := &tlcp.Config{RootCAs: benchGMClientPool(b, gmCA), ServerName: "localhost"}
				// 预热一次：首次握手要加载证书链，不计入稳态。
				benchDialTLCP(b, addr, cfg)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					benchDialTLCP(b, addr, cfg)
				}
				return
			}
			// 两侧都做完整证书校验，避免把"跳过验签"的收益算进标准 TLS。
			benchDialTLS(b, addr, stdClientCf)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchDialTLS(b, addr, stdClientCf)
			}
		})
	}
}

// BenchmarkTLCPThroughput 测数据面吞吐：SM4-GCM 的对称加密开销。
// 与握手基准分开，避免把 CPU 指令集差异混进建连成本里。
func BenchmarkTLCPThroughput(b *testing.B) {
	stdCA := newStandardTestCA(b)
	certPEM, keyPEM := stdCA.issue(b, "localhost", x509.ExtKeyUsageServerAuth)
	addr, gmCA := benchGMTLSServer(b, TLSProtocolGM, certPEM, keyPEM)

	conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, &tlcp.Config{
		RootCAs:    benchGMClientPool(b, gmCA),
		ServerName: "localhost",
	})
	if err != nil {
		b.Fatalf("tlcp dial: %v", err)
	}
	defer conn.Close()
	benchEcho(b, conn)
}

// BenchmarkStandardTLSThroughput 作为对照。
func BenchmarkStandardTLSThroughput(b *testing.B) {
	stdCA := newStandardTestCA(b)
	certPEM, keyPEM := stdCA.issue(b, "localhost", x509.ExtKeyUsageServerAuth)
	addr, _ := benchGMTLSServer(b, TLSProtocolTLS, certPEM, keyPEM)

	conn, err := tls.DialWithDialer(gmClientDialer(), "tcp", addr, benchStandardClientConfig(b, stdCA.certPEM))
	if err != nil {
		b.Fatalf("tls dial: %v", err)
	}
	defer conn.Close()
	benchEcho(b, conn)
}

// benchEcho 在一条长连接上反复收发固定长度数据，报告 MB/s。
func benchEcho(b *testing.B, conn net.Conn) {
	b.Helper()
	payload := make([]byte, benchEchoPayload)
	buf := make([]byte, benchEchoPayload)
	b.SetBytes(benchEchoPayload)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.Write(payload); err != nil {
			b.Fatalf("write: %v", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, err := readFull(conn, buf); err != nil {
			b.Fatalf("read: %v", err)
		}
	}
}

// readFull 读满 buf，避免单次 Read 返回部分数据导致基准读数偏小。
func readFull(conn net.Conn, buf []byte) (int, error) {
	read := 0
	for read < len(buf) {
		n, err := conn.Read(buf[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}
