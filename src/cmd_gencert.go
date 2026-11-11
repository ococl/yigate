package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emmansun/gmsm/pkcs"
	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
)

// gencert 生成国密 TLCP 样板证书：SM2 根 CA + 签名证书 + 加密证书。
//
// 内置实现而非依赖外部 openssl：各平台自带 OpenSSL 对 SM2 的支持不一致，
// 而 yigate 已是静态二进制，用同一条命令就能在任意平台生成可用材料。
// 生成结果是自签样板，生产环境应换成具备资质的 CA 与密码模块。
func runGenCert(args []string) {
	fs := flag.NewFlagSet("gencert", flag.ExitOnError)
	outDir := fs.String("o", "certs/gm", "output directory")
	domain := fs.String("d", "localhost", "certificate DNS name")
	password := fs.String("p", "", "encrypt private keys with this password (PBES2-SM4)")
	force := fs.Bool("f", false, "overwrite existing files")
	caCert := fs.String("ca", "", "existing SM2 CA certificate; when set, no new CA is generated")
	caKey := fs.String("ca-key", "", "private key of the CA given by -ca")
	caKeyPassword := fs.String("ca-key-password", "", "password of -ca-key (supports ${ENV} references)")
	days := fs.Int("days", genCertDefaultDays, "validity of the signing/encryption certificates in days")
	checkPath := fs.String("check", "", "inspect certificates in a file or directory instead of generating")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: yigate gencert [-o <dir>] [-d <domain>] [-p <password>] [-days <n>] [-f]")
		fmt.Fprintln(os.Stderr, "       yigate gencert -ca <ca.crt> -ca-key <ca.key> [-ca-key-password <pwd>] [-o <dir>] [-d <domain>]")
		fmt.Fprintln(os.Stderr, "       yigate gencert -check <file|dir>")
		fmt.Fprintln(os.Stderr, "\nGenerates an SM2 CA plus TLCP signing and encryption certificates.")
		fmt.Fprintln(os.Stderr, "With -ca the signing/encryption certificates are issued by the given CA instead.")
		fmt.Fprintln(os.Stderr, "-check only reports subjects and expiry (exit code 1 when a certificate is expired or expiring).")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *checkPath != "" {
		os.Exit(runCertCheck(*checkPath))
	}

	err := generateGMCertificatesWithOptions(genCertOptions{
		OutDir:        *outDir,
		Domain:        *domain,
		KeyPassword:   *password,
		Force:         *force,
		CACertFile:    *caCert,
		CAKeyFile:     *caKey,
		CAKeyPassword: *caKeyPassword,
		Days:          *days,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gencert: %v\n", err)
		os.Exit(1)
	}
}

type gmCertMaterial struct {
	certPEM []byte
	keyPEM  []byte
	cert    *smx509.Certificate
	key     *sm2.PrivateKey
}

// gmCertFile 描述一个待写出的证书/私钥对。
type gmCertFile struct {
	name string
	m    gmCertMaterial
}

// expandCLIEnv 只在值里出现 ${VAR} 时才展开，避免把含 $ 的普通口令误当成占位符。
func expandCLIEnv(field, value string) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}
	return expandEnvStrict(field, value)
}

// 有效期默认值：叶子证书按常见实践一年一换，自带 CA 只作样板故给足十年。
const (
	genCertDefaultDays   = 365
	genCertDefaultCADays = 3650
	genCertMaxDays       = 36500 // 约 100 年，挡住明显笔误
)

// genCertOptions 收敛 gencert 的入参，便于测试覆盖"复用既有 CA"分支。
type genCertOptions struct {
	OutDir        string
	Domain        string
	KeyPassword   string
	Force         bool
	CACertFile    string
	CAKeyFile     string
	CAKeyPassword string
	Days          int
}

// generateGMCertificates 是生成整套 CA + 叶子证书的便捷入口，保留给测试与内部调用。
func generateGMCertificates(outDir, domain, password string, force bool) error {
	return generateGMCertificatesWithOptions(genCertOptions{
		OutDir:      outDir,
		Domain:      domain,
		KeyPassword: password,
		Force:       force,
	})
}

