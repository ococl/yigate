package proxy

import "testing"

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
