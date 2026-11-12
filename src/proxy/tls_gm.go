package proxy

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"gitee.com/Trisia/gotlcp/pa"
	"gitee.com/Trisia/gotlcp/tlcp"
	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
)

// 传输层国密（TLCP，GB/T 38636-2020）适配。
//
// 设计原则：
//   - protocol=tls 时行为与改造前完全一致，存量配置不受影响；
//   - protocol=gm 时强制 TLCP，握手失败即失败，不做降级；
//   - protocol=auto 时 GM 优先、失败回落标准 TLS，并对协商结果做记忆，
//     避免对不支持 TLCP 的上游每条连接都多一次失败握手。
const (
	TLSProtocolAuto = "auto"
	TLSProtocolGM   = "gm"
	TLSProtocolTLS  = "tls"

	// tlsDowngradeRetryInterval 是 auto 模式降级到标准 TLS 后，
	// 重新尝试 GM 的冷却时间。
	tlsDowngradeRetryInterval = 5 * time.Minute

	// gmSessionCacheSize 是 TLCP 会话缓存容量。
	gmSessionCacheSize = 128
)

// gmCipherSuiteNames 建立配置名到 TLCP 密码套件 ID 的映射。
var gmCipherSuiteNames = map[string]uint16{
	"ECDHE_SM4_GCM_SM3": tlcp.ECDHE_SM4_GCM_SM3,
	"ECC_SM4_GCM_SM3":   tlcp.ECC_SM4_GCM_SM3,
	"ECDHE_SM4_CBC_SM3": tlcp.ECDHE_SM4_CBC_SM3,
	"ECC_SM4_CBC_SM3":   tlcp.ECC_SM4_CBC_SM3,
}

// defaultGMCipherSuites 是内置优先级：GCM 在前，兼顾完整性与机密性。
var defaultGMCipherSuites = []uint16{
	tlcp.ECDHE_SM4_GCM_SM3,
	tlcp.ECC_SM4_GCM_SM3,
	tlcp.ECDHE_SM4_CBC_SM3,
}

// GMCipherSuiteNames 返回支持的 TLCP 密码套件名称，供配置示例与校验提示使用。
func GMCipherSuiteNames() []string {
	return []string{"ECDHE_SM4_GCM_SM3", "ECC_SM4_GCM_SM3", "ECDHE_SM4_CBC_SM3", "ECC_SM4_CBC_SM3"}
}

// NormalizeTLSProtocol 归一化协议取值，空值按 auto 处理。
func NormalizeTLSProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TLSProtocolGM:
		return TLSProtocolGM
	case TLSProtocolTLS:
		return TLSProtocolTLS
	default:
		return TLSProtocolAuto
	}
}

// ValidateTLSProtocol 校验协议取值。
func ValidateTLSProtocol(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", TLSProtocolAuto, TLSProtocolGM, TLSProtocolTLS:
		return nil
	default:
		return fmt.Errorf("unsupported tls protocol %q (want auto, gm or tls)", value)
	}
}

