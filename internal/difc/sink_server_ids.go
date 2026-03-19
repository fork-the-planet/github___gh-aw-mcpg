package difc

import (
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/github/gh-aw-mcpg/internal/strutil"
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

	if len(serverIDs) == 0 {
		sinkServerIDs = nil
		sinkServerIDsMu.Unlock()

		logSink.Print("No sink server IDs provided, clearing configuration")
		return
	}

	normalized := strutil.DeduplicateStrings(serverIDs, true)
	if len(normalized) < len(serverIDs) {
		logSink.Printf("Removed %d duplicate or empty sink server IDs", len(serverIDs)-len(normalized))
	}
	sinkServerIDs = normalized

	sinkServerIDsMu.Unlock()

	logSink.Printf("Sink server IDs configured: count=%d, ids=%v", len(normalized), normalized)
}

// IsSinkServerID reports whether serverID is in the configured set of DIFC sink server IDs.
func IsSinkServerID(serverID string) bool {
	sinkServerIDsMu.RLock()
	matched := false
	for _, sinkServerID := range sinkServerIDs {
		if serverID == sinkServerID {
			matched = true
			break
		}
	}
	sinkServerIDsMu.RUnlock()

	if matched {
		logSink.Printf("Sink server ID match: serverID=%s", serverID)
		return true
	}
	return false
}
