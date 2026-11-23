package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/protocol"
)

func (c *Client) enroll(ctx context.Context) error {
	u, err := url.Parse(c.config.URL)
	if err != nil {
		return err
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = "/api/v1/probes/enroll"
	host := c.host.Host
	if c.config.HostName != "" {
		host.Name = c.config.HostName
	}
	if c.config.HostAddress != "" {
		host.Address = c.config.HostAddress
	}
	if c.config.Location != "" {
		host.Location = c.config.Location
	}
	input := protocol.ProbeEnrollment{Host: host, HostSecret: c.host.Secret, RuntimeID: c.identity.ID, RuntimeSecret: c.identity.Secret, Name: c.config.Name, ServiceType: c.config.ServiceType, ClientType: c.config.ClientType, Driver: c.config.Driver}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.SetBasicAuth("probe-enroll", c.config.EnrollmentToken)
	request.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("probe server announcement unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("probe server announcement rejected")
	}
	var reply protocol.ProbeEnrollmentReply
	if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&reply) != nil || reply.HostID != c.host.Host.ID || reply.RuntimeID != c.identity.ID {
		return errors.New("invalid probe server acknowledgement")
	}
	return nil
}
