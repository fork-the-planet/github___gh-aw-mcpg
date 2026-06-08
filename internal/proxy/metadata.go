package proxy

var metadataPassthrough = map[string]bool{
	"/meta":       true,
	"/rate_limit": true,
	"/octocat":    true,
	"/zen":        true,
	"/versions":   true,
}

func isMetadataPassthroughPath(path string) bool {
	return metadataPassthrough[path]
}
