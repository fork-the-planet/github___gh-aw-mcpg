package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/github/gh-aw-mcpg/internal/strutil"
)

const observedURLDomainsFileName = "observed-url-domains.json"

var urlDomainAuditEnabled atomic.Bool

// SetURLDomainAuditEnabled toggles URL domain observation for middleware and guards.
func SetURLDomainAuditEnabled(enabled bool) {
	urlDomainAuditEnabled.Store(enabled)
}

// URLDomainAuditEnabled reports whether URL domain observation is enabled.
func URLDomainAuditEnabled() bool {
	return urlDomainAuditEnabled.Load()
}

// ObservedURLDomainsLogger manages unique observed URL domains grouped by server ID.
type ObservedURLDomainsLogger struct {
	lockable
	logDir      string
	fileName    string
	data        map[string]map[string]struct{}
	useFallback bool
}

var (
	globalObservedURLDomainsLogger *ObservedURLDomainsLogger
	globalObservedURLDomainsMu     sync.RWMutex
)

func setupObservedURLDomainsLogger(file *os.File, logDir, fileName string) (*ObservedURLDomainsLogger, error) {
	if file != nil {
		file.Close()
	}

	l := &ObservedURLDomainsLogger{
		logDir:   logDir,
		fileName: fileName,
		data:     make(map[string]map[string]struct{}),
	}
	if err := l.writeToFile(); err != nil {
		return nil, err
	}
	log.Printf("Observed URL domains logging to file: %s", filepath.Join(logDir, fileName))
	return l, nil
}

func handleObservedURLDomainsLoggerError(err error, logDir, fileName string) (*ObservedURLDomainsLogger, error) {
	return fallbackLoggerOnInitError(err, "Failed to initialize observed URL domains log file", "Observed URL domains logging disabled", &ObservedURLDomainsLogger{
		logDir:      logDir,
		fileName:    fileName,
		data:        make(map[string]map[string]struct{}),
		useFallback: true,
	})
}

var observedURLDomainsLoggerFactory = loggerFactory[*ObservedURLDomainsLogger]{
	setup:   setupObservedURLDomainsLogger,
	onError: handleObservedURLDomainsLoggerError,
}

// InitObservedURLDomainsLogger initializes observed-url-domains.json logger.
func InitObservedURLDomainsLogger(logDir, fileName string) error {
	l, err := initLogger(logDir, fileName, os.O_TRUNC, observedURLDomainsLoggerFactory)
	initGlobalLogger(&globalObservedURLDomainsMu, &globalObservedURLDomainsLogger, l)
	return err
}

// LogDomains logs unique domains for a server ID.
func (l *ObservedURLDomainsLogger) LogDomains(serverID string, domains []string) error {
	if serverID == "" || len(domains) == 0 {
		return nil
	}

	return l.withLock(func() error {
		if l.useFallback {
			return nil
		}

		serverDomains, ok := l.data[serverID]
		if !ok {
			serverDomains = make(map[string]struct{})
			l.data[serverID] = serverDomains
		}

		changed := false
		for _, d := range domains {
			if d == "" {
				continue
			}
			if _, exists := serverDomains[d]; exists {
				continue
			}
			serverDomains[d] = struct{}{}
			changed = true
		}

		if !changed {
			return nil
		}
		return l.writeToFile()
	})
}

func (l *ObservedURLDomainsLogger) writeToFile() error {
	serialized := make(map[string][]string, len(l.data))
	for serverID, domains := range l.data {
		serialized[serverID] = strutil.SortedSetKeys(domains)
	}

	jsonData, err := json.MarshalIndent(serialized, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal observed URL domains: %w", err)
	}

	filePath := filepath.Join(l.logDir, l.fileName)
	return atomicWriteFile(filePath, jsonData, 0600)
}

func (l *ObservedURLDomainsLogger) Close() error { return nil }

// LogObservedURLDomains appends newly observed domains for a server.
func LogObservedURLDomains(serverID string, domains []string) {
	withGlobalLogger(&globalObservedURLDomainsMu, &globalObservedURLDomainsLogger, func(l *ObservedURLDomainsLogger) {
		if err := l.LogDomains(serverID, domains); err != nil {
			log.Printf("WARNING: Failed to log observed URL domains for server %s: %v", serverID, err)
		}
	})
}
