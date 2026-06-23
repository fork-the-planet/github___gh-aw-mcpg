package urlutil

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// urlPattern is intentionally permissive; each match is validated with url.Parse
// before hostname extraction so malformed or punctuated candidates are discarded.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// ExtractURLDomainsFromValue recursively extracts unique URL hostnames from string leaves.
func ExtractURLDomainsFromValue(value any) []string {
	domainSet := make(map[string]struct{})
	collectURLDomains(value, domainSet)
	if len(domainSet) == 0 {
		return nil
	}

	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func collectURLDomains(value any, domains map[string]struct{}) {
	switch v := value.(type) {
	case string:
		for _, domain := range ExtractURLDomains(v) {
			domains[domain] = struct{}{}
		}
	case map[string]any:
		for _, child := range v {
			collectURLDomains(child, domains)
		}
	case []any:
		for _, child := range v {
			collectURLDomains(child, domains)
		}
	case []map[string]any:
		for _, child := range v {
			collectURLDomains(child, domains)
		}
	}
}

// ExtractURLDomains extracts unique URL hostnames from a string.
func ExtractURLDomains(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	domainSet := make(map[string]struct{})
	for _, match := range matches {
		parsed, err := url.Parse(match)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "" {
			continue
		}
		domainSet[host] = struct{}{}
	}

	if len(domainSet) == 0 {
		return nil
	}
	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}
