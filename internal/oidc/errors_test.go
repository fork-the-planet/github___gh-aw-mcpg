package oidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrMissingOIDCEnvVar(t *testing.T) {
	err := ErrMissingOIDCEnvVar("my-server")
	require.Error(t, err)
	assert.ErrorContains(t, err, `"my-server"`)
	assert.ErrorContains(t, err, "OIDC authentication")
	assert.ErrorContains(t, err, "ACTIONS_ID_TOKEN_REQUEST_URL")
	assert.ErrorContains(t, err, "permissions: { id-token: write }")
}
