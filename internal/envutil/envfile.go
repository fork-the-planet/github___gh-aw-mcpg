package envutil

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/logger/sanitize"
)

// LoadEnvFile reads a .env file and sets environment variables.
// Lines beginning with '#' and blank lines are ignored.
// Each remaining line is expected in KEY=VALUE format; lines without '='
// are silently skipped. Values may reference existing environment variables
// using $VAR or ${VAR} syntax (expanded via os.ExpandEnv).
func LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	log.Printf("Loading environment from %s...", path)
	scanner := bufio.NewScanner(file)
	loadedVars := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Expand $VAR references in value
		value = os.ExpandEnv(value)

		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("failed to set %s: %w", key, err)
		}

		// Log loaded variable (hide sensitive values)
		log.Printf("  Loaded: %s=%s", key, sanitize.TruncateSecret(value))
		loadedVars++
	}

	log.Printf("Loaded %d environment variables from %s", loadedVars, path)

	return scanner.Err()
}
