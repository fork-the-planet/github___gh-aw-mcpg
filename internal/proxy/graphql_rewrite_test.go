package proxy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectGuardFields_SkipsIrrelevantTools(t *testing.T) {
	body := []byte(`{"query":"{ viewer { login } }"}`)
	result := InjectGuardFields(body, "get_me")
	assert.Equal(t, body, result)
}

func TestInjectGuardFields_SkipsWhenFieldsPresent(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { pullRequests(first:10) { nodes { number author{login} authorAssociation } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})
	result := InjectGuardFields(body, "list_pull_requests")
	assert.Equal(t, body, result)
}

func TestInjectGuardFields_SkipsWhenCommitFieldsPresent(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { defaultBranchRef { target { ... on Commit { history(first:10) { nodes { oid author{user{login}} } } } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})
	result := InjectGuardFields(body, "list_commits")
	assert.Equal(t, body, result)
}

func TestInjectGuardFields_InjectsIntoNodes(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { pullRequests(first:10) { nodes { number title } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_pull_requests")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
	// Original fields still present
	assert.Contains(t, gql.Query, "number")
	assert.Contains(t, gql.Query, "title")
}

func TestInjectGuardFields_InjectsIntoFragment(t *testing.T) {
	query := `fragment pr on PullRequest{number,title}
query { repository(owner:"o", name:"r") { pullRequests(first:10) { nodes { ...pr } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_pull_requests")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
	// Fragment still intact
	assert.Contains(t, gql.Query, "fragment pr on PullRequest")
	assert.Contains(t, gql.Query, "number")
}

func TestInjectGuardFields_InjectsOnlyMissing(t *testing.T) {
	// Has author{login} but not authorAssociation
	query := `query { repository(owner:"o", name:"r") { issues(first:10) { nodes { number author{login} } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_issues")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "authorAssociation")
	// Should not double-inject author{login}
	assert.Equal(t, 1, countOccurrences(gql.Query, "author{login}"))
}

func TestInjectGuardFields_HandlesIssues(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { issues(first:10) { nodes { number labels } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_issues")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
}

func TestInjectGuardFields_PreservesVariables(t *testing.T) {
	query := `query($owner:String!,$repo:String!) { repository(owner:$owner, name:$repo) { pullRequests(first:10) { nodes { number } } } }`
	vars := map[string]interface{}{"owner": "github", "repo": "gh-aw-mcpg"}
	body, _ := json.Marshal(GraphQLRequest{Query: query, Variables: vars})

	result := InjectGuardFields(body, "list_pull_requests")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Equal(t, "github", gql.Variables["owner"])
	assert.Equal(t, "gh-aw-mcpg", gql.Variables["repo"])
}

func TestInjectGuardFields_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	result := InjectGuardFields(body, "list_pull_requests")
	assert.Equal(t, body, result)
}

func TestInjectGuardFields_RealGhCliQuery(t *testing.T) {
	// Actual query from `gh pr list --json number,title`
	query := `fragment pr on PullRequest{number,title}
    query PullRequestList(
      $owner: String!,
      $repo: String!,
      $limit: Int!,
      $endCursor: String,
      $state: [PullRequestState!] = OPEN
    ) {
      repository(owner: $owner, name: $repo) {
        pullRequests(
          states: $state,
          first: $limit,
          after: $endCursor,
          orderBy: {field: CREATED_AT, direction: DESC}
        ) {
          totalCount
          nodes {
            ...pr
          }
          pageInfo {
            hasNextPage
            endCursor
          }
        }
      }
    }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_pull_requests")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
	// Injected into fragment, not nodes
	assert.Contains(t, gql.Query, "fragment pr on PullRequest{number,title,author{login},authorAssociation}")
}

func TestInjectIntoFragment_NestedBraces(t *testing.T) {
	query := `fragment pr on PullRequest{number,labels{nodes{name}}}
query { repository(owner:"o",name:"r") { pullRequests(first:1) { nodes { ...pr } } } }`

	result := injectIntoFragment(query, "pr", "author{login},authorAssociation")
	assert.Contains(t, result, "labels{nodes{name}}")
	assert.Contains(t, result, "author{login},authorAssociation}")
}

func TestInjectGuardFields_CommitInjectsAuthorUser(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { defaultBranchRef { target { ... on Commit { history(first:10) { nodes { oid messageHeadline } } } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_commits")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{user{login}}")
	// Should NOT inject issue/PR fields
	assert.NotContains(t, gql.Query, "authorAssociation")
	// Original fields still present
	assert.Contains(t, gql.Query, "oid")
	assert.Contains(t, gql.Query, "messageHeadline")
}

func TestInjectGuardFields_CommitFragment(t *testing.T) {
	query := `fragment c on Commit{oid,messageHeadline}
query { repository(owner:"o", name:"r") { defaultBranchRef { target { ... on Commit { history(first:10) { nodes { ...c } } } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_commits")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{user{login}}")
	assert.Contains(t, gql.Query, "fragment c on Commit{oid,messageHeadline,author{user{login}}}")
}

func TestInjectGuardFields_CommitSkipsWhenAuthorUserPresent(t *testing.T) {
	// Has author{user{login}} already — should be a no-op
	query := `query { repository(owner:"o", name:"r") { defaultBranchRef { target { ... on Commit { history(first:10) { nodes { oid author { user { login } } } } } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})
	result := InjectGuardFields(body, "list_commits")
	assert.Equal(t, body, result)
}

func TestInjectGuardFields_SearchIssues(t *testing.T) {
	query := `query { search(query: "is:issue repo:o/r", type: ISSUE, first: 10) { nodes { number title } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "search_issues")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
	assert.Contains(t, gql.Query, "number")
	assert.Contains(t, gql.Query, "title")
}

func TestInjectGuardFields_IssueRead(t *testing.T) {
	query := `query { repository(owner:"o", name:"r") { issue(number: 1) { number title comments(first:10) { nodes { body } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "issue_read")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
}

func TestInjectGuardFields_PullRequestRead(t *testing.T) {
	// Use a query with a nodes block (e.g., reviews sub-collection) so injection can occur.
	query := `query { repository(owner:"o", name:"r") { pullRequest(number: 1) { reviews(first:5) { nodes { body } } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "pull_request_read")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
}

func TestInjectGuardFields_NoNodesNoFragment(t *testing.T) {
	// A query with required tool but no "nodes" block and no fragment spread —
	// the injector cannot find a place to insert fields, so body is returned unchanged.
	query := `query { repository(owner:"o", name:"r") { pullRequest(number: 1) { title } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "list_pull_requests")

	assert.Equal(t, body, result)
}

func TestInjectGuardFields_EmptyQuery(t *testing.T) {
	body, _ := json.Marshal(GraphQLRequest{Query: ""})
	result := InjectGuardFields(body, "list_pull_requests")
	assert.Equal(t, body, result)
}

func TestFieldsForTool(t *testing.T) {
	tests := []struct {
		toolName      string
		wantNil       bool
		expectedField string
	}{
		{toolName: "list_issues", wantNil: false, expectedField: "author{login}"},
		{toolName: "list_pull_requests", wantNil: false, expectedField: "author{login}"},
		{toolName: "issue_read", wantNil: false, expectedField: "author{login}"},
		{toolName: "pull_request_read", wantNil: false, expectedField: "author{login}"},
		{toolName: "search_issues", wantNil: false, expectedField: "authorAssociation"},
		{toolName: "list_commits", wantNil: false, expectedField: "author{user{login}}"},
		{toolName: "get_me", wantNil: true},
		{toolName: "get_file_contents", wantNil: true},
		{toolName: "", wantNil: true},
		{toolName: "unknown_tool", wantNil: true},
	}

	for _, tt := range tests {
		t.Run("tool: "+tt.toolName, func(t *testing.T) {
			fields := fieldsForTool(tt.toolName)
			if tt.wantNil {
				assert.Nil(t, fields, "expected nil fields for tool %q", tt.toolName)
			} else {
				require.NotNil(t, fields, "expected non-nil fields for tool %q", tt.toolName)
				fieldStrings := make([]string, len(fields))
				for i, f := range fields {
					fieldStrings[i] = f.field
				}
				assert.Contains(t, fieldStrings, tt.expectedField)
			}
		})
	}
}

func TestAllFieldsPresent(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		fields []guardFieldSet
		want   bool
	}{
		{
			name:   "all fields present",
			query:  `{ nodes { author { login } authorAssociation } }`,
			fields: issueAndPRFields,
			want:   true,
		},
		{
			name:   "author login missing",
			query:  `{ nodes { authorAssociation } }`,
			fields: issueAndPRFields,
			want:   false,
		},
		{
			name:   "authorAssociation missing",
			query:  `{ nodes { author { login } } }`,
			fields: issueAndPRFields,
			want:   false,
		},
		{
			name:   "both missing",
			query:  `{ nodes { title } }`,
			fields: issueAndPRFields,
			want:   false,
		},
		{
			name:   "empty fields set is always true",
			query:  `{ nodes { title } }`,
			fields: []guardFieldSet{},
			want:   true,
		},
		{
			name:   "commit fields present",
			query:  `{ nodes { author { user { login } } } }`,
			fields: commitFields,
			want:   true,
		},
		{
			name:   "commit fields missing",
			query:  `{ nodes { oid } }`,
			fields: commitFields,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allFieldsPresent(tt.query, tt.fields)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMissingFields(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		fields []guardFieldSet
		want   []string
	}{
		{
			name:   "both fields missing",
			query:  `{ nodes { title } }`,
			fields: issueAndPRFields,
			want:   []string{"author{login}", "authorAssociation"},
		},
		{
			name:   "only authorAssociation missing",
			query:  `{ nodes { author { login } title } }`,
			fields: issueAndPRFields,
			want:   []string{"authorAssociation"},
		},
		{
			name:   "nothing missing",
			query:  `{ nodes { author { login } authorAssociation } }`,
			fields: issueAndPRFields,
			want:   nil,
		},
		{
			name:   "empty fields set returns nil",
			query:  `{ nodes { title } }`,
			fields: []guardFieldSet{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingFields(tt.query, tt.fields)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInjectIntoFragment_FragmentNotFound(t *testing.T) {
	query := `fragment other on Issue { title } query { repository(owner:"o",name:"r") { pullRequests(first:1) { nodes { ...other } } } }`

	// Inject into fragment "nonexistent" — should return query unchanged.
	result := injectIntoFragment(query, "nonexistent", "author{login}")
	assert.Equal(t, query, result)
}

func TestInjectIntoFragment_NoOpeningBrace(t *testing.T) {
	// Malformed: "fragment pr on PullRequest" with no brace
	query := `fragment pr on PullRequest`
	result := injectIntoFragment(query, "pr", "author{login}")
	assert.Equal(t, query, result)
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			count++
		}
	}
	return count
}
