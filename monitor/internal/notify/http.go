package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func webhookClient(cfg Config) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.TLSServerName}, MaxIdleConnsPerHost: 100, IdleConnTimeout: 60 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func postWebhook(ctx context.Context, client *http.Client, cfg Config, body []byte) (bool, int, string) {
	request, err := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(body))
	if err != nil {
		return false, 0, "INVALID_TARGET"
	}
	request.Header.Set("Content-Type", "application/json")
	v1, v2 := signatures(cfg.Secret, body)
	request.Header.Set("Agora-Signature", v1)
	request.Header.Set("Agora-Signature-V2", v2)
	response, err := client.Do(request)
	if err != nil {
		return false, 0, "NETWORK_OR_TLS_ERROR"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return false, response.StatusCode, "RECEIVER_NON_200"
	}
	ack, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	if readErr != nil || len(ack) > 4096 || !json.Valid(ack) {
		return false, response.StatusCode, "RECEIVER_INVALID_JSON"
	}
	return true, response.StatusCode, ""
}
