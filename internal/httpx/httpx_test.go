package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type envelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()

	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not a JSON envelope (%v): %s", err, rec.Body.String())
	}

	return env
}

func TestWriteErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	// A quote and a newline would break a body built with Sprintf.
	WriteError(rec, http.StatusConflict, "email_already_exists", "\"quoted\"\nmessage")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	env := decodeEnvelope(t, rec)
	if env.Error.Code != "email_already_exists" || env.Error.Message != "\"quoted\"\nmessage" {
		t.Errorf("envelope = %+v", env)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteJSON(rec, http.StatusCreated, map[string]string{"id": "abc"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"id":"abc"}` {
		t.Errorf("body = %s", body)
	}
}

// A value that cannot be marshalled must yield a clean 500, not a 200 with
// a truncated body.
func TestWriteJSONMarshalFailure(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteJSON(rec, http.StatusOK, map[string]any{"bad": make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != CodeInternal {
		t.Errorf("code = %q, want %q", env.Error.Code, CodeInternal)
	}
}

func TestWriteNoContent(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteNoContent(rec)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q on a 204", ct)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 carried a body: %q", rec.Body.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
		if err := DecodeJSON(httptest.NewRecorder(), req, &dst); err != nil {
			t.Fatalf("DecodeJSON: %v", err)
		}
		if dst.Name != "x" {
			t.Errorf("name = %q, want x", dst.Name)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
		if err := DecodeJSON(httptest.NewRecorder(), req, &dst); err == nil {
			t.Error("malformed JSON was accepted")
		}
	})

	t.Run("body over the cap", func(t *testing.T) {
		huge := `{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(huge))
		if err := DecodeJSON(httptest.NewRecorder(), req, &dst); err == nil {
			t.Error("a body larger than MaxBodyBytes was accepted")
		}
	})
}

func TestURLParamUUID(t *testing.T) {
	want := uuid.New()

	serve := func(target string) (uuid.UUID, bool, *httptest.ResponseRecorder) {
		var (
			got uuid.UUID
			ok  bool
		)
		r := chi.NewRouter()
		r.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
			got, ok = URLParamUUID(w, r, "id")
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

		return got, ok, rec
	}

	t.Run("valid", func(t *testing.T) {
		got, ok, rec := serve("/items/" + want.String())
		if !ok || got != want {
			t.Errorf("got (%v, %v), want (%v, true)", got, ok, want)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("a valid ID wrote a response: %s", rec.Body.String())
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, ok, rec := serve("/items/not-a-uuid")
		if ok {
			t.Fatal("an invalid ID was accepted")
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if env := decodeEnvelope(t, rec); env.Error.Code != CodeInvalidID {
			t.Errorf("code = %q, want %q", env.Error.Code, CodeInvalidID)
		}
	})
}

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		limit   int
		offset  int
		wantErr bool
	}{
		{name: "absent uses the default", query: "", limit: 50},
		{name: "explicit values", query: "?limit=10&offset=20", limit: 10, offset: 20},
		{name: "range is left to the service", query: "?limit=-1&offset=-5", limit: -1, offset: -5},
		{name: "non-integer limit", query: "?limit=ten", wantErr: true},
		{name: "non-integer offset", query: "?offset=1.5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+tt.query, nil)

			limit, offset, err := ParsePagination(req, 50)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if limit != tt.limit || offset != tt.offset {
				t.Errorf("got (%d, %d), want (%d, %d)", limit, offset, tt.limit, tt.offset)
			}
		})
	}
}