// ResolveGMCipherSuites 把配置中的套件名解析为 ID，空列表返回内置优先级。
func ResolveGMCipherSuites(names []string) ([]uint16, error) {
	if len(names) == 0 {
		return append([]uint16(nil), defaultGMCipherSuites...), nil
	}
	suites := make([]uint16, 0, len(names))
	seen := make(map[uint16]struct{}, len(names))
	for _, raw := range names {
		name := strings.ToUpper(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		id, ok := gmCipherSuiteNames[name]
		if !ok {
			return nil, fmt.Errorf("unsupported tls.cipher_suites entry %q (want one of %s)", raw, strings.Join(GMCipherSuiteNames(), ", "))
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		suites = append(suites, id)
	}
	if len(suites) == 0 {
		return nil, errors.New("tls.cipher_suites must not be empty")
	}
	return suites, nil
}

// ServerTLSOptions 描述入站 TLS 材料。
//
// 标准 TLS 与 TLCP 使用两套互不通用的密钥材料：TLCP 要求 SM2 双证书
// （签名证书 + 加密证书），而 Go 标准库的 crypto/tls 不支持 SM2 曲线。
// 因此 auto 模式必须同时配置两套证书才能同时服务两类客户端。
type ServerTLSOptions struct {
	Protocol string

	// 标准 TLS 证书（RSA/ECDSA）。
	CertFile string
	KeyFile  string

	// TLCP 签名证书与加密证书（SM2，GB/T 38636-2020）。
	GMSignCertFile    string
	GMSignKeyFile     string
	GMSignKeyPassword string
	GMEncCertFile     string
	GMEncKeyFile      string
	GMEncKeyPassword  string

	ClientCAFile string
	CipherSuites []string
}

// HasCertificate 返回是否配置了标准 TLS 证书。
func (o ServerTLSOptions) HasCertificate() bool {
	return strings.TrimSpace(o.CertFile) != "" && strings.TrimSpace(o.KeyFile) != ""
}

// HasGMCertificate 返回是否配置了完整的 TLCP 双证书。
func (o ServerTLSOptions) HasGMCertificate() bool {
	return strings.TrimSpace(o.GMSignCertFile) != "" && strings.TrimSpace(o.GMSignKeyFile) != "" &&
		strings.TrimSpace(o.GMEncCertFile) != "" && strings.TrimSpace(o.GMEncKeyFile) != ""
}

// HasAnyCertificate 返回是否配置了任意一套证书。
func (o ServerTLSOptions) HasAnyCertificate() bool {
	return o.HasCertificate() || strings.TrimSpace(o.GMSignCertFile) != "" || strings.TrimSpace(o.GMEncCertFile) != ""
}

// NewServerListener 按协议包装底层监听器：
//   - tls：标准 TLS 终止（行为与改造前一致）；
//   - gm：强制 TLCP；
//   - auto：同时配置两套证书时启用 TLCP/TLS 自适应，否则用可用的那一种。
func NewServerListener(inner net.Listener, o ServerTLSOptions) (net.Listener, error) {
	if inner == nil {
		return nil, errors.New("tls: nil listener")
	}
	protocol := NormalizeTLSProtocol(o.Protocol)
	hasTLS := o.HasCertificate()
	hasGM := o.HasGMCertificate()

	switch protocol {
	case TLSProtocolTLS:
		if !hasTLS {
			return nil, errors.New("tls.protocol tls requires tls.cert_file and tls.key_file")
		}
		cfg, err := buildStandardServerTLSConfig(o)
		if err != nil {
			return nil, err
		}
		return tls.NewListener(inner, cfg), nil
	case TLSProtocolGM:
		if !hasGM {
			return nil, errors.New("tls.protocol gm requires tls.gm_sign_cert_file/gm_sign_key_file and tls.gm_enc_cert_file/gm_enc_key_file")
		}
		cfg, err := buildServerTLCPConfig(o)
		if err != nil {
			return nil, err
		}
		return tlcp.NewListener(inner, cfg), nil
	default:
		if !hasTLS && !hasGM {
			return inner, nil
		}
		if hasTLS && hasGM {
			tlsCfg, err := buildStandardServerTLSConfig(o)
			if err != nil {
				return nil, err
			}
			tlcpCfg, err := buildServerTLCPConfig(o)
			if err != nil {
				return nil, err
			}
			return pa.NewListener(inner, tlcpCfg, tlsCfg), nil
		}
		if hasGM {
			cfg, err := buildServerTLCPConfig(o)
			if err != nil {
				return nil, err
			}
			return tlcp.NewListener(inner, cfg), nil
		}
		cfg, err := buildStandardServerTLSConfig(o)
		if err != nil {
			return nil, err
		}
		return tls.NewListener(inner, cfg), nil
	}
}

func buildStandardServerTLSConfig(o ServerTLSOptions) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair: %w", err)
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if strings.TrimSpace(o.ClientCAFile) != "" {
		pool, err := loadStandardClientCAs(o.ClientCAFile)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return config, nil
}

// loadStandardClientCAs 逐块解析客户端 CA 文件，只收编标准库能解析的证书。
//
// auto 模式下一个文件里可能同时放着 SM2 CA 与标准 TLS CA：
// crypto/x509 无法解析 SM2 证书（签名算法与曲线均不被支持），
// 直接调用 AppendCertsFromPEM 会整份失败，导致端口起不来。
// 因此这里逐块尝试，跳过非 X.509 的块，只要求至少有一张可用于标准 TLS。
func loadStandardClientCAs(path string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read client CA file: %w", err)
	}
	pool := x509.NewCertPool()
	count := 0
	rest := caPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			// 可能是 SM2 证书，交给 TLCP 侧使用。
			continue
		}
		pool.AddCert(cert)
		count++
	}
	if count == 0 {
		return nil, fmt.Errorf("client CA file %q contains no standard X.509 certificate; "+
			"auto/gm 混用 mTLS 时请把标准 TLS CA 与 SM2 CA 放在同一个文件里", path)
	}
	return pool, nil
}

