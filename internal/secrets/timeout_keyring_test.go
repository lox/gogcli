package secrets

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestKeyringOperationTimeoutGuards(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		backend  string
		dbusAddr string
		wantWrap bool
	}{
		{name: "darwin auto", goos: "darwin", backend: "auto", wantWrap: true},
		{name: "darwin keychain", goos: "darwin", backend: "keychain", wantWrap: true},
		{name: "darwin file", goos: "darwin", backend: "file", wantWrap: false},
		{name: "linux auto with dbus", goos: "linux", backend: "auto", dbusAddr: "unix:path=/run/user/1000/bus", wantWrap: true},
		{name: "linux auto without dbus", goos: "linux", backend: "auto", wantWrap: false},
		{name: "linux keychain", goos: "linux", backend: "keychain", dbusAddr: "unix:path=/run/user/1000/bus", wantWrap: false},
		{name: "windows auto", goos: "windows", backend: "auto", wantWrap: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldUseKeyringOperationTimeout(tt.goos, KeyringBackendInfo{Value: tt.backend}, tt.dbusAddr)
			if got != tt.wantWrap {
				t.Fatalf("shouldUseKeyringOperationTimeout=%v, want %v", got, tt.wantWrap)
			}
		})
	}
}

func TestKeyringTimeoutHint(t *testing.T) {
	tests := []struct {
		goos       string
		wantSubstr string
	}{
		{"darwin", "Always Allow"},
		{"linux", "D-Bus SecretService"},
		{"windows", "keyring backend"},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			hint := keyringTimeoutHint(tt.goos)
			if !strings.Contains(hint, tt.wantSubstr) {
				t.Fatalf("keyringTimeoutHint(%q)=%q, want substring %q", tt.goos, hint, tt.wantSubstr)
			}
		})
	}
}

func TestIsKeyringTimeoutRecognizesOperationDeadline(t *testing.T) {
	err := fmt.Errorf("list tokens: keyring list keys timed out after 10ms: %w", context.DeadlineExceeded)
	if !IsKeyringTimeout(err) {
		t.Fatalf("expected keyring timeout, got %v", err)
	}
}
