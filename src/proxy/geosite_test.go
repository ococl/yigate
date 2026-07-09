package proxy

import "testing"

func TestDomainSetMatch(t *testing.T) {
	s := newDomainSet()
	s.add(3, "example.com") // Full
	s.add(2, "example.org") // Domain
	s.add(0, "keyword")     // Plain
	s.add(1, `^.*\.test$`)  // Regex

	if !s.Match("example.com") {
		t.Fatal("expected full match")
	}
	if !s.Match("sub.example.org") {
		t.Fatal("expected suffix match")
	}
	if !s.Match("keyworddomain.com") {
		t.Fatal("expected keyword match")
	}
	if !s.Match("site.test") {
		t.Fatal("expected regex match")
	}
	if s.Match("unknown.com") {
		t.Fatal("expected no match")
	}
	if s.Match("") {
		t.Fatal("expected empty host no match")
	}
}

func TestDomainSetAddEmpty(t *testing.T) {
	s := newDomainSet()
	s.add(2, "")
	if s.Match("anything.com") {
		t.Fatal("expected no match after adding empty value")
	}
}

func TestMatchSuffix(t *testing.T) {
	s := newDomainSet()
	s.add(2, "example.com")
	if !s.matchSuffix("example.com") {
		t.Fatal("expected exact suffix match")
	}
	if !s.matchSuffix("sub.example.com") {
		t.Fatal("expected subdomain suffix match")
	}
	if s.matchSuffix("notexample.com") {
		t.Fatal("expected no suffix match")
	}
}

func TestLoadGeoSiteDatMissing(t *testing.T) {
	if _, err := LoadGeoSiteDat("/nonexistent/geosite.dat", []string{"CN"}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseVarint(t *testing.T) {
	v, n, err := readVarint([]byte{0x01})
	if err != nil || v != 1 || n != 1 {
		t.Fatalf("readVarint = %d, %d, %v", v, n, err)
	}
	_, _, err = readVarint([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80})
	if err == nil {
		t.Fatal("expected invalid varint")
	}
	_, _, err = readVarint([]byte{0x80})
	if err == nil {
		t.Fatal("expected EOF")
	}
}

func TestSkipProtoValue(t *testing.T) {
	if n, err := skipProtoValue([]byte{0x01}, 0); err != nil || n != 1 {
		t.Fatalf("skip varint = %d, %v", n, err)
	}
	if n, err := skipProtoValue([]byte{0, 0, 0, 0, 0, 0, 0, 0}, 1); err != nil || n != 8 {
		t.Fatalf("skip 64bit = %d, %v", n, err)
	}
	if n, err := skipProtoValue([]byte{0x02, 0x01, 0x00}, 2); err != nil || n != 3 {
		t.Fatalf("skip length = %d, %v", n, err)
	}
	if n, err := skipProtoValue([]byte{0, 0, 0, 0}, 5); err != nil || n != 4 {
		t.Fatalf("skip 32bit = %d, %v", n, err)
	}
	if _, err := skipProtoValue([]byte{}, 5); err == nil {
		t.Fatal("expected EOF")
	}
	if _, err := skipProtoValue([]byte{}, 9); err == nil {
		t.Fatal("expected unsupported")
	}
}

func TestGeoSiteCodeNormalization(t *testing.T) {
	codes := geositeCodes([]RouteRule{
		{Type: "geosite", Values: []string{"GEOSITE:CN", "google", " cn "}},
		{Type: "domain_suffix", Values: []string{"example.com"}},
	})
	want := []string{"CN", "GOOGLE"}
	if len(codes) != len(want) {
		t.Fatalf("codes = %#v, want %#v", codes, want)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %#v, want %#v", codes, want)
		}
	}
}
