package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

type Identity struct {
	ID     string `json:"agent_id"`
	Secret string `json:"agent_secret"`
}

func LoadIdentity(dir string) (Identity, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Identity{}, err
	}
	file := filepath.Join(dir, "agent-identity.json")
	if data, err := os.ReadFile(file); err == nil {
		var v Identity
		if json.Unmarshal(data, &v) != nil || v.ID == "" || len(v.Secret) < 24 {
			return v, errors.New("invalid persisted Agent identity")
		}
		return v, nil
	} else if !os.IsNotExist(err) {
		return Identity{}, err
	}
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return Identity{}, err
	}
	v := Identity{ID: "avops-" + hex.EncodeToString(raw[:16]), Secret: hex.EncodeToString(raw[16:])}
	data, _ := json.Marshal(v)
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Identity{}, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Identity{}, err
	}
	d, err := os.Open(dir)
	if err != nil {
		return Identity{}, err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return Identity{}, err
	}
	return v, nil
}
func Enroll(ctx context.Context, endpoint, token string, identity Identity, host model.HostInfo, tlsConfig *tls.Config) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	u.Path = "/api/v1/agent/enroll"
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	data, _ := json.Marshal(map[string]any{"agent_id": identity.ID, "agent_secret": identity.Secret, "host_info": host})
	request, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.SetBasicAuth("agent-enroll", token)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("Agent enrollment unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("Agent enrollment rejected")
	}
	var result struct {
		ID              string `json:"agent_id"`
		ObservedAddress string `json:"observed_address"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result) != nil || result.ID != identity.ID {
		return "", errors.New("invalid enrollment response")
	}
	return result.ObservedAddress, nil
}
