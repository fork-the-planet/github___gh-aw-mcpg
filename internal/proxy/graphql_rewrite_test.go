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

func TestInjectGuardFields_InjectsIntoInlineFragment(t *testing.T) {
	// Search queries use union types with inline fragments; fields must go
	// inside the inline fragment, not directly on the nodes level.
	query := `query { search(query:"repo:o/r is:issue", type:ISSUE, first:3) { issueCount nodes { ... on Issue { number title } } } }`
	body, _ := json.Marshal(GraphQLRequest{Query: query})

	result := InjectGuardFields(body, "search_issues")

	var gql GraphQLRequest
	require.NoError(t, json.Unmarshal(result, &gql))
	assert.Contains(t, gql.Query, "author{login}")
	assert.Contains(t, gql.Query, "authorAssociation")
	// Fields must be inside the inline fragment, not before it
	assert.Contains(t, gql.Query, "... on Issue {author{login},authorAssociation,")
	assert.Contains(t, gql.Query, "number")
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

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			count++
		}
	}
	return count
}