func buildServerTLCPConfig(o ServerTLSOptions) (*tlcp.Config, error) {
	if !o.HasGMCertificate() {
		return nil, errors.New("TLCP requires gm_sign_cert_file/gm_sign_key_file and gm_enc_cert_file/gm_enc_key_file")
	}
	sigCert, err := LoadSM2KeyPair(o.GMSignCertFile, o.GMSignKeyFile, o.GMSignKeyPassword)
	if err != nil {
		return nil, fmt.Errorf("load TLCP signing key pair: %w", err)
	}
	encCert, err := LoadSM2KeyPair(o.GMEncCertFile, o.GMEncKeyFile, o.GMEncKeyPassword)
	if err != nil {
		return nil, fmt.Errorf("load TLCP encryption key pair: %w", err)
	}
	suites, err := ResolveGMCipherSuites(o.CipherSuites)
	if err != nil {
		return nil, err
	}
	config := &tlcp.Config{
		Certificates: []tlcp.Certificate{sigCert, encCert},
		CipherSuites: suites,
		SessionCache: tlcp.NewLRUSessionCache(gmSessionCacheSize),
	}
	if strings.TrimSpace(o.ClientCAFile) != "" {
		caPEM, err := os.ReadFile(o.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA file: %w", err)
		}
		pool := smx509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse client CA certificate")
		}
		config.ClientCAs = pool
		config.ClientAuth = tlcp.RequireAndVerifyClientCert
	}
	return config, nil
}

// LoadSM2KeyPair 加载 SM2 证书与私钥对，password 非空时按加密 PKCS#8 解析。
func LoadSM2KeyPair(certFile, keyFile, password string) (tlcp.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tlcp.Certificate{}, err
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return tlcp.Certificate{}, err
	}
	return ParseSM2KeyPair(certPEM, keyPEM, password)
}

// ParseSM2KeyPair 解析 PEM 编码的 SM2 证书与私钥对。
// 无口令时优先走 gotlcp 的标准解析；有口令时用 gmsm 解密 PKCS#8。
func ParseSM2KeyPair(certPEM, keyPEM []byte, password string) (tlcp.Certificate, error) {
	if strings.TrimSpace(password) == "" {
		cert, err := tlcp.X509KeyPair(certPEM, keyPEM)
		if err == nil {
			return cert, nil
		}
		// 明文解析失败时继续尝试 PKCS#8，便于兼容其他编码形式。
	}
	derCerts, err := parseCertificatePEM(certPEM)
	if err != nil {
		return tlcp.Certificate{}, err
	}
	keyDER, err := firstPrivateKeyDER(keyPEM)
	if err != nil {
		return tlcp.Certificate{}, err
	}
	priv, err := pkcs8.ParsePKCS8PrivateKeySM2(keyDER, []byte(password))
	if err != nil {
		return tlcp.Certificate{}, fmt.Errorf("parse SM2 private key: %w", err)
	}
	leaf, err := smx509.ParseCertificate(derCerts[0])
	if err != nil {
		return tlcp.Certificate{}, err
	}
	if err := verifySM2KeyPair(leaf, priv); err != nil {
		return tlcp.Certificate{}, err
	}
	return tlcp.Certificate{
		Certificate: derCerts,
		PrivateKey:  priv,
		Leaf:        leaf,
	}, nil
}

func parseCertificatePEM(certPEM []byte) ([][]byte, error) {
	var derCerts [][]byte
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			derCerts = append(derCerts, block.Bytes)
		}
	}
	if len(derCerts) == 0 {
		return nil, errors.New("failed to find any CERTIFICATE PEM block")
	}
	return derCerts, nil
}

func firstPrivateKeyDER(keyPEM []byte) ([]byte, error) {
	rest := keyPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "PRIVATE KEY" || strings.HasSuffix(block.Type, " PRIVATE KEY") {
			return block.Bytes, nil
		}
	}
	return nil, errors.New("failed to find any PRIVATE KEY PEM block")
}

func verifySM2KeyPair(leaf *smx509.Certificate, priv *sm2.PrivateKey) error {
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("tls: certificate public key is not an SM2 key")
	}
	// 用 Equal 而不是逐坐标比较：X/Y 自 Go 1.26 起已废弃，
	// 直接读原始坐标可能得到非法的密钥表示。
	if !pub.Equal(&priv.PublicKey) {
		return errors.New("tls: private key does not match public key")
	}
	return nil
}

