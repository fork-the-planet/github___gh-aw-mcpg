package httputil

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestReadResponseBody(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantBody  string
		wantErr   string
		wantNoErr bool
	}{
		{
			name:      "success 200",
			status:    http.StatusOK,
			body:      `{"ok":true}`,
			wantBody:  `{"ok":true}`,
			wantNoErr: true,
		},
		{
			name:      "success 201",
			status:    http.StatusCreated,
			body:      `created`,
			wantBody:  `created`,
			wantNoErr: true,
		},
		{
			name:      "success 302 redirect",
			status:    http.StatusFound,
			body:      `redirect`,
			wantBody:  `redirect`,
			wantNoErr: true,
		},
		{
			name:    "error 400",
			status:  http.StatusBadRequest,
			body:    `bad request`,
			wantErr: "GitHub API returned HTTP 400",
		},
		{
			name:    "error 404",
			status:  http.StatusNotFound,
			body:    `not found`,
			wantErr: "GitHub API returned HTTP 404",
		},
		{
			name:    "error 500",
			status:  http.StatusInternalServerError,
			body:    `server error`,
			wantErr: "GitHub API returned HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := newResponse(tt.status, tt.body)
			body, err := ReadResponseBody(resp, "GitHub API")

			if tt.wantNoErr {
				require.NoError(t, err)
				assert.Equal(t, tt.wantBody, string(body))
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, body)
			}
		})
	}
}

func TestReadResponseBody_ReadError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&failReader{}),
	}
	body, err := ReadResponseBody(resp, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read test response")
	assert.Nil(t, body)
}

func TestReadResponseBodyStrict(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		expectedStatus int
		body           string
		wantBody       string
		wantErr        string
		wantNoErr      bool
	}{
		{
			name:           "exact match 200",
			status:         http.StatusOK,
			expectedStatus: http.StatusOK,
			body:           `{"token":"abc"}`,
			wantBody:       `{"token":"abc"}`,
			wantNoErr:      true,
		},
		{
			name:           "exact match 201",
			status:         http.StatusCreated,
			expectedStatus: http.StatusCreated,
			body:           `created`,
			wantBody:       `created`,
			wantNoErr:      true,
		},
		{
			name:           "mismatch 201 vs 200",
			status:         http.StatusCreated,
			expectedStatus: http.StatusOK,
			body:           `unexpected`,
			wantErr:        "OIDC returned HTTP 201: unexpected",
		},
		{
			name:           "error 403 includes body",
			status:         http.StatusForbidden,
			expectedStatus: http.StatusOK,
			body:           `{"error":"forbidden"}`,
			wantErr:        `OIDC returned HTTP 403: {"error":"forbidden"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := newResponse(tt.status, tt.body)
			body, err := ReadResponseBodyStrict(resp, tt.expectedStatus, "OIDC")

			if tt.wantNoErr {
				require.NoError(t, err)
				assert.Equal(t, tt.wantBody, string(body))
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, body)
			}
		})
	}
}

func TestReadResponseBodyStrict_ReadError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&failReader{}),
	}
	body, err := ReadResponseBodyStrict(resp, http.StatusOK, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read test response")
	assert.Nil(t, body)
}

type failReader struct{}

func (f *failReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
