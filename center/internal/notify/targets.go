package notify

import (
	"context"
	"net/http"
	"time"
)

// Check endpoints concurrently so adding dedicated routes does not multiply
// the HTTP request's response budget. Every endpoint must acknowledge.
func probeTargets(targets []Config, factory func(Config) *http.Client, body []byte) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := make(chan string, len(targets))
	for _, target := range targets {
		go func(c Config) {
			client := factory(c)
			defer client.CloseIdleConnections()
			ok, _, reason := postWebhook(ctx, client, c, body)
			if ok {
				reason = ""
			}
			results <- reason
		}(target)
	}
	for range targets {
		if reason := <-results; reason != "" {
			return reason
		}
	}
	return ""
}
