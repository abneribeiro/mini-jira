package task

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func decodeUpdate(t *testing.T, body string) UpdateInput {
	t.Helper()

	var input UpdateInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}

	return input
}

// The SET clauses and their arguments must stay in lockstep: a mismatch
// between $N placeholders and the args slice is a runtime Postgres error that
// no compiler catches.
func TestBuildUpdateSetPlaceholdersMatchArgs(t *testing.T) {
	assignee := uuid.New()

	tests := []struct {
		name        string
		body        string
		wantClauses []string
	}{
		{"no fields", `{}`, nil},
		{"title only", `{"title":"t"}`, []string{"title = $1"}},
		{"description only", `{"description":"d"}`, []string{"description = $1"}},
		{"status only", `{"status":"DONE"}`, []string{"status = $1"}},
		{"assignee only", `{"assignee_id":"` + assignee.String() + `"}`, []string{"assignee_id = $1"}},
		{"clear assignee", `{"assignee_id":null}`, []string{"assignee_id = $1"}},
		{
			"title and status",
			`{"title":"t","status":"DONE"}`,
			[]string{"title = $1", "status = $2"},
		},
		{
			"every field",
			`{"title":"t","description":"d","status":"DONE","assignee_id":"` + assignee.String() + `"}`,
			[]string{"title = $1", "description = $2", "status = $3", "assignee_id = $4"},
		},
		{
			"gap in the middle keeps numbering dense",
			`{"title":"t","assignee_id":null}`,
			[]string{"title = $1", "assignee_id = $2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := buildUpdateSet(decodeUpdate(t, tt.body))

			if strings.Join(clauses, ", ") != strings.Join(tt.wantClauses, ", ") {
				t.Errorf("clauses = %v, want %v", clauses, tt.wantClauses)
			}
			if len(args) != len(clauses) {
				t.Fatalf("%d args for %d clauses; placeholders and args must match 1:1", len(args), len(clauses))
			}
		})
	}
}

// Regression: "assignee_id": null must emit a SET clause binding a nil
// pointer (SQL NULL). Treating it as "field absent" made unassigning a task
// impossible through the API.
func TestBuildUpdateSetClearsAssignee(t *testing.T) {
	t.Run("explicit null emits a clause binding nil", func(t *testing.T) {
		clauses, args := buildUpdateSet(decodeUpdate(t, `{"assignee_id":null}`))

		if len(clauses) != 1 {
			t.Fatalf("clauses = %v, want exactly one assignee clause", clauses)
		}
		if len(args) != 1 {
			t.Fatalf("args = %v, want exactly one", args)
		}

		ptr, ok := args[0].(*uuid.UUID)
		if !ok {
			t.Fatalf("arg type = %T, want *uuid.UUID so pgx binds SQL NULL", args[0])
		}
		if ptr != nil {
			t.Errorf("arg = %v, want a nil pointer", ptr)
		}
	})

	t.Run("absent field emits no clause", func(t *testing.T) {
		clauses, _ := buildUpdateSet(decodeUpdate(t, `{"title":"t"}`))

		for _, c := range clauses {
			if strings.HasPrefix(c, "assignee_id") {
				t.Errorf("an absent assignee_id produced %q; absent must differ from null", c)
			}
		}
	})

	t.Run("explicit uuid binds that uuid", func(t *testing.T) {
		assignee := uuid.New()

		_, args := buildUpdateSet(decodeUpdate(t, `{"assignee_id":"`+assignee.String()+`"}`))

		ptr, ok := args[0].(*uuid.UUID)
		if !ok || ptr == nil {
			t.Fatalf("arg = %#v, want a non-nil *uuid.UUID", args[0])
		}
		if *ptr != assignee {
			t.Errorf("arg = %v, want %v", *ptr, assignee)
		}
	})
}
