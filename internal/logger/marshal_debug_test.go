package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogMarshaledForDebug_Success(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	var (
		gotJSON       string
		gotMarshalErr error
	)

	LogMarshaledForDebug(
		map[string]string{"status": "ok"},
		func(resultJSON string) {
			gotJSON = resultJSON
		},
		func(err error) {
			gotMarshalErr = err
		},
	)

	require.NoError(gotMarshalErr)
	assert.JSONEq(`{"status":"ok"}`, gotJSON)
}

func TestLogMarshaledForDebug_MarshalFailure(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	var (
		gotJSON       string
		gotMarshalErr error
	)

	LogMarshaledForDebug(
		make(chan int),
		func(resultJSON string) {
			gotJSON = resultJSON
		},
		func(err error) {
			gotMarshalErr = err
		},
	)

	require.Error(gotMarshalErr)
	assert.Empty(gotJSON)
}
