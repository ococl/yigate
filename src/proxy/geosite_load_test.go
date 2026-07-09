package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func buildMinimalGeoSiteDat(t *testing.T) string {
	t.Helper()
	// Protobuf varint helper.
	varint := func(v uint64) []byte {
		var buf []byte
		for v >= 0x80 {
			buf = append(buf, byte(v&0x7f|0x80))
			v >>= 7
		}
		buf = append(buf, byte(v))
		return buf
	}
	field := func(num, wire uint64, data []byte) []byte {
		key := varint((num << 3) | wire)
		if wire == 2 {
			return append(append(key, varint(uint64(len(data)))...), data...)
		}
		return append(key, data...)
	}

	// geoDomainPB: type=2 (Domain), value=example.com
	domainPB := append(field(1, 0, varint(2)), field(2, 2, []byte("example.com"))...)
	// geoSitePB: country_code=CN, domains=[domainPB]
	sitePB := append(field(1, 2, []byte("CN")), field(2, 2, domainPB)...)
	// geoSiteListPB: entries=[sitePB]
	listPB := field(1, 2, sitePB)

	path := filepath.Join(t.TempDir(), "geosite.dat")
	if err := writeFile(path, listPB); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	return path
}

func writeFile(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func TestLoadGeoSiteDatAndMatch(t *testing.T) {
	path := buildMinimalGeoSiteDat(t)
	m, err := LoadGeoSiteDat(path, []string{"CN"})
	if err != nil {
		t.Fatalf("LoadGeoSiteDat: %v", err)
	}
	if !m.Match("CN", "sub.example.com") {
		t.Fatal("expected CN suffix match")
	}
	if m.Match("CN", "unknown.com") {
		t.Fatal("expected no match")
	}
	if m.Match("US", "example.com") {
		t.Fatal("expected no match for US code")
	}
}

func TestLoadGeoSiteDatCodeNotFound(t *testing.T) {
	path := buildMinimalGeoSiteDat(t)
	_, err := LoadGeoSiteDat(path, []string{"US"})
	if err == nil {
		t.Fatal("expected error for missing code")
	}
}

func TestLoadGeoSiteDatAllCodes(t *testing.T) {
	path := buildMinimalGeoSiteDat(t)
	m, err := LoadGeoSiteDat(path, nil)
	if err != nil {
		t.Fatalf("LoadGeoSiteDat: %v", err)
	}
	if !m.Match("CN", "example.com") {
		t.Fatal("expected match when loading all codes")
	}
}

func TestGeoSiteMatcherNil(t *testing.T) {
	var m *GeoSiteMatcher
	if m.Match("CN", "example.com") {
		t.Fatal("nil matcher should not match")
	}
}
