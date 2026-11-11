package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// certInspection 是 gencert -check 的单行输出。
type certInspection struct {
	File      string
	Subject   string
	IsCA      bool
	NotBefore time.Time
	NotAfter  time.Time
	DaysLeft  int
	Expired   bool
}

// inspectCertificates 扫描文件或目录（不递归）下的所有证书。
func inspectCertificates(path string, now time.Time) ([]certInspection, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".crt", ".pem", ".cer":
				files = append(files, filepath.Join(path, entry.Name()))
			}
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}

	var out []certInspection
	for _, file := range files {
		for _, cert := range parseCertificatesFile(file) {
			out = append(out, certInspection{
				File:      file,
				Subject:   cert.Subject.CommonName,
				IsCA:      cert.IsCA,
				NotBefore: cert.NotBefore,
				NotAfter:  cert.NotAfter,
				DaysLeft:  int(cert.NotAfter.Sub(now).Hours() / 24),
				Expired:   !cert.NotAfter.After(now),
			})
		}
	}
	return out, nil
}

// runCertCheck 打印证书体检结果，返回进程退出码。
// 有证书已过期或落在告警窗口内时返回 1，便于在发布脚本里直接拦截。
func runCertCheck(path string) int {
	now := time.Now()
	items, err := inspectCertificates(path, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gencert -check: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Fprintf(os.Stderr, "gencert -check: no certificate found in %s\n", path)
		return 1
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FILE\tKIND\tSUBJECT\tNOT BEFORE\tNOT AFTER\tREMAINING\tSTATUS")
	urgent := false
	for _, item := range items {
		kind := "leaf"
		if item.IsCA {
			kind = "ca"
		}
		status := "ok"
		switch {
		case item.Expired:
			status = "EXPIRED"
			urgent = true
		case item.DaysLeft < int(certExpiryWarnWindow.Hours()/24):
			status = "expiring"
			urgent = true
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d days\t%s\n",
			item.File, kind, item.Subject,
			item.NotBefore.Format("2006-01-02"), item.NotAfter.Format("2006-01-02"),
			item.DaysLeft, status)
	}
	_ = w.Flush()

	if urgent {
		return 1
	}
	return 0
}
