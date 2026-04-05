package envutil

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logExpand = logger.New("envutil:expand")

// ExpandEnvArgs expands Docker -e flags that reference environment variables.
// Converts "-e VAR_NAME" to "-e VAR_NAME=value" by reading from the process environment.
// If the variable is not set, the flag is passed through unchanged.
func ExpandEnvArgs(args []string) []string {
	logExpand.Printf("Expanding env args: input_count=%d", len(args))
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
					logExpand.Printf("Expanding env var: name=%s", nextArg)
					result = append(result, "-e")
					result = append(result, fmt.Sprintf("%s=%s", nextArg, value))
					i++ // Skip the next arg since we processed it
					continue
				}
				logExpand.Printf("Env var not found in process environment: name=%s", nextArg)
			}
		}
		result = append(result, arg)
	}
	logExpand.Printf("Env args expansion complete: output_count=%d", len(result))
	return result
}
