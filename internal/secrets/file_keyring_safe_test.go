package secrets

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/lox/keyring/v2"

	"github.com/steipete/gogcli/internal/config"
)

var errLegacyRemoveFailed = errors.New("legacy remove failed")

func TestLegacyFileSafeKeyRoundTrip(t *testing.T) {
	keys := []string{
		"token:default:user@example.com",
		`token:work:user@example.com`,
		`tracking/user@example.com/tracking_key`,
		`<>:"/\|?*%`,
		"",
	}

	for _, key := range keys {
		encoded := legacyFileSafeKey(key)
		if encoded == key {
			t.Fatalf("expected encoded key for %q", key)
		}

		if strings.ContainsAny(encoded, `<>:"/\|?*`) {
			t.Fatalf("encoded key %q still contains a Windows filename separator/reserved char", encoded)
		}

		if got := decodeLegacyFileSafeKey(encoded); got != key {
			t.Fatalf("decodeLegacyFileSafeKey(%q)=%q, want %q", encoded, got, key)
		}
	}

	rawPrefixKey := fileKeyPrefix + "dGVzdA"
	if got := decodeLegacyFileSafeKey(rawPrefixKey); got != "test" {
		t.Fatalf("expected canonical encoded key to decode, got %q", got)
	}

	if got := decodeLegacyFileSafeKey(fileKeyPrefix + "not valid"); got != fileKeyPrefix+"not valid" {
		t.Fatalf("expected invalid encoded key to remain raw, got %q", got)
	}
}

func TestLegacyFileKeyringReadsAndRemovesGogEncodedKeys(t *testing.T) {
	ctx := context.Background()
	inner := keyring.NewArrayKeyring(nil)
	ring := newLegacyFileKeyring(inner)
	key := "token:default:user@example.com"

	if err := inner.Set(ctx, keyring.Item{Key: legacyFileSafeKey(key), Data: []byte("legacy")}); err != nil {
		t.Fatalf("set legacy key: %v", err)
	}

	item, err := ring.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get legacy key: %v", err)
	}

	if item.Key != key || string(item.Data) != "legacy" {
		t.Fatalf("unexpected legacy item: key=%q data=%q", item.Key, item.Data)
	}

	if setErr := ring.Set(ctx, keyring.Item{Key: key, Data: []byte("new")}); setErr != nil {
		t.Fatalf("Set new key: %v", setErr)
	}

	keys, err := ring.Keys(ctx)
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}

	got := 0

	for _, listedKey := range keys {
		if listedKey == key {
			got++
		}
	}

	if got != 1 {
		t.Fatalf("expected one decoded key in %v, got count %d", keys, got)
	}

	if err := ring.Remove(ctx, key); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := inner.Get(ctx, key); !errors.Is(err, keyring.ErrKeyNotFound) {
		t.Fatalf("expected new key removed, got %v", err)
	}

	if _, err := inner.Get(ctx, legacyFileSafeKey(key)); !errors.Is(err, keyring.ErrKeyNotFound) {
		t.Fatalf("expected legacy key removed, got %v", err)
	}
}

func TestLegacyFileKeyringRemoveReportsLegacyDeleteError(t *testing.T) {
	ctx := context.Background()
	key := "token:default:user@example.com"
	ring := newLegacyFileKeyring(&legacyRemoveErrorKeyring{
		key: key,
		items: map[string]keyring.Item{
			legacyFileSafeKey(key): {Key: legacyFileSafeKey(key), Data: []byte("legacy")},
			key:                    {Key: key, Data: []byte("new")},
		},
		err: errLegacyRemoveFailed,
	})

	err := ring.Remove(ctx, key)
	if !errors.Is(err, errLegacyRemoveFailed) {
		t.Fatalf("expected legacy remove error, got %v", err)
	}

	item, getErr := ring.Get(ctx, key)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}

	if string(item.Data) != "legacy" {
		t.Fatalf("expected legacy item still readable, got %q", string(item.Data))
	}
}

func TestOpenKeyringWrapsExplicitFileBackend(t *testing.T) {
	layout := config.Layout{
		ConfigDir: t.TempDir(),
		DataDir:   t.TempDir(),
	}
	options := OpenOptions{
		Layout:      layout,
		Config:      config.NewConfigStore(layout),
		Backend:     "file",
		Password:    "test-pass",
		PasswordSet: true,
		GOOS:        runtime.GOOS,
		openKeyringFn: func(context.Context, ...keyring.Option) (keyring.Keyring, error) {
			return keyring.NewArrayKeyring(nil), nil
		},
	}

	store, err := Open(options)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	keyringStore, ok := store.(*KeyringStore)
	if !ok {
		t.Fatalf("expected *KeyringStore, got %T", store)
	}

	if _, ok := keyringStore.ring.(*legacyFileKeyring); !ok {
		t.Fatalf("expected legacy file keyring, got %T", keyringStore.ring)
	}
}

type legacyRemoveErrorKeyring struct {
	key   string
	items map[string]keyring.Item
	err   error
}

func (k *legacyRemoveErrorKeyring) Get(_ context.Context, key string) (keyring.Item, error) {
	item, ok := k.items[key]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}

	return item, nil
}

func (k *legacyRemoveErrorKeyring) Metadata(_ context.Context, key string) (keyring.Metadata, error) {
	if _, ok := k.items[key]; !ok {
		return keyring.Metadata{}, keyring.ErrKeyNotFound
	}

	return keyring.Metadata{}, nil
}

func (k *legacyRemoveErrorKeyring) Set(_ context.Context, item keyring.Item) error {
	k.items[item.Key] = item
	return nil
}

func (k *legacyRemoveErrorKeyring) Remove(_ context.Context, key string) error {
	if key == legacyFileSafeKey(k.key) {
		return k.err
	}

	if _, ok := k.items[key]; !ok {
		return keyring.ErrKeyNotFound
	}

	delete(k.items, key)

	return nil
}

func (k *legacyRemoveErrorKeyring) Keys(context.Context) ([]string, error) {
	keys := make([]string, 0, len(k.items))
	for key := range k.items {
		keys = append(keys, key)
	}

	return keys, nil
}
