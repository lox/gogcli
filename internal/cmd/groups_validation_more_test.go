package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/cloudidentity/v1"
	"google.golang.org/api/option"

	"github.com/steipete/gogcli/internal/ui"
)

func TestGroupsMembers_ValidationErrors(t *testing.T) {
	u, uiErr := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	if uiErr != nil {
		t.Fatalf("ui.New: %v", uiErr)
	}
	ctx := ui.WithUI(context.Background(), u)

	if err := (&GroupsMembersCmd{}).Run(ctx, &RootFlags{}); err == nil {
		t.Fatalf("expected missing account error")
	}
	if err := (&GroupsMembersCmd{}).Run(ctx, &RootFlags{Account: "a@b.com"}); err == nil {
		t.Fatalf("expected missing group email error")
	}
}

func TestGroupsInvalidMaxFailsBeforeService(t *testing.T) {
	origNew := newCloudIdentityService
	t.Cleanup(func() { newCloudIdentityService = origNew })
	newCloudIdentityService = func(context.Context, string) (*cloudidentity.Service, error) {
		t.Fatalf("expected max validation to fail before creating Cloud Identity service")
		return nil, errors.New("unexpected service call")
	}

	u, uiErr := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	if uiErr != nil {
		t.Fatalf("ui.New: %v", uiErr)
	}
	ctx := ui.WithUI(context.Background(), u)
	flags := &RootFlags{Account: "a@b.com"}

	testCases := []struct {
		name string
		run  func() error
	}{
		{name: "list zero", run: func() error { return (&GroupsListCmd{Max: 0}).Run(ctx, flags) }},
		{name: "list negative", run: func() error { return (&GroupsListCmd{Max: -1}).Run(ctx, flags) }},
		{name: "members zero", run: func() error { return (&GroupsMembersCmd{GroupEmail: "eng@example.com", Max: 0}).Run(ctx, flags) }},
		{name: "members negative", run: func() error { return (&GroupsMembersCmd{GroupEmail: "eng@example.com", Max: -1}).Run(ctx, flags) }},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "max must be > 0") {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestGroupsList_NoGroups_Text(t *testing.T) {
	origNew := newCloudIdentityService
	t.Cleanup(func() { newCloudIdentityService = origNew })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "groups/-/memberships:searchTransitiveGroups") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memberships": []map[string]any{},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc, err := cloudidentity.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	newCloudIdentityService = func(context.Context, string) (*cloudidentity.Service, error) { return svc, nil }

	var errBuf strings.Builder
	u, uiErr := ui.New(ui.Options{Stdout: io.Discard, Stderr: &errBuf, Color: "never"})
	if uiErr != nil {
		t.Fatalf("ui.New: %v", uiErr)
	}
	ctx := ui.WithUI(context.Background(), u)

	if err := (&GroupsListCmd{Max: 100}).Run(ctx, &RootFlags{Account: "a@b.com"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(errBuf.String(), "No groups found") {
		t.Fatalf("unexpected stderr: %q", errBuf.String())
	}
}

func TestWrapCloudIdentityError_Messages(t *testing.T) {
	accessErr := errors.New("accessNotConfigured")
	if err := wrapCloudIdentityError(accessErr, "user@company.com"); err == nil || !strings.Contains(err.Error(), "Cloud Identity API is not enabled") {
		t.Fatalf("unexpected error: %v", err)
	}

	permErr := errors.New("insufficientPermissions")
	if err := wrapCloudIdentityError(permErr, "user@company.com"); err == nil || !strings.Contains(err.Error(), "Insufficient permissions") {
		t.Fatalf("unexpected error: %v", err)
	}

	other := errors.New("other")
	if err := wrapCloudIdentityError(other, "user@company.com"); err == nil || err.Error() != "other" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetRelationType_More(t *testing.T) {
	if got := getRelationType("DIRECT"); got != "direct" {
		t.Fatalf("unexpected DIRECT: %q", got)
	}
	if got := getRelationType("INDIRECT"); got != "indirect" {
		t.Fatalf("unexpected INDIRECT: %q", got)
	}
	if got := getRelationType("OTHER"); got != "OTHER" {
		t.Fatalf("unexpected OTHER: %q", got)
	}
}
