package difc

import (
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logSink = logger.New("difc:sink_server_ids")

var (
	sinkServerIDsMu sync.RWMutex
	sinkServerIDs   = []string{}
)

// SetSinkServerIDs configures backend server IDs that should receive DIFC tag snapshot
// enrichment in RPC JSONL logs.
func SetSinkServerIDs(serverIDs []string) {
	logSink.Printf("Setting sink server IDs: input_count=%d", len(serverIDs))
	sinkServerIDsMu.Lock()
	defer sinkServerIDsMu.Unlock()

	if len(serverIDs) == 0 {
		logSink.Print("No sink server IDs provided, clearing configuration")
		sinkServerIDs = nil
		return
	}

	unique := make(map[string]struct{}, len(serverIDs))
	normalized := make([]string, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		trimmed := strings.TrimSpace(serverID)
		if trimmed == "" {
			continue
		}
		if _, exists := unique[trimmed]; exists {
			logSink.Printf("Skipping duplicate sink server ID: %s", trimmed)
			continue
		}
		unique[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	sort.Strings(normalized)
	sinkServerIDs = normalized
	logSink.Printf("Sink server IDs configured: count=%d, ids=%v", len(normalized), normalized)
}

// IsSinkServerID reports whether serverID is in the configured set of DIFC sink server IDs.
func IsSinkServerID(serverID string) bool {
	sinkServerIDsMu.RLock()
	defer sinkServerIDsMu.RUnlock()

	for _, sinkServerID := range sinkServerIDs {
		if serverID == sinkServerID {
			logSink.Printf("Sink server ID match: serverID=%s", serverID)
			return true
		}
	}
	return false
}
