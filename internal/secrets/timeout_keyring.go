package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func keyringTimeoutError(operation string, timeout time.Duration, hint string) error {
	return fmt.Errorf("%w after %v while %s (%s); "+
		"set %s to allow more time, or set GOG_KEYRING_BACKEND=file and GOG_KEYRING_PASSWORD=<password> to use encrypted file storage instead",
		errKeyringTimeout, timeout, operation, hint, keyringOpenTimeoutEnv)
}

func IsKeyringTimeout(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, errKeyringTimeout) ||
		errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(err.Error(), errKeyringTimeout.Error())
}
