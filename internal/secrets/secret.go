package secrets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/99designs/keyring"
)

var errMissingSecretKey = errors.New("missing secret key")

func SetSecret(key string, value []byte) error {
	store, err := openDefaultRepository()
	if err != nil {
		return err
	}

	if err := store.SetSecret(key, value); err != nil {
		return fmt.Errorf("set secret: %w", err)
	}

	return nil
}

func (s *KeyringStore) SetSecret(key string, value []byte) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errMissingSecretKey
	}

	if err := s.withWriteLock(func() error {
		return verifiedSet(s.ring, key, value, "secret")
	}); err != nil {
		return wrapKeychainError(fmt.Errorf("store secret: %w", err))
	}

	return nil
}

func GetSecret(key string) ([]byte, error) {
	store, err := openDefaultRepository()
	if err != nil {
		return nil, err
	}

	value, err := store.GetSecret(key)
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}

	return value, nil
}

func (s *KeyringStore) GetSecret(key string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errMissingSecretKey
	}

	var item keyring.Item

	if err := s.withReadLock(func() error {
		var getErr error

		item, getErr = s.ring.Get(key)
		if getErr != nil {
			return fmt.Errorf("get secret: %w", getErr)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("read secret: %w", err)
	}

	return item.Data, nil
}

func (s *KeyringStore) DeleteSecret(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errMissingSecretKey
	}

	if err := s.withWriteLock(func() error {
		if removeErr := s.ring.Remove(key); removeErr != nil {
			return fmt.Errorf("delete secret: %w", removeErr)
		}

		return nil
	}); err != nil {
		return wrapKeychainError(fmt.Errorf("delete secret: %w", err))
	}

	return nil
}
