package proxy

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// GeoSiteMatcher loads v2ray/xray geosite.dat and matches domains by code.
type GeoSiteMatcher struct {
	sets map[string]*DomainSet
}

type DomainSet struct {
	full    map[string]struct{}
	suffix  map[string]struct{}
	keyword []string
	regex   []*regexp.Regexp
}

func LoadGeoSiteDat(path string, codes []string) (*GeoSiteMatcher, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read geosite dat %q: %w", path, err)
	}
	list, err := parseGeoSiteList(data)
	if err != nil {
		return nil, fmt.Errorf("parse geosite dat %q: %w", path, err)
	}

	wanted := make(map[string]struct{})
	for _, code := range codes {
		code = normalizeGeoSiteCode(code)
		if code != "" {
			wanted[code] = struct{}{}
		}
	}

	matcher := &GeoSiteMatcher{sets: make(map[string]*DomainSet)}
	for _, site := range list.Entries {
		code := strings.ToUpper(site.CountryCode)
		if len(wanted) > 0 {
			if _, ok := wanted[code]; !ok {
				continue
			}
		}
		set := newDomainSet()
		for _, domain := range site.Domains {
			set.add(domain.Type, domain.Value)
		}
		matcher.sets[code] = set
	}
	for code := range wanted {
		if _, ok := matcher.sets[code]; !ok {
			return nil, fmt.Errorf("geosite code %q not found in %s", code, path)
		}
	}
	return matcher, nil
}

func (m *GeoSiteMatcher) Match(code, host string) bool {
	if m == nil {
		return false
	}
	set := m.sets[normalizeGeoSiteCode(code)]
	if set == nil {
		return false
	}
	return set.Match(host)
}

func newDomainSet() *DomainSet {
	return &DomainSet{
		full:   make(map[string]struct{}),
		suffix: make(map[string]struct{}),
	}
}

func (s *DomainSet) add(typ int, value string) {
	value = normalizeDomainRuleValue(value)
	if value == "" {
		return
	}
	switch typ {
	case 0: // Plain
		s.keyword = append(s.keyword, value)
	case 1: // Regex
		re, err := regexp.Compile(value)
		if err == nil {
			s.regex = append(s.regex, re)
		}
	case 2: // Domain
		s.suffix[value] = struct{}{}
	case 3: // Full
		s.full[value] = struct{}{}
	default:
		s.suffix[value] = struct{}{}
	}
}

func (s *DomainSet) Match(host string) bool {
	host = normalizeDomainRuleValue(host)
	if host == "" {
		return false
	}
	if _, ok := s.full[host]; ok {
		return true
	}
	if s.matchSuffix(host) {
		return true
	}
	for _, keyword := range s.keyword {
		if strings.Contains(host, keyword) {
			return true
		}
	}
	for _, re := range s.regex {
		if re.MatchString(host) {
			return true
		}
	}
	return false
}

func (s *DomainSet) matchSuffix(host string) bool {
	for {
		if _, ok := s.suffix[host]; ok {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			return false
		}
		host = host[i+1:]
	}
}

type geoSiteListPB struct {
	Entries []geoSitePB
}

type geoSitePB struct {
	CountryCode string
	Domains     []geoDomainPB
}

type geoDomainPB struct {
	Type  int
	Value string
}

func parseGeoSiteList(data []byte) (*geoSiteListPB, error) {
	list := &geoSiteListPB{}
	for len(data) > 0 {
		field, wire, n, err := readProtoKey(data)
		if err != nil {
			return nil, err
		}
		data = data[n:]
		if field != 1 || wire != 2 {
			var skip int
			skip, err = skipProtoValue(data, wire)
			if err != nil {
				return nil, err
			}
			data = data[skip:]
			continue
		}
		payload, consumed, err := readLengthDelimited(data)
		if err != nil {
			return nil, err
		}
		data = data[consumed:]
		site, err := parseGeoSite(payload)
		if err != nil {
			return nil, err
		}
		list.Entries = append(list.Entries, site)
	}
	return list, nil
}

func parseGeoSite(data []byte) (geoSitePB, error) {
	var site geoSitePB
	for len(data) > 0 {
		field, wire, n, err := readProtoKey(data)
		if err != nil {
			return site, err
		}
		data = data[n:]
		switch field {
		case 1:
			if wire != 2 {
				return site, fmt.Errorf("invalid geosite country_code wire %d", wire)
			}
			payload, consumed, err := readLengthDelimited(data)
			if err != nil {
				return site, err
			}
			site.CountryCode = string(payload)
			data = data[consumed:]
		case 2:
			if wire != 2 {
				return site, fmt.Errorf("invalid geosite domain wire %d", wire)
			}
			payload, consumed, err := readLengthDelimited(data)
			if err != nil {
				return site, err
			}
			domain, err := parseGeoDomain(payload)
			if err != nil {
				return site, err
			}
			site.Domains = append(site.Domains, domain)
			data = data[consumed:]
		default:
			skip, err := skipProtoValue(data, wire)
			if err != nil {
				return site, err
			}
			data = data[skip:]
		}
	}
	return site, nil
}

func parseGeoDomain(data []byte) (geoDomainPB, error) {
	domain := geoDomainPB{Type: 0}
	for len(data) > 0 {
		field, wire, n, err := readProtoKey(data)
		if err != nil {
			return domain, err
		}
		data = data[n:]
		switch field {
		case 1:
			if wire != 0 {
				return domain, fmt.Errorf("invalid domain type wire %d", wire)
			}
			v, consumed, err := readVarint(data)
			if err != nil {
				return domain, err
			}
			domain.Type = int(v)
			data = data[consumed:]
		case 2:
			if wire != 2 {
				return domain, fmt.Errorf("invalid domain value wire %d", wire)
			}
			payload, consumed, err := readLengthDelimited(data)
			if err != nil {
				return domain, err
			}
			domain.Value = string(payload)
			data = data[consumed:]
		default:
			skip, err := skipProtoValue(data, wire)
			if err != nil {
				return domain, err
			}
			data = data[skip:]
		}
	}
	return domain, nil
}

func readProtoKey(data []byte) (field int, wire int, consumed int, err error) {
	v, n, err := readVarint(data)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(v >> 3), int(v & 0x7), n, nil
}

func readLengthDelimited(data []byte) ([]byte, int, error) {
	length, n, err := readVarint(data)
	if err != nil {
		return nil, 0, err
	}
	if uint64(len(data)-n) < length {
		return nil, 0, io.ErrUnexpectedEOF
	}
	start := n
	end := n + int(length)
	return data[start:end], end, nil
}

func readVarint(data []byte) (uint64, int, error) {
	var value uint64
	for i := 0; i < len(data) && i < 10; i++ {
		b := data[i]
		value |= uint64(b&0x7f) << uint(7*i)
		if b < 0x80 {
			return value, i + 1, nil
		}
	}
	if len(data) < 10 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	return 0, 0, fmt.Errorf("invalid varint")
}

func skipProtoValue(data []byte, wire int) (int, error) {
	switch wire {
	case 0:
		_, n, err := readVarint(data)
		return n, err
	case 1:
		if len(data) < 8 {
			return 0, io.ErrUnexpectedEOF
		}
		return 8, nil
	case 2:
		_, n, err := readLengthDelimited(data)
		return n, err
	case 5:
		if len(data) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported protobuf wire type %d", wire)
	}
}
