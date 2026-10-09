// pgverify verifies the live PostgreSQL center without printing credentials.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func envFile(path string) map[string]string {
	b, e := os.ReadFile(path)
	if e != nil {
		panic("private verification material unavailable")
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			m[k] = strings.Trim(v, "\"'")
		}
	}
	return m
}
func main() {
	admin := envFile("/etc/avops-monitor/center.env")
	account := envFile("/etc/avops-monitor/database.env")
	client := http.Client{Timeout: 5 * time.Second}
	token := ""
	call := func(method, path string, body any, basic bool, target any) int {
		var data io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			data = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, "http://127.0.0.1:18084"+path, data)
		r.Header.Set("Content-Type", "application/json")
		if basic {
			r.SetBasicAuth(admin["AVOPS_ADMIN_ID"], admin["AVOPS_ADMIN_SECRET"])
		} else if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := client.Do(r)
		if e != nil {
			panic("center unavailable")
		}
		defer resp.Body.Close()
		if target != nil {
			if e = json.NewDecoder(resp.Body).Decode(target); e != nil {
				panic("invalid center response")
			}
		}
		return resp.StatusCode
	}
	proof := map[string]any{}
	var version struct {
		Accounts bool `json:"accounts_enabled"`
	}
	if call("GET", "/version", nil, true, &version) != 200 || !version.Accounts {
		panic("account capability unavailable")
	}
	proof["accounts_enabled"] = true
	var login struct {
		Token string `json:"token"`
		User  struct {
			Username    string   `json:"username"`
			Permissions []string `json:"permissions"`
		} `json:"user"`
	}
	if call("POST", "/api/v1/auth/login", map[string]string{"username": account["AVOPS_BOOTSTRAP_USERNAME"], "password": account["AVOPS_BOOTSTRAP_PASSWORD"]}, false, &login) != 200 || login.Token == "" {
		panic("bootstrap account login failed")
	}
	token = login.Token
	proof["account_login"] = true
	proof["username"] = login.User.Username
	proof["permission_count"] = len(login.User.Permissions)
	var roles struct {
		Roles []any `json:"roles"`
	}
	if call("GET", "/api/v1/roles", nil, false, &roles) != 200 || len(roles.Roles) != 2 {
		panic("RBAC roles unavailable")
	}
	proof["role_count"] = len(roles.Roles)
	var baseline struct {
		Revision   string `json:"revision"`
		Definition struct {
			Clusters []struct {
				Services []struct {
					Nodes []any `json:"nodes"`
				} `json:"services"`
			} `json:"clusters"`
		} `json:"definition"`
	}
	if call("GET", "/api/v1/baseline", nil, false, &baseline) != 200 {
		panic("baseline unavailable")
	}
	count := 0
	for _, c := range baseline.Definition.Clusters {
		for _, s := range c.Services {
			count += len(s.Nodes)
		}
	}
	proof["baseline_revision"] = baseline.Revision
	proof["node_count"] = count
	var ops struct {
		Total int `json:"total"`
	}
	if call("GET", "/api/v1/operations?limit=50", nil, false, &ops) != 200 {
		panic("operations unavailable")
	}
	proof["operation_count"] = ops.Total
	var keys struct {
		Keys []any `json:"keys"`
	}
	if call("GET", "/api/v1/access-keys", nil, false, &keys) != 200 {
		panic("keys unavailable")
	}
	proof["key_count"] = len(keys.Keys)
	var notifications map[string]any
	if call("GET", "/api/v1/notifications/status", nil, false, &notifications) != 200 {
		panic("notifications unavailable")
	}
	proof["notification_storage_available"] = notifications["storage_available"]
	if call("POST", "/api/v1/auth/logout", map[string]string{}, false, nil) != 200 {
		panic("logout failed")
	}
	if call("GET", "/version", nil, false, nil) != 401 {
		panic("revoked session retained access")
	}
	proof["logout_revokes_session"] = true
	b, _ := json.MarshalIndent(proof, "", "  ")
	if os.WriteFile("/var/backups/avops-center-pg-20261007/verification.json", b, 0600) != nil {
		panic("proof unavailable")
	}
	fmt.Println(string(b))
}
