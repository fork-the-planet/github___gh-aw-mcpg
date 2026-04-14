package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSONResponse(t *testing.T) {
	t.Run("sets content-type to application/json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{"key": "value"})

		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})

	t.Run("writes the provided status code", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
		}{
			{"200 OK", http.StatusOK},
			{"201 Created", http.StatusCreated},
			{"400 Bad Request", http.StatusBadRequest},
			{"404 Not Found", http.StatusNotFound},
			{"500 Internal Server Error", http.StatusInternalServerError},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				WriteJSONResponse(rec, tt.statusCode, nil)

				assert.Equal(t, tt.statusCode, rec.Code)
			})
		}
	})

	t.Run("encodes body as JSON", func(t *testing.T) {
		type payload struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, payload{Name: "test", Count: 42})

		var got payload
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "test", got.Name)
		assert.Equal(t, 42, got.Count)
	})

	t.Run("encodes map body as JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]interface{}{
			"error":   "not found",
			"code":    404,
			"details": []string{"a", "b"},
		})

		var got map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "not found", got["error"])
		assert.Equal(t, float64(404), got["code"])
		assert.Len(t, got["details"], 2)
	})

	t.Run("encodes nil body as JSON null", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, nil)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, "null", rec.Body.String())
	})

	t.Run("encodes empty struct as empty JSON object", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, struct{}{})

		assert.JSONEq(t, "{}", rec.Body.String())
	})

	t.Run("encodes slice body as JSON array", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, []string{"alpha", "beta"})

		var got []string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, []string{"alpha", "beta"}, got)
	})

	t.Run("encodes nested structs", func(t *testing.T) {
		type inner struct {
			ID int `json:"id"`
		}
		type outer struct {
			Items []inner `json:"items"`
		}
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, outer{Items: []inner{{ID: 1}, {ID: 2}}})

		var got outer
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		require.Len(t, got.Items, 2)
		assert.Equal(t, 1, got.Items[0].ID)
		assert.Equal(t, 2, got.Items[1].ID)
	})

	t.Run("body with special characters", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{
			"msg": `hello "world" & <friends>`,
		})

		var got map[string]string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, `hello "world" & <friends>`, got["msg"])
	})

	t.Run("body with unicode", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSONResponse(rec, http.StatusOK, map[string]string{
			"greeting": "こんにちは 🌍",
		})

		var got map[string]string
		err := json.NewDecoder(rec.Body).Decode(&got)
		require.NoError(t, err)
		assert.Equal(t, "こんにちは 🌍", got["greeting"])
	})

	t.Run("marshal failure writes headers but no body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		// Channels cannot be marshaled to JSON; json.Marshal returns an error.
		WriteJSONResponse(rec, http.StatusInternalServerError, make(chan int))

		// Content-Type and status code are committed before the marshal attempt,
		// so they are still present even when encoding fails.
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		// No body is written when encoding fails.
		assert.Empty(t, rec.Body.String())
	})
}