func generateGMCertificatesWithOptions(opts genCertOptions) error {
	domain := strings.TrimSpace(opts.Domain)
	if domain == "" {
		return fmt.Errorf("domain must not be empty")
	}
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		return fmt.Errorf("output directory must not be empty")
	}

	caCertFile := strings.TrimSpace(opts.CACertFile)
	caKeyFile := strings.TrimSpace(opts.CAKeyFile)
	reuseCA := caCertFile != "" || caKeyFile != ""
	if reuseCA && (caCertFile == "" || caKeyFile == "") {
		return fmt.Errorf("-ca and -ca-key must be provided together")
	}

	// 复用 CA 时不再生成 ca.crt/ca.key，避免把已有根证书覆盖成新的自签证书。
	files := []string{"sign.crt", "sign.key", "enc.crt", "enc.key"}
	if !reuseCA {
		files = append(files, "ca.crt", "ca.key")
	}
	if !opts.Force {
		for _, name := range files {
			if _, err := os.Stat(filepath.Join(outDir, name)); err == nil {
				return fmt.Errorf("%s already exists, use -f to overwrite", filepath.Join(outDir, name))
			}
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	days := opts.Days
	if days <= 0 {
		days = genCertDefaultDays
	}
	if days > genCertMaxDays {
		return fmt.Errorf("-days must not exceed %d", genCertMaxDays)
	}
	leafNotAfter := time.Now().AddDate(0, 0, days)

	var ca gmCertMaterial
	var err error
	if reuseCA {
		caKeyPassword, err := expandCLIEnv("-ca-key-password", opts.CAKeyPassword)
		if err != nil {
			return err
		}
		ca, err = loadGMCA(caCertFile, caKeyFile, caKeyPassword)
		if err != nil {
			return err
		}
	} else {
		ca, err = newGMMaterial("yigate GM CA", 1, smx509.KeyUsageCertSign|smx509.KeyUsageCRLSign, true, domain, nil,
			time.Now().AddDate(0, 0, genCertDefaultCADays))
		if err != nil {
			return err
		}
	}

	// 叶子证书不能比签发它的 CA 活得久：CA 一到期整条链就失效，
	// 那样签出来的证书看似有效、实际不可用。
	capped := false
	if ca.cert.NotAfter.Before(leafNotAfter) {
		leafNotAfter = ca.cert.NotAfter
		capped = true
	}

	sign, err := newGMMaterial(domain, 2, smx509.KeyUsageDigitalSignature, false, domain, &ca, leafNotAfter)
	if err != nil {
		return err
	}
	enc, err := newGMMaterial(domain, 3, smx509.KeyUsageKeyEncipherment|smx509.KeyUsageDataEncipherment, false, domain, &ca, leafNotAfter)
	if err != nil {
		return err
	}

	keyPassword, err := expandCLIEnv("-p", opts.KeyPassword)
	if err != nil {
		return err
	}
	written := make([]gmCertFile, 0, 3)
	if !reuseCA {
		written = append(written, gmCertFile{"ca", ca})
	}
	written = append(written, gmCertFile{"sign", sign}, gmCertFile{"enc", enc})
	for _, item := range written {
		certPath := filepath.Join(outDir, item.name+".crt")
		keyPath := filepath.Join(outDir, item.name+".key")
		if err := os.WriteFile(certPath, item.m.certPEM, 0o644); err != nil { // #nosec G306 -- 证书是公开材料
			return err
		}
		keyPEM, err := marshalGMPrivateKey(item.m.key, []byte(keyPassword))
		if err != nil {
			return err
		}
		if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return err
		}
	}

	if reuseCA {
		fmt.Printf(`已使用现有 CA 签发国密 TLCP 证书：

  CA 证书        %s
  %s/sign.crt   TLCP 签名证书
  %s/enc.crt    TLCP 加密证书

监听端口配置示例：
tls:
  protocol: gm
  gm_sign_cert_file: "%s/sign.crt"
  gm_sign_key_file: "%s/sign.key"
  gm_enc_cert_file: "%s/enc.crt"
  gm_enc_key_file: "%s/enc.key"
`, caCertFile, outDir, outDir, outDir, outDir, outDir, outDir)
	} else {
		fmt.Printf(`已生成国密 TLCP 证书：

  %s/ca.crt     SM2 根证书
  %s/sign.crt   TLCP 签名证书
  %s/enc.crt    TLCP 加密证书

监听端口配置示例：
tls:
  protocol: gm
  gm_sign_cert_file: "%s/sign.crt"
  gm_sign_key_file: "%s/sign.key"
  gm_enc_cert_file: "%s/enc.crt"
  gm_enc_key_file: "%s/enc.key"
`, outDir, outDir, outDir, outDir, outDir, outDir, outDir)
	}
	if keyPassword != "" {
		fmt.Print(`  gm_sign_key_password: "${YIGATE_GM_SIGN_KEY_PASSWORD}"
  gm_enc_key_password: "${YIGATE_GM_ENC_KEY_PASSWORD}"

私钥已用 PBES2-SM4 加密，请通过环境变量提供口令。
`)
	}

	effectiveDays := int(math.Round(time.Until(leafNotAfter).Hours() / 24))
	if effectiveDays < 0 {
		effectiveDays = 0
	}
	fmt.Printf("\n证书到期时间：%s（约 %d 天）\n", leafNotAfter.Format("2006-01-02"), effectiveDays)
	if capped {
		fmt.Printf("注意：请求的 %d 天超过签发 CA 的有效期，已收敛到 CA 的到期时间。\n", days)
	}
	return nil
}