// ClientTLSOptions 描述出站 TLS 材料。
type ClientTLSOptions struct {
	Protocol           string
	CAFile             string
	ServerName         string
	InsecureSkipVerify bool
}

func (o ClientTLSOptions) buildStandardConfig(addr string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		//nolint:gosec // G402: 该字段来自用户显式配置（默认 false），是排查自签证书上游的必要开关
		InsecureSkipVerify: o.InsecureSkipVerify,
	}
	if strings.TrimSpace(o.CAFile) != "" {
		caPEM, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read TLS CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse TLS CA certificate")
		}
		cfg.RootCAs = pool
	}
	if name := o.serverName(addr); name != "" {
		cfg.ServerName = name
	}
	return cfg, nil
}

func (o ClientTLSOptions) buildTLCPConfig(addr string) (*tlcp.Config, error) {
	cfg := &tlcp.Config{
		InsecureSkipVerify: o.InsecureSkipVerify,
		SessionCache:       tlcp.NewLRUSessionCache(gmSessionCacheSize),
	}
	if strings.TrimSpace(o.CAFile) != "" {
		caPEM, err := os.ReadFile(o.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read TLCP CA file: %w", err)
		}
		pool := smx509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse TLCP CA certificate")
		}
		cfg.RootCAs = pool
	}
	if name := o.serverName(addr); name != "" {
		cfg.ServerName = name
	}
	return cfg, nil
}

// serverName 返回 SNI：显式配置优先，其次从地址提取域名（IP 不发 SNI）。
func (o ClientTLSOptions) serverName(addr string) string {
	if name := strings.TrimSpace(o.ServerName); name != "" {
		return name
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// tlsNegotiator 记忆 auto 模式下的协议协商结果。
// 零值可用；所有方法并发安全。
type tlsNegotiator struct {
	mu             sync.Mutex
	protocol       string
	downgradeUntil time.Time
}

func (n *tlsNegotiator) remember(protocol string, downgradeUntil time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.protocol = protocol
	n.downgradeUntil = downgradeUntil
	n.mu.Unlock()
}

func (n *tlsNegotiator) state() (string, time.Time) {
	if n == nil {
		return "", time.Time{}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.protocol, n.downgradeUntil
}

// dial 按协议建立到 addr 的 TLS/TLCP 连接。
func (n *tlsNegotiator) dial(addr string, o ClientTLSOptions, dialer *net.Dialer) (net.Conn, error) {
	switch NormalizeTLSProtocol(o.Protocol) {
	case TLSProtocolGM:
		return dialTLCP(dialer, addr, o)
	case TLSProtocolTLS:
		return dialStandardTLS(dialer, addr, o)
	}

	memorized, downgradeUntil := n.state()
	if memorized == TLSProtocolTLS && time.Now().Before(downgradeUntil) {
		return dialStandardTLS(dialer, addr, o)
	}

	conn, gmErr := dialTLCP(dialer, addr, o)
	if gmErr == nil {
		n.remember(TLSProtocolGM, time.Time{})
		return conn, nil
	}
	// 曾经成功过 GM 时不因一次失败就永久降级，避免上游临时故障导致协议回落到标准 TLS。
	if memorized == TLSProtocolGM {
		return nil, gmErr
	}

	conn, tlsErr := dialStandardTLS(dialer, addr, o)
	if tlsErr != nil {
		return nil, fmt.Errorf("tlcp handshake failed (%v); tls handshake failed: %w", gmErr, tlsErr)
	}
	n.remember(TLSProtocolTLS, time.Now().Add(tlsDowngradeRetryInterval))
	return conn, nil
}

func dialTLCP(dialer *net.Dialer, addr string, o ClientTLSOptions) (net.Conn, error) {
	cfg, err := o.buildTLCPConfig(addr)
	if err != nil {
		return nil, err
	}
	return tlcp.DialWithDialer(dialer, "tcp", addr, cfg)
}

func dialStandardTLS(dialer *net.Dialer, addr string, o ClientTLSOptions) (net.Conn, error) {
	cfg, err := o.buildStandardConfig(addr)
	if err != nil {
		return nil, err
	}
	return tls.DialWithDialer(dialer, "tcp", addr, cfg)
}
