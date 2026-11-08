package proxy

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGeoSiteUpdateURL(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"ftp://host/dlc.dat",
		"http://127.0.0.1:8890/api/v1/geosite/dat",
		"http://localhost:8890/api/v1/geosite/dat",
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/dlc.dat",
		"https://",
	} {
		if err := ValidateGeoSiteUpdateURL(raw); err == nil {
			t.Fatalf("%q should be rejected", raw)
		}
	}
	for _, raw := range []string{
		"https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
		"http://node-a:8892/api/v1/geosite/dat",
		"https://${GEOSITE_HOST}/dlc.dat",
		"",
	} {
		if err := ValidateGeoSiteUpdateURL(raw); err != nil {
			t.Fatalf("%q should be accepted: %v", raw, err)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("https://user:secret@host.example/dlc.dat?token=abc#frag")
	if strings.Contains(got, "user") || strings.Contains(got, "secret") || strings.Contains(got, "token") {
		t.Fatalf("credentials leaked in redacted url: %q", got)
	}
	if got != "https://host.example/dlc.dat" {
		t.Fatalf("redacted url = %q", got)
	}
}

func TestValidateGeoSiteDataPath(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	for _, candidate := range []string{
		"/etc/passwd",
		"../../../../etc/passwd",
		"/proc/self/environ",
	} {
		if err := ValidateGeoSiteDataPath(cfgPath, candidate); err == nil {
			t.Fatalf("geosite path %q should be rejected", candidate)
		}
	}
	for _, candidate := range []string{
		"../data/geosite.dat",
		"geosite.dat",
		"",
		"${GEOSITE_PATH}",
	} {
		if err := ValidateGeoSiteDataPath(cfgPath, candidate); err != nil {
			t.Fatalf("geosite path %q should be accepted: %v", candidate, err)
		}
	}
}
