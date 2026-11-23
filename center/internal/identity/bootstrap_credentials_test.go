package identity

import (
	"strings"
	"testing"
)

func TestDefaultInitialPasswordExceptionIsLimitedToBootstrapAdmin(t *testing.T) {
	for _, test := range []struct {
		username, password string
		valid              bool
	}{
		{"admin", "1111", true}, {"other", "1111", false}, {"admin", "1234", false},
		{"admin", "long-enough-password", true}, {"bad username", "1111", false},
		{"admin", strings.Repeat("x", 73), false},
	} {
		if got := validBootstrapCredentials(test.username, test.password); got != test.valid {
			t.Errorf("bootstrap credential policy mismatch for %q", test.username)
		}
	}
}
