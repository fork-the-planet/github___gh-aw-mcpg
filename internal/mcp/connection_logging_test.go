package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogInboundRPCResponseFromResult_LogsMarshaledResponseAndReturnsResultAndError(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")

	require.NoError(logger.InitJSONLLogger(logDir, "rpc-messages.jsonl"))
	t.Cleanup(func() {
		require.NoError(logger.CloseJSONLLogger())
	})

	expectedErr := errors.New("expected error")
	expectedResult := &Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  []byte(`{"ok":true}`),
	}

	result, err := logInboundRPCResponseFromResult("test-server", expectedResult, expectedErr, nil)

	assert.Same(expectedResult, result)
	assert.ErrorIs(err, expectedErr)

	logFile, err := os.Open(filepath.Join(logDir, "rpc-messages.jsonl"))
	require.NoError(err)
	defer logFile.Close()

	scanner := bufio.NewScanner(logFile)
	require.True(scanner.Scan(), "expected a JSONL entry to be logged")

	var entry logger.JSONLRPCMessage
	require.NoError(json.Unmarshal([]byte(scanner.Text()), &entry))
	assert.Equal(string(logger.RPCDirectionInbound), entry.Direction)
	assert.Equal("rpc_response", entry.Event)
	assert.Equal("rpc-message/v2", entry.Schema)
	assert.Equal("test-server", entry.ServerID)
	assert.Equal(expectedErr.Error(), entry.Error)
	assert.JSONEq(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`, string(entry.Payload))
	assert.False(scanner.Scan(), "expected exactly one JSONL entry")
	require.NoError(scanner.Err())
}

func TestLogInboundRPCResponseFromResult_AllowsNilResult(t *testing.T) {
	assert := assert.New(t)

	result, err := logInboundRPCResponseFromResult("test-server", nil, nil, nil)

	assert.Nil(result)
	assert.NoError(err)
}
