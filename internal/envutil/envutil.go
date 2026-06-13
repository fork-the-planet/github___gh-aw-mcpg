package envutil

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/sanitize"
)

var logEnvUtil = logger.New("envutil:envutil")

// HasEnvVar reports whether the named environment variable is present in the
// process environment, regardless of whether its value is empty.
func HasEnvVar(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}

// GetEnvString returns the value of the environment variable specified by envKey.
// If the environment variable is not set or is empty, it returns the defaultValue.
func GetEnvString(envKey, defaultValue string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvIntRaw returns the raw integer value of envKey without applying defaults
// or positivity constraints.
// It returns (0, false, nil) when envKey is unset or empty.
// It returns (0, true, err) when envKey is set but cannot be parsed as an integer.
func GetEnvIntRaw(envKey string) (int, bool, error) {
	envValue := os.Getenv(envKey)
	if envValue == "" {
		return 0, false, nil
	}

	value, err := strconv.Atoi(envValue)
	if err != nil {
		return 0, true, err
	}
	return value, true, nil
}

// GetEnvInt returns the integer value of the environment variable specified by envKey.
// If the environment variable is not set, is empty, cannot be parsed as an integer,
// or is not positive (> 0), it returns the defaultValue.
// This function validates that the value is a positive integer.
func GetEnvInt(envKey string, defaultValue int) int {
	value, ok, err := GetEnvIntRaw(envKey)
	if !ok {
		return defaultValue
	}
	if err == nil && value > 0 {
		return value
	}
	logEnvUtil.Printf("GetEnvInt: %s=%q is not a valid positive integer, using default=%d", envKey, sanitize.TruncateSecret(os.Getenv(envKey)), defaultValue)
	return defaultValue
}

// GetEnvDuration returns the time.Duration value of the environment variable specified by envKey.
// If the environment variable is not set, is empty, cannot be parsed by time.ParseDuration,
// or is not positive (> 0), it returns the defaultValue.
// Accepts any string valid for time.ParseDuration (e.g. "2h", "30m", "90s").
func GetEnvDuration(envKey string, defaultValue time.Duration) time.Duration {
	if envValue := os.Getenv(envKey); envValue != "" {
		if d, err := time.ParseDuration(envValue); err == nil && d > 0 {
			return d
		}
		logEnvUtil.Printf("GetEnvDuration: %s=%q is not a valid positive duration, using default=%v", envKey, sanitize.TruncateSecret(envValue), defaultValue)
	}
	return defaultValue
}

// GetEnvBool returns the boolean value of the environment variable specified by envKey.
// If the environment variable is not set or is empty, it returns the defaultValue.
// Truthy values (case-insensitive): "1", "true", "yes", "on"
// Falsy values (case-insensitive): "0", "false", "no", "off"
// Any other value returns the defaultValue.
func GetEnvBool(envKey string, defaultValue bool) bool {
	if envValue := os.Getenv(envKey); envValue != "" {
		switch strings.ToLower(envValue) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
		logEnvUtil.Printf("GetEnvBool: %s=%q is not a recognized boolean value, using default=%v", envKey, sanitize.TruncateSecret(envValue), defaultValue)
	}
	return defaultValue
}
