package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/option"
	"google.golang.org/api/people/v1"
)

func TestExecute_ContactsMoreCommands_JSON(t *testing.T) {
	origContacts := newPeopleContactsService
	origOther := newPeopleOtherContactsService
	origDir := newPeopleDirectoryService
	t.Cleanup(func() {
		newPeopleContactsService = origContacts
		newPeopleOtherContactsService = origOther
		newPeopleDirectoryService = origDir
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "people/c1") && r.Method == http.MethodGet && !strings.Contains(path, ":"):
			// people.get (used by contacts update)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resourceName": "people/c1",
				"names":        []map[string]any{{"givenName": "Ada", "familyName": "Lovelace"}},
			})
			return
		case strings.Contains(path, "people:searchContacts") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"person": map[string]any{
							"resourceName": "people/c1",
							"names":        []map[string]any{{"displayName": "Ada"}},
							"emailAddresses": []map[string]any{
								{"value": "ada@example.com"},
							},
							"phoneNumbers": []map[string]any{{"value": "+1"}},
						},
					},
				},
			})
			return
		case strings.Contains(path, "people:createContact") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resourceName": "people/c1",
				"names":        []map[string]any{{"displayName": "Ada"}},
			})
			return
		case strings.Contains(path, "people/c1") && strings.Contains(path, ":updateContact") && (r.Method == http.MethodPatch || r.Method == http.MethodPost):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resourceName": "people/c1",
				"names":        []map[string]any{{"displayName": "Ada Updated"}},
			})
			return
		case strings.Contains(path, "people/c1:deleteContact") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		case strings.Contains(path, "people:listDirectoryPeople") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"people":        []map[string]any{{"resourceName": "people/d1", "names": []map[string]any{{"displayName": "Dir"}}}},
				"nextPageToken": "npt",
			})
			return
		case strings.Contains(path, "people:searchDirectoryPeople") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"people": []map[string]any{{"resourceName": "people/d2", "names": []map[string]any{{"displayName": "Dir2"}}}},
			})
			return
		case strings.Contains(path, "otherContacts:search") && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("readMask"); got != contactsOtherReadMask {
				t.Fatalf("other search readMask = %q, want %q", got, contactsOtherReadMask)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"person": map[string]any{
							"resourceName": "people/o1",
							"names":        []map[string]any{{"displayName": "Other"}},
						},
					},
				},
			})
			return
		case strings.Contains(path, "/otherContacts") && r.Method == http.MethodGet:
			// otherContacts.list
			if got := r.URL.Query().Get("readMask"); got != contactsOtherReadMask {
				t.Fatalf("other list readMask = %q, want %q", got, contactsOtherReadMask)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"otherContacts": []map[string]any{
					{"resourceName": "people/o1", "names": []map[string]any{{"displayName": "Other"}}},
				},
				"nextPageToken": "npt",
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	svc, err := people.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	newPeopleContactsService = func(context.Context, string) (*people.Service, error) { return svc, nil }
	newPeopleOtherContactsService = func(context.Context, string) (*people.Service, error) { return svc, nil }
	newPeopleDirectoryService = func(context.Context, string) (*people.Service, error) { return svc, nil }

	_ = captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "search", "Ada"}); err != nil {
				t.Fatalf("search: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "create", "--given", "Ada", "--email", "ada@example.com", "--phone", "+1"}); err != nil {
				t.Fatalf("create: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "update", "people/c1", "--given", "Ada", "--family", "Updated"}); err != nil {
				t.Fatalf("update: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--force", "--account", "a@b.com", "contacts", "delete", "people/c1"}); err != nil {
				t.Fatalf("delete: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "directory", "list", "--max", "1"}); err != nil {
				t.Fatalf("dir list: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "directory", "search", "Dir", "--max", "1"}); err != nil {
				t.Fatalf("dir search: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "other", "list", "--max", "1"}); err != nil {
				t.Fatalf("other list: %v", err)
			}
		})
		_ = captureStdout(t, func() {
			if err := Execute([]string{"--json", "--account", "a@b.com", "contacts", "other", "search", "Other"}); err != nil {
				t.Fatalf("other search: %v", err)
			}
		})
	})
}

func TestExecute_ContactsDirectoryOtherInvalidMaxFailsBeforeService(t *testing.T) {
	origOther := newPeopleOtherContactsService
	origDir := newPeopleDirectoryService
	t.Cleanup(func() {
		newPeopleOtherContactsService = origOther
		newPeopleDirectoryService = origDir
	})
	newPeopleOtherContactsService = func(context.Context, string) (*people.Service, error) {
		t.Fatalf("expected max validation to fail before creating other contacts service")
		return nil, context.Canceled
	}
	newPeopleDirectoryService = func(context.Context, string) (*people.Service, error) {
		t.Fatalf("expected max validation to fail before creating directory service")
		return nil, context.Canceled
	}

	testCases := [][]string{
		{"--account", "a@b.com", "contacts", "directory", "list", "--max", "0"},
		{"--account", "a@b.com", "contacts", "directory", "list", "--max=-1"},
		{"--account", "a@b.com", "contacts", "directory", "search", "alice", "--max", "0"},
		{"--account", "a@b.com", "contacts", "directory", "search", "alice", "--max=-1"},
		{"--account", "a@b.com", "contacts", "other", "list", "--max", "0"},
		{"--account", "a@b.com", "contacts", "other", "list", "--max=-1"},
		{"--account", "a@b.com", "contacts", "other", "search", "alice", "--max", "0"},
		{"--account", "a@b.com", "contacts", "other", "search", "alice", "--max=-1"},
	}
	for _, args := range testCases {
		t.Run(strings.Join(args[2:], "_"), func(t *testing.T) {
			_ = captureStderr(t, func() {
				err := Execute(args)
				if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "max must be > 0") {
					t.Fatalf("unexpected err: %v", err)
				}
			})
		})
	}
}
