package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/guard"
)

// containerGuardWasmPath is the baked-in guard path in the container image.
const containerGuardWasmPath = "/guards/github/00-github-guard.wasm"

// detectGuardWasm returns the baked-in container guard path if it exists,
// or empty string if not found (requiring the user to specify --guard-wasm).
func detectGuardWasm() string {
	debugLog.Printf("Checking for baked-in guard at %s", containerGuardWasmPath)
	if _, err := os.Stat(containerGuardWasmPath); err == nil {
		debugLog.Printf("Auto-detected baked-in guard: %s", containerGuardWasmPath)
		return containerGuardWasmPath
	}
	debugLog.Print("Baked-in guard not found, --guard-wasm flag required")
	return ""
}

func defaultWasmCacheDir(logDir string) string {
	if logDir == "" {
		return config.DefaultWasmCacheDirName
	}
	return filepath.Join(filepath.Dir(logDir), config.DefaultWasmCacheDirName)
}

func resolveWasmCacheDir(flagChanged bool, flagValue, effectiveLogDir string) string {
	if trimmed := strings.TrimSpace(flagValue); flagChanged && trimmed != "" {
		debugLog.Printf("WASM cache dir resolved from CLI flag: %q", trimmed)
		return trimmed
	}

	if envValue, exists := os.LookupEnv(wasmCacheDirEnvVar); exists {
		if trimmed := strings.TrimSpace(envValue); trimmed != "" {
			debugLog.Printf("WASM cache dir resolved from %s: %q", wasmCacheDirEnvVar, trimmed)
			return trimmed
		}
	}

	resolved := defaultWasmCacheDir(effectiveLogDir)
	debugLog.Printf("WASM cache dir resolved from default (logDir=%q): %q", effectiveLogDir, resolved)
	return resolved
}

func configureWasmCompilationCache(ctx context.Context, flagChanged bool, flagValue, effectiveLogDir string, warn func(string, ...interface{})) (string, error) {
	resolvedDir := resolveWasmCacheDir(flagChanged, flagValue, effectiveLogDir)
	debugLog.Printf("Configuring WASM compilation cache: resolvedDir=%q, flagChanged=%v", resolvedDir, flagChanged)

	if err := guard.ConfigureGlobalCompilationCache(ctx, resolvedDir); err == nil {
		if resolvedDir == "" {
			debugLog.Print("WASM compilation cache configured: mode=in-memory")
		} else {
			debugLog.Printf("WASM compilation cache configured: mode=disk, dir=%q", resolvedDir)
		}
		return resolvedDir, nil
	} else if resolvedDir == "" {
		return "", fmt.Errorf("failed to configure WASM compilation cache: %w", err)
	} else {
		debugLog.Printf("Disk-backed WASM cache failed, falling back to in-memory: dir=%q, err=%v", resolvedDir, err)
		if warn != nil {
			warn("Falling back to in-memory WASM compilation cache after %q failed: %v", resolvedDir, err)
		}
		if fallbackErr := guard.ConfigureGlobalCompilationCache(ctx, ""); fallbackErr != nil {
			return "", errors.Join(
				fmt.Errorf("failed to configure WASM compilation cache at %q: %w", resolvedDir, err),
				fmt.Errorf("failed to configure in-memory WASM compilation cache fallback: %w", fallbackErr),
			)
		}
		debugLog.Print("WASM compilation cache fallback configured: mode=in-memory")
		return "", nil
	}
}
