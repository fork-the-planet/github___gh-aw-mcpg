package dockerutil

import (
	"fmt"
	"os"
	"strings"
)

// ExpandEnvArgs expands Docker -e flags that reference environment variables
// Converts "-e VAR_NAME" to "-e VAR_NAME=value" by reading from the process environment
func ExpandEnvArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check if this is a -e flag
		if arg == "-e" && i+1 < len(args) {
			nextArg := args[i+1]
			// If next arg doesn't contain '=', it's a variable reference
			if len(nextArg) > 0 && !strings.Contains(nextArg, "=") {
				// Look up the variable in the environment
				if value, exists := os.LookupEnv(nextArg); exists {
					result = append(result, "-e")
					result = append(result, fmt.Sprintf("%s=%s", nextArg, value))
					i++ // Skip the next arg since we processed it
					continue
				}
			}
		}
		result = append(result, arg)
	}
	return result
}
