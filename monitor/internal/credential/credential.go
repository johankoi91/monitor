// Package credential defines the shared machine-credential vocabulary.
// The protocol is common; secrets and scopes remain isolated by kind.
package credential

import (
	"errors"
	"strings"
)

type Kind string

const (
	KindAgent                Kind = "AGENT"
	KindNotificationReceiver Kind = "NOTIFICATION_RECEIVER"
	KindRestart              Kind = "RESTART"
)

func ValidateBasic(kind Kind, id, secret string) error {
	if kind == "" || strings.TrimSpace(id) == "" || strings.Contains(id, ":") || secret == "" {
		return errors.New("invalid credential")
	}
	return nil
}
