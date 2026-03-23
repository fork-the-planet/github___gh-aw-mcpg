package proxy

import (
	"encoding/json"
	"regexp"
	"strings"
)

// guardRequiredFields lists the GraphQL selection fields the DIFC guard needs
// for accurate integrity labeling. author{login} enables trusted-bot detection;
// authorAssociation provides the integrity level directly (MEMBER, CONTRIBUTOR,
// etc.) so the guard doesn't need extra enrichment REST round-trips.
var guardRequiredFields = []struct {
	field   string         // field text to inject
	present *regexp.Regexp // pattern that indicates the field is already selected
}{
	{"author{login}", regexp.MustCompile(`\bauthor\s*\{[^}]*\blogin\b`)},
	{"authorAssociation", regexp.MustCompile(`\bauthorAssociation\b`)},
}

// allGuardFieldsPresent returns true if the query already contains every
// required guard field.
func allGuardFieldsPresent(query string) bool {
	for _, f := range guardRequiredFields {
		if !f.present.MatchString(query) {
			return false
		}
	}
	return true
}

// missingGuardFields returns the field strings not yet present in the query.
func missingGuardFields(query string) []string {
	var missing []string
	for _, f := range guardRequiredFields {
		if !f.present.MatchString(query) {
			missing = append(missing, f.field)
		}
	}
	return missing
}

// InjectGuardFields rewrites a GraphQL request body to include fields
// required by the DIFC guard (e.g. author{login} for trusted-bot detection).
// Returns the (possibly modified) body. If injection is not needed or fails,
// the original body is returned unchanged.
func InjectGuardFields(body []byte, toolName string) []byte {
	// Only rewrite for tools that need author info
	switch toolName {
	case "list_issues", "list_pull_requests", "issue_read", "pull_request_read",
		"search_issues":
	default:
		return body
	}

	var gql GraphQLRequest
	if err := json.Unmarshal(body, &gql); err != nil {
		return body
	}

	if gql.Query == "" || allGuardFieldsPresent(gql.Query) {
		return body
	}

	missing := missingGuardFields(gql.Query)
	modified := injectFieldsIntoQuery(gql.Query, missing)
	if modified == gql.Query {
		return body
	}

	logGraphQL.Printf("injected %v into GraphQL query for %s", missing, toolName)

	gql.Query = modified
	out, err := json.Marshal(gql)
	if err != nil {
		return body
	}
	return out
}

// injectFieldsIntoQuery adds the given fields into the GraphQL query's node
// selection or fragment. Each field string (e.g. "author{login}",
// "authorAssociation") is comma-joined and injected as a single block.
func injectFieldsIntoQuery(query string, fields []string) string {
	injection := strings.Join(fields, ",")

	// Step 1: Check if the query uses a fragment spread in the nodes.
	// Pattern: nodes { ...fragmentName }
	fragmentInNodes := regexp.MustCompile(`nodes\s*\{\s*\.\.\.(\w+)`)
	if m := fragmentInNodes.FindStringSubmatch(query); m != nil {
		fragName := m[1]
		return injectIntoFragment(query, fragName, injection)
	}

	// Step 2: No fragment — inject directly into nodes { ... }
	nodesPattern := regexp.MustCompile(`(nodes\s*\{)`)
	if nodesPattern.MatchString(query) {
		return nodesPattern.ReplaceAllString(query, "${1}"+injection+",")
	}

	return query
}

// injectIntoFragment adds a field to the end of a named fragment definition.
// "fragment Name on Type { existing fields }" → "fragment Name on Type { existing fields field }"
func injectIntoFragment(query, fragName, field string) string {
	// Match: fragment <name> on <Type> { ... }
	// We need to find the closing brace of this specific fragment.
	fragPrefix := "fragment " + fragName + " on "
	idx := strings.Index(query, fragPrefix)
	if idx == -1 {
		return query
	}

	// Find the opening brace of the fragment body
	braceStart := strings.Index(query[idx:], "{")
	if braceStart == -1 {
		return query
	}
	braceStart += idx

	// Find the matching closing brace (handle nested braces)
	depth := 0
	braceEnd := -1
	for i := braceStart; i < len(query); i++ {
		if query[i] == '{' {
			depth++
		} else if query[i] == '}' {
			depth--
			if depth == 0 {
				braceEnd = i
				break
			}
		}
	}

	if braceEnd == -1 {
		return query
	}

	// Insert field before the closing brace
	return query[:braceEnd] + "," + field + query[braceEnd:]
}