// loadGMCA 读取既有 SM2 CA 的证书与私钥，并校验两者确实配对。
// 私钥支持 PBES2-SM4 加密，口令可写 ${ENV} 引用环境变量。
func loadGMCA(certFile, keyFile, password string) (gmCertMaterial, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("read CA certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return gmCertMaterial{}, fmt.Errorf("%s does not contain a PEM certificate", certFile)
	}
	cert, err := smx509.ParseCertificate(block.Bytes)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("parse CA certificate: %w", err)
	}
	if !cert.IsCA {
		return gmCertMaterial{}, fmt.Errorf("%s is not a CA certificate", certFile)
	}
	// 用已过期的 CA 签发只会得到一条当场失效的链，不如直接报错。
	if !cert.NotAfter.After(time.Now()) {
		return gmCertMaterial{}, fmt.Errorf("CA certificate %s expired at %s", certFile, cert.NotAfter.Format(time.RFC3339))
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("read CA private key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return gmCertMaterial{}, fmt.Errorf("%s does not contain a PEM private key", keyFile)
	}
	key, err := pkcs8.ParsePKCS8PrivateKeySM2(keyBlock.Bytes, []byte(password))
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("parse CA private key (wrong password?): %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return gmCertMaterial{}, fmt.Errorf("CA certificate %s and private key %s do not match", certFile, keyFile)
	}
	return gmCertMaterial{certPEM: certPEM, keyPEM: keyPEM, cert: cert, key: key}, nil
}

func newGMMaterial(cn string, serial int64, usage smx509.KeyUsage, isCA bool, domain string, parent *gmCertMaterial, notAfter time.Time) (gmCertMaterial, error) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("generate SM2 key: %w", err)
	}
	randomSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return gmCertMaterial{}, err
	}
	tmpl := &smx509.Certificate{
		SerialNumber:          randomSerial.Add(randomSerial, big.NewInt(serial)),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"yigate demo"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              usage,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if !isCA {
		tmpl.DNSNames = []string{domain, "localhost"}
		tmpl.ExtKeyUsage = []smx509.ExtKeyUsage{smx509.ExtKeyUsageServerAuth, smx509.ExtKeyUsageClientAuth}
	}
	parentCert, parentKey := tmpl, key
	if parent != nil {
		parentCert, parentKey = parent.cert, parent.key
	}
	der, err := smx509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, parentKey)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("create certificate: %w", err)
	}
	cert, err := smx509.ParseCertificate(der)
	if err != nil {
		return gmCertMaterial{}, fmt.Errorf("parse certificate: %w", err)
	}
	return gmCertMaterial{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		cert:    cert,
		key:     key,
	}, nil
}

func marshalGMPrivateKey(key *sm2.PrivateKey, password []byte) ([]byte, error) {
	var (
		der []byte
		err error
	)
	if len(password) == 0 {
		der, err = smx509.MarshalPKCS8PrivateKey(key)
	} else {
		der, err = pkcs8.MarshalPrivateKey(key, password, pkcs.NewSMPBESEncrypter(16, 10000))
	}
	if err != nil {
		return nil, fmt.Errorf("marshal SM2 private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
