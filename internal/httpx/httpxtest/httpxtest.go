// Package httpxtest holds assertions on httpx responses for handler tests.
// Only test files import it.
package httpxtest

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// ErrorCode decodes the error envelope and returns its code. It fails the
// test when the body is not valid JSON or the envelope has no message.
func ErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON (%v): %s", err, rec.Body.String())
	}
	if body.Error.Message == "" {
		t.Errorf("error envelope has no message: %s", rec.Body.String())
	}

	return body.Error.Code
}
