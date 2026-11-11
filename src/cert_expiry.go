package main

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emmansun/gmsm/smx509"

	"yigate/proxy"
)

// 证书到期检查只是可观测性手段：证书短周期轮换（gencert -days）之后，
// 没有到期提醒就等于把续期交给运气。检查失败一律不阻断启动/热加载，
// 因为真正的可用性问题会在建监听时以更明确的错误暴露。
const certExpiryWarnWindow = 30 * 24 * time.Hour

type certRef struct {
	kind string
	path string
}

type certExpiryFinding struct {
	Kind     string
	Path     string
	Subject  string
	IsCA     bool
	NotAfter time.Time
	Expired  bool
}

// collectCertificateRefs 汇总配置中引用的所有证书文件。
func collectCertificateRefs(cfg *Config) []certRef {
	if cfg == nil {
		return nil
	}
	var refs []certRef
	addTLS := func(t TLSConfig) {
		refs = append(refs,
			certRef{"listener certificate", t.CertFile},
			certRef{"TLCP signing certificate", t.GMSignCertFile},
			certRef{"TLCP encryption certificate", t.GMEncCertFile},
			certRef{"client CA", t.ClientCAFile},
		)
	}
	addTLS(cfg.TLS)
	for _, lc := range cfg.Listeners {
		addTLS(lc.TLS)
	}
	for _, up := range cfg.Upstreams {
		refs = append(refs, certRef{"upstream CA", up.TLSRootCAFile})
	}

	// 同一个文件常被多个监听共用（如共享 client_ca_file），只检查一次。
	seen := make(map[string]struct{}, len(refs))
	out := refs[:0]
	for _, ref := range refs {
		path := strings.TrimSpace(ref.path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		ref.path = path
		out = append(out, ref)
	}
	return out
}

// collectCertificates 返回配置中引用的全部证书，按到期时间升序。
func collectCertificates(cfg *Config, baseDir string, now time.Time) []certExpiryFinding {
	var findings []certExpiryFinding

	for _, ref := range collectCertificateRefs(cfg) {
		path := ref.path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		for _, cert := range parseCertificatesFile(path) {
			findings = append(findings, certExpiryFinding{
				Kind:     ref.kind,
				Path:     path,
				Subject:  cert.Subject.CommonName,
				IsCA:     cert.IsCA,
				NotAfter: cert.NotAfter,
				Expired:  !cert.NotAfter.After(now),
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].NotAfter.Before(findings[j].NotAfter) })
	return findings
}

// collectExpiringCertificates 只保留窗口内到期或已过期的证书。
func collectExpiringCertificates(cfg *Config, baseDir string, now time.Time) []certExpiryFinding {
	var findings []certExpiryFinding
	for _, f := range collectCertificates(cfg, baseDir, now) {
		if f.Expired || !f.NotAfter.After(now.Add(certExpiryWarnWindow)) {
			findings = append(findings, f)
		}
	}
	return findings
}

// parseCertificatesFile 解析 PEM 文件里的全部证书。
// 用 smx509 而非 crypto/x509：后者解析不了 SM2 证书，而这里的文件
// 既可能是标准 TLS 证书，也可能是国密双证书或混合 CA 包。
func parseCertificatesFile(path string) []*smx509.Certificate {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var certs []*smx509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := smx509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certs = append(certs, cert)
	}
	return certs
}

// 证书文件列举的上限：配置目录通常只有几十个文件，设上限是为了防止
// 有人把进程工作目录误配成配置目录时，把整棵树扫一遍并回传给 WebUI。
const (
	certFileScanDepth = 4
	certFileScanLimit = 500
)

// listCertificateFiles 列出配置目录内可作为 tls_ca_file 的证书文件（相对路径）。
// 只回传文件名，不读取内容；不跟随符号链接目录，避免被指向配置目录之外。
func listCertificateFiles(baseDir string) ([]string, error) {
	root, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, 16)
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > certFileScanDepth || len(files) >= certFileScanLimit {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			// 子目录不可读不影响其它目录的列举。
			if dir == root {
				return err
			}
			return nil
		}
		for _, entry := range entries {
			if len(files) >= certFileScanLimit {
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(dir, name)
			if entry.IsDir() {
				if entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				if err := walk(path, depth+1); err != nil {
					return err
				}
				continue
			}
			switch strings.ToLower(filepath.Ext(name)) {
			case ".crt", ".cer", ".pem":
			default:
				continue
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				continue
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}
	if err := walk(root, 0); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// warnExpiringCertificates 在启动与每次热加载后输出到期告警。
func warnExpiringCertificates(cfg *Config, baseDir string) {
	now := time.Now()
	for _, f := range collectExpiringCertificates(cfg, baseDir, now) {
		daysLeft := int(f.NotAfter.Sub(now).Hours() / 24)
		if f.Expired {
			proxy.Logger().Errorw("certificate has expired",
				"file", f.Path, "kind", f.Kind, "subject", f.Subject,
				"expired_at", f.NotAfter.Format(time.RFC3339))
			continue
		}
		proxy.Logger().Warnw("certificate expires soon",
			"file", f.Path, "kind", f.Kind, "subject", f.Subject,
			"days_left", daysLeft, "not_after", f.NotAfter.Format(time.RFC3339))
	}
}
