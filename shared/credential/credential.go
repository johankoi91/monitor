// Package credential defines the shared machine-credential vocabulary.
// The protocol is common; secrets and scopes remain isolated by kind.
package credential

type Kind string

const (
	KindAgent                Kind = "AGENT"
	KindNotificationReceiver Kind = "NOTIFICATION_RECEIVER"
	KindRestart              Kind = "RESTART"
)
