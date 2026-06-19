package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"github.com/steipete/gogcli/internal/app"
	"github.com/steipete/gogcli/internal/ui"
)

type docsBatchUpdateCapture struct {
	GetCalls int
	Requests [][]*docs.Request
}

func newDocsBatchUpdateTestService(t *testing.T, document any) (*docs.Service, *docsBatchUpdateCapture) {
	t.Helper()
	capture := &docsBatchUpdateCapture{}
	svc, _ := newDocsServiceForTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/documents/"):
			capture.GetCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(document)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, ":batchUpdate"):
			var request docs.BatchUpdateDocumentRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode batchUpdate: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			capture.Requests = append(capture.Requests, request.Requests)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"documentId":"doc1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return svc, capture
}

func newDocsDocumentTestService(t *testing.T, document any, includeTabs *string) *docs.Service {
	t.Helper()
	svc, _ := newDocsServiceForTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/documents/") {
			http.NotFound(w, r)
			return
		}
		if includeTabs != nil {
			*includeTabs = r.URL.Query().Get("includeTabsContent")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(document)
	}))
	return svc
}

func newDocsServiceForTest(t *testing.T, h http.HandlerFunc) (*docs.Service, func()) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	docSvc, err := docs.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewDocsService: %v", err)
	}
	return docSvc, func() {} // retained for call-site compat; cleanup is via t.Cleanup
}

func withDocsTestService(ctx context.Context, svc *docs.Service) context.Context {
	return withDocsTestServiceFactory(ctx, func(context.Context, string) (*docs.Service, error) {
		return svc, nil
	})
}

func withDocsTestServiceFactory(ctx context.Context, factory app.DocsServiceFactory) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime := &app.Runtime{}
	if existing, ok := app.FromContext(ctx); ok {
		*runtime = *existing
	}
	runtime.Services.Docs = factory
	return app.WithRuntime(ctx, runtime)
}

func withDocsTestHTTPClientFactory(ctx context.Context, factory app.DocsHTTPClientFactory) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime := &app.Runtime{}
	if existing, ok := app.FromContext(ctx); ok {
		*runtime = *existing
	}
	runtime.Services.DocsHTTP = factory
	return app.WithRuntime(ctx, runtime)
}

func newDocsCmdContext(t *testing.T) context.Context {
	t.Helper()
	u, err := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	return ui.WithUI(context.Background(), u)
}

func newDocsCmdOutputContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	u, err := ui.New(ui.Options{Stdout: &out, Stderr: io.Discard, Color: "never"})
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	return ui.WithUI(context.Background(), u), &out
}

func newDocsJSONContextWithDrive(t *testing.T, svc *drive.Service) context.Context {
	t.Helper()
	return withDriveTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
}

func newDocsJSONContextWithoutDrive(t *testing.T, message string) context.Context {
	t.Helper()
	return withDriveTestServiceFactory(
		newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard),
		func(context.Context, string) (*drive.Service, error) {
			t.Fatal(message)
			return nil, errors.New("unexpected Drive service call")
		},
	)
}

func docBodyWithText(text string) map[string]any {
	return map[string]any{
		"documentId": "doc1",
		"body": map[string]any{
			"content": []any{
				map[string]any{
					"startIndex":   0,
					"endIndex":     1,
					"sectionBreak": map[string]any{"sectionStyle": map[string]any{}},
				},
				map[string]any{
					"startIndex": 1,
					"endIndex":   1 + len(text),
					"paragraph": map[string]any{
						"elements": []any{
							map[string]any{
								"startIndex": 1,
								"endIndex":   1 + len(text),
								"textRun":    map[string]any{"content": text},
							},
						},
					},
				},
			},
		},
	}
}
