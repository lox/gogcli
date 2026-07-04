package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lox/keyring/v2"
)

const fileKeyPrefix = "_gogcli_key_v1_"

type legacyFileKeyring struct {
	inner keyring.Keyring
}

func newLegacyFileKeyring(inner keyring.Keyring) keyring.Keyring {
	return &legacyFileKeyring{inner: inner}
}

func fileKeyringBackendOnly(backends []keyring.Backend) bool {
	return len(backends) == 1 && backends[0] == keyring.FileBackend
}

func legacyFileSafeKey(key string) string {
	return fileKeyPrefix + base64.RawURLEncoding.EncodeToString([]byte(key))
}

func decodeLegacyFileSafeKey(key string) string {
	encoded, ok := strings.CutPrefix(key, fileKeyPrefix)
	if !ok {
		return key
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return key
	}

	decoded := string(raw)
	if legacyFileSafeKey(decoded) != key {
		return key
	}

	return decoded
}

func (k *legacyFileKeyring) Get(ctx context.Context, key string) (keyring.Item, error) {
	item, err := k.inner.Get(ctx, key)
	if err == nil {
		return item, nil
	}

	if !errors.Is(err, keyring.ErrKeyNotFound) {
		return keyring.Item{}, fmt.Errorf("read file keyring item: %w", err)
	}

	item, legacyErr := k.inner.Get(ctx, legacyFileSafeKey(key))
	if legacyErr != nil {
		if errors.Is(legacyErr, keyring.ErrKeyNotFound) {
			return keyring.Item{}, keyring.ErrKeyNotFound
		}

		return keyring.Item{}, fmt.Errorf("read legacy file keyring item: %w", legacyErr)
	}

	item.Key = key

	return item, nil
}

func (k *legacyFileKeyring) Metadata(ctx context.Context, key string) (keyring.Metadata, error) {
	reader, ok := k.inner.(keyring.MetadataReader)
	if !ok {
		return keyring.Metadata{}, keyring.ErrMetadataNotSupported
	}

	meta, err := reader.Metadata(ctx, key)
	if err == nil {
		return meta, nil
	}

	if !errors.Is(err, keyring.ErrKeyNotFound) {
		return keyring.Metadata{}, fmt.Errorf("read file keyring metadata: %w", err)
	}

	meta, legacyErr := reader.Metadata(ctx, legacyFileSafeKey(key))
	if legacyErr != nil {
		if errors.Is(legacyErr, keyring.ErrKeyNotFound) {
			return keyring.Metadata{}, keyring.ErrKeyNotFound
		}

		return keyring.Metadata{}, fmt.Errorf("read legacy file keyring metadata: %w", legacyErr)
	}

	if meta.Item != nil {
		meta.Key = key
	}

	return meta, nil
}

func (k *legacyFileKeyring) Set(ctx context.Context, item keyring.Item) error {
	if err := k.inner.Set(ctx, item); err != nil {
		return fmt.Errorf("store file keyring item: %w", err)
	}

	return nil
}

func (k *legacyFileKeyring) Remove(ctx context.Context, key string) error {
	err := k.inner.Remove(ctx, key)
	legacyErr := k.inner.Remove(ctx, legacyFileSafeKey(key))

	if (err == nil || errors.Is(err, keyring.ErrKeyNotFound)) &&
		(legacyErr == nil || errors.Is(legacyErr, keyring.ErrKeyNotFound)) {
		if errors.Is(err, keyring.ErrKeyNotFound) && errors.Is(legacyErr, keyring.ErrKeyNotFound) {
			return keyring.ErrKeyNotFound
		}

		return nil
	}

	if err != nil && !errors.Is(err, keyring.ErrKeyNotFound) {
		return fmt.Errorf("remove file keyring item: %w", err)
	}

	return fmt.Errorf("remove legacy file keyring item: %w", legacyErr)
}

func (k *legacyFileKeyring) Keys(ctx context.Context) ([]string, error) {
	keys, err := k.inner.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list file keyring keys: %w", err)
	}

	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))

	for _, key := range keys {
		decoded := decodeLegacyFileSafeKey(key)
		if _, ok := seen[decoded]; ok {
			continue
		}
		seen[decoded] = struct{}{}
		out = append(out, decoded)
	}

	sort.Strings(out)

	return out, nil
}
