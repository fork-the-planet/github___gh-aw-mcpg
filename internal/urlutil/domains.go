package urlutil

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logDomains = logger.New("urlutil:domains")

// urlPattern requires a non-empty hostname candidate and then captures the rest
// of the URL until common delimiter characters. The (?i) flag makes the scheme
// match case-insensitive (e.g. "HTTPS://"). Matches are still validated with
// url.Parse before hostname extraction.
var urlPattern = regexp.MustCompile(`(?i)https?://[^\s/"'<>]+[^\s"'<>]*`)

// ExtractURLDomainsFromValue recursively extracts unique URL hostnames from string leaves.
func ExtractURLDomainsFromValue(value any) []string {
	domainSet := make(map[string]struct{})
	collectURLDomains(value, domainSet)
	if len(domainSet) == 0 {
		logDomains.Print("ExtractURLDomainsFromValue: no domains found in value tree")
		return nil
	}

	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	logDomains.Printf("ExtractURLDomainsFromValue: extracted %d unique domain(s)", len(domains))
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
	logDomains.Printf("ExtractURLDomains: found %d URL candidate(s) in text", len(matches))

	domainSet := make(map[string]struct{})
	for _, match := range matches {
		// Strip trailing punctuation that may appear when a URL is embedded in
		// prose (e.g. "https://example.com," or "https://example.com)"). These
		// characters are valid inside a URL so the regex cannot exclude them
		// blindly; trimming them from the tail of each candidate is the safest
		// heuristic.
		match = strings.TrimRight(match, ".,;:!?)]}\"'")
		parsed, err := url.Parse(match)
		if err != nil {
			logDomains.Printf("ExtractURLDomains: skipping unparseable URL candidate: %v", err)
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
	logDomains.Printf("ExtractURLDomains: resolved %d unique domain(s) from %d candidate(s)", len(domains), len(matches))
	return domains
}
