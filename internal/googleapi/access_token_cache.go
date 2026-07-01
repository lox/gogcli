package googleapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const accessTokenCacheMinTTL = 5 * time.Minute

type accessTokenCacheEntry struct {
	AccessToken string    `json:"access_token"`
	Expiry      time.Time `json:"expiry"`
	Scopes      []string  `json:"scopes,omitempty"`
}

type accessTokenCachingSource struct {
	base   oauth2.TokenSource
	dir    string
	client string
	email  string
	scopes []string
}

func cachedAccessTokenSource(dir, client, email string, scopes []string, now time.Time) (oauth2.TokenSource, bool) {
	entry, err := readAccessTokenCache(dir, client, email, scopes)
	if err != nil {
		return nil, false
	}

	if !entry.valid(scopes, now) {
		return nil, false
	}

	return oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: entry.AccessToken,
		Expiry:      entry.Expiry,
	}), true
}

func newAccessTokenCachingSource(base oauth2.TokenSource, dir, client, email string, scopes []string) oauth2.TokenSource {
	if strings.TrimSpace(dir) == "" {
		return base
	}

	return &accessTokenCachingSource{
		base:   base,
		dir:    dir,
		client: client,
		email:  email,
		scopes: normalizeScopeList(scopes),
	}
}

func (s *accessTokenCachingSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, fmt.Errorf("get token from base source: %w", err)
	}

	if err := writeAccessTokenCache(s.dir, s.client, s.email, s.scopes, tok); err != nil {
		slog.Debug("write access token cache failed", "err", err)
	}

	return tok, nil
}

func readAccessTokenCache(dir, client, email string, scopes []string) (accessTokenCacheEntry, error) {
	if strings.TrimSpace(dir) == "" {
		return accessTokenCacheEntry{}, errAccessTokenCacheDisabled
	}

	data, err := os.ReadFile(accessTokenCachePath(dir, client, email, scopes))
	if err != nil {
		return accessTokenCacheEntry{}, fmt.Errorf("read access token cache: %w", err)
	}

	var entry accessTokenCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return accessTokenCacheEntry{}, fmt.Errorf("decode access token cache: %w", err)
	}

	return entry, nil
}

func writeAccessTokenCache(dir, client, email string, scopes []string, tok *oauth2.Token) error {
	if strings.TrimSpace(dir) == "" || tok == nil {
		return errAccessTokenCacheDisabled
	}

	entry := accessTokenCacheEntry{
		AccessToken: strings.TrimSpace(tok.AccessToken),
		Expiry:      tok.Expiry,
		Scopes:      normalizeScopeList(scopes),
	}
	if !entry.valid(scopes, time.Now()) {
		return errAccessTokenCacheInvalid
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure access token cache dir: %w", err)
	}

	data, err := json.Marshal(entry) //nolint:gosec // cache intentionally stores only short-lived Google access tokens
	if err != nil {
		return fmt.Errorf("encode access token cache: %w", err)
	}

	data = append(data, '\n')

	path := accessTokenCachePath(dir, client, email, scopes)

	tmp, err := os.CreateTemp(dir, ".access-token-*.tmp")
	if err != nil {
		return fmt.Errorf("create access token cache temp file: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	if chmodErr := tmp.Chmod(0o600); chmodErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod access token cache temp file: %w", chmodErr)
	}

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return fmt.Errorf("write access token cache temp file: %w", writeErr)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("close access token cache temp file: %w", closeErr)
	}

	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		return fmt.Errorf("replace access token cache file: %w", renameErr)
	}

	return nil
}

func accessTokenCachePath(dir, client, email string, scopes []string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(client),
		strings.ToLower(strings.TrimSpace(email)),
		strings.Join(normalizeScopeList(scopes), "\n"),
	}, "\x00")))

	return filepath.Join(dir, fmt.Sprintf("%x.json", sum))
}

func (e accessTokenCacheEntry) valid(scopes []string, now time.Time) bool {
	if strings.TrimSpace(e.AccessToken) == "" || e.Expiry.IsZero() {
		return false
	}

	if !e.Expiry.After(now.Add(accessTokenCacheMinTTL)) {
		return false
	}

	return stringSlicesEqual(normalizeScopeList(e.Scopes), normalizeScopeList(scopes))
}

var (
	errAccessTokenCacheDisabled = errors.New("access token cache disabled")
	errAccessTokenCacheInvalid  = errors.New("access token cache invalid")
)
