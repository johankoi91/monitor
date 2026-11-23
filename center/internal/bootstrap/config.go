// Package bootstrap owns first-start configuration, independently of Compose interpolation.
package bootstrap

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/center/internal/identity"
	b "github.com/johankoi91/monitor/runtime/shared/bootstrap"
	"github.com/johankoi91/monitor/runtime/shared/durable"
)

type Options struct {
	Private, Runtime, InitSQL, State, Postgres, Receiver, URL, SiteCode, SiteName, Cert, Key string
	Owner                                                                                    int
}

func FromEnvironment() Options {
	get := func(k, f string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return f
	}
	return Options{Private: get("AVOPS_PRIVATE_DIR", "/private"), Runtime: get("AVOPS_RUNTIME_CONFIG_DIR", "/config"), InitSQL: get("AVOPS_PG_INIT_DIR", "/docker-entrypoint-initdb.d"), State: get("AVOPS_DATA_DIR", "/state"), Postgres: get("PGDATA", "/postgres"), Receiver: get("AVOPS_RECEIVER_DATA_DIR", "/receiver"), URL: os.Getenv("AVOPS_PUBLIC_URL"), SiteCode: get("AVOPS_SITE_CODE", "customer"), SiteName: get("AVOPS_SITE_NAME", "客户监控"), Cert: get("AVOPS_TLS_SOURCE_CERT", "/input/server.pem"), Key: get("AVOPS_TLS_SOURCE_KEY", "/input/server-key.pem"), Owner: 0}
}
func random() string {
	var value [32]byte
	if _, e := rand.Read(value[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(value[:])
}
func (o Options) validate() (*url.URL, error) {
	u, e := url.Parse(o.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("AVOPS_PUBLIC_URL must be an HTTPS origin")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	number, e := strconv.Atoi(port)
	if e != nil || number < 1 || number > 65535 {
		return nil, errors.New("invalid center public port")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`).MatchString(o.SiteCode) || strings.TrimSpace(o.SiteName) == "" || strings.ContainsAny(o.SiteName, "\r\n") {
		return nil, errors.New("valid site code/name required")
	}
	for _, path := range []string{o.Private, o.Runtime, o.InitSQL, o.State, o.Postgres, o.Receiver} {
		if !filepath.IsAbs(path) {
			return nil, errors.New("absolute persistent paths required")
		}
	}
	pair, e := tls.LoadX509KeyPair(o.Cert, o.Key)
	if e != nil {
		return nil, errors.New("valid matching PEM certificate and key required")
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return nil, errors.New("invalid center certificate")
	}
	if e = cert.VerifyHostname(u.Hostname()); e != nil {
		return nil, errors.New("center certificate SAN does not match AVOPS_PUBLIC_URL")
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, errors.New("center certificate is outside its validity period")
	}
	return u, nil
}

var required = []string{"center.env", "center.yaml", "postgres.env", "owner.env", "database.env", "init.sql", "receiver.env", "agent.env", "probe-client.env", "restart-rules.yaml", "admin-curl.conf"}

func (o Options) files(u *url.URL) map[string]string {
	admin, agent, probe, receiver, webhook, owner, runtime, key := random(), random(), random(), random(), random(), random(), random(), random()
	password := identity.DefaultBootstrapPassword
	endpoint := func(path string) string { v := *u; v.Scheme = "wss"; v.Path = path; return v.String() }
	port := u.Port()
	if port == "" {
		port = "443"
	}
	dsn := func(user, secret string) string {
		return "postgres://" + user + ":" + secret + "@127.0.0.1:5432/avops_monitor?sslmode=disable"
	}
	return map[string]string{
		"center.env":         fmt.Sprintf("AVOPS_ADMIN_ID=system-admin\nAVOPS_ADMIN_SECRET=%s\nAVOPS_AGENT_ENROLL_TOKEN=%s\nAVOPS_PROBE_ENROLL_TOKEN=%s\nAVOPS_CENTER_ADDR=0.0.0.0:%s\nAVOPS_CENTER_LOCAL_ADDR=127.0.0.1:18084\nAVOPS_ALLOWED_IPS=\n", admin, agent, probe, port),
		"center.yaml":        fmt.Sprintf("site:\n  code: %s\n  name: %q\nrtc_probe:\n  max_concurrent_runs: 16\n", o.SiteCode, o.SiteName),
		"postgres.env":       fmt.Sprintf("POSTGRES_USER=avops_owner\nPOSTGRES_PASSWORD=%s\nPOSTGRES_DB=avops_monitor\nPOSTGRES_INITDB_ARGS=--auth-host=scram-sha-256\n", owner),
		"owner.env":          fmt.Sprintf("AVOPS_DATABASE_URL=%s\nAVOPS_STORAGE_KEY=%s\n", dsn("avops_owner", owner), key),
		"database.env":       fmt.Sprintf("AVOPS_DATABASE_URL=%s\nAVOPS_STORAGE_KEY=%s\nAVOPS_BOOTSTRAP_USERNAME=admin\nAVOPS_BOOTSTRAP_PASSWORD=%s\n", dsn("avops_runtime", runtime), key, password),
		"init.sql":           fmt.Sprintf("CREATE ROLE avops_runtime LOGIN PASSWORD '%s';\nCREATE SCHEMA IF NOT EXISTS avops AUTHORIZATION avops_owner;\nGRANT CONNECT ON DATABASE avops_monitor TO avops_runtime;\nGRANT USAGE ON SCHEMA avops TO avops_runtime;\nALTER DEFAULT PRIVILEGES FOR ROLE avops_owner IN SCHEMA avops GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO avops_runtime;\nALTER DEFAULT PRIVILEGES FOR ROLE avops_owner IN SCHEMA avops GRANT USAGE,SELECT ON SEQUENCES TO avops_runtime;\n", runtime),
		"agent.env":          fmt.Sprintf("AVOPS_CENTER_URL=%s\nAVOPS_AGENT_ENROLL_TOKEN=%s\nAVOPS_HOST_ADDRESS=\nAVOPS_COLLECT_SECONDS=15\n", endpoint("/api/v1/agent/connect"), agent),
		"probe-client.env":   fmt.Sprintf("AVOPS_PROBE_CENTER_URL=%s\nAVOPS_PROBE_ENROLL_TOKEN=%s\n", endpoint("/api/v1/probes/connect"), probe),
		"receiver.env":       fmt.Sprintf("AVOPS_RECEIVER_ID=monitor-admin\nAVOPS_RECEIVER_SECRET=%s\nAVOPS_WEBHOOK_SECRET=%s\nAVOPS_RECEIVER_ADDR=0.0.0.0:18443\n", receiver, webhook),
		"restart-rules.yaml": "[]\n", "admin-curl.conf": fmt.Sprintf("user = \"system-admin:%s\"\n", admin),
		"account-handoff.txt": fmt.Sprintf("控制台账号：admin\n初始密码：%s\n首次登录后请修改密码。\n", password),
	}
}
func (o Options) Initialize() error {
	u, e := o.validate()
	if e != nil {
		return e
	}
	if e = b.PrivateDirectory(o.Private, o.Owner); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(o.Private, ".bootstrap.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	count := 0
	for _, name := range required {
		info, e := os.Lstat(filepath.Join(o.Private, name))
		if e == nil {
			if !info.Mode().IsRegular() {
				return errors.New("private initialization files must be regular files")
			}
			count++
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if _, err := os.Stat(filepath.Join(o.Private, "initialization.json")); err == nil && count != len(required) {
		return errors.New("initialized site has missing files; restore original configuration")
	}
	if count == 0 {
		for _, path := range []string{o.State, o.Postgres, o.Receiver} {
			entries, e := os.ReadDir(path)
			if e == nil && len(entries) > 0 {
				return errors.New("existing data without matching private configuration; restore original secrets")
			}
			if e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		for name, value := range o.files(u) {
			target := filepath.Join(o.Private, name)
			if _, e = os.Lstat(target); !os.IsNotExist(e) {
				return errors.New("existing initialization material retained")
			}
			if e = durable.Bytes(target, []byte(value)); e != nil {
				return e
			}
		}
	} else if count != len(required) {
		return errors.New("partial initialization detected; original files retained, restore missing files before restart")
	}
	if e = o.validateExisting(); e != nil {
		return e
	}
	marker := filepath.Join(o.Private, "initialization.json")
	if _, e = os.Stat(marker); os.IsNotExist(e) {
		if e = durable.Write(marker, map[string]any{"version": 1, "created_at": time.Now().UTC(), "site_code": o.SiteCode}); e != nil {
			return e
		}
	} else if e != nil {
		return e
	} else {
		var value struct{ Version int }
		if e = durable.Read(marker, 8192, &value); e != nil || value.Version != 1 {
			return errors.New("invalid initialization marker")
		}
	}
	owner := 65532
	postgres := 999
	if o.Owner < 0 {
		owner = -1
		postgres = -1
	}
	for path, uid := range map[string]int{o.Runtime: owner, o.State: owner, o.Postgres: postgres, o.Receiver: owner, o.InitSQL: postgres} {
		if e = b.PrivateDirectory(path, uid); e != nil {
			return e
		}
	}
	for name, source := range map[string]string{"center.yaml": filepath.Join(o.Private, "center.yaml"), "server.pem": o.Cert, "server-key.pem": o.Key} {
		if e = b.CopyOwned(source, filepath.Join(o.Runtime, name), 0400, owner); e != nil {
			return e
		}
	}
	return b.CopyOwned(filepath.Join(o.Private, "init.sql"), filepath.Join(o.InitSQL, "00-roles.sql"), 0400, postgres)
}
func (o Options) validateExisting() error {
	envs := map[string]map[string]string{}
	for _, name := range []string{"center.env", "postgres.env", "owner.env", "database.env", "receiver.env", "agent.env", "probe-client.env"} {
		values, e := b.ReadEnv(filepath.Join(o.Private, name))
		if e != nil {
			return e
		}
		envs[name] = values
	}
	for _, key := range []string{"AVOPS_ADMIN_SECRET", "AVOPS_AGENT_ENROLL_TOKEN", "AVOPS_PROBE_ENROLL_TOKEN"} {
		if len(envs["center.env"][key]) < 24 {
			return errors.New("invalid persisted center credential")
		}
	}
	owner, runtime := envs["owner.env"], envs["database.env"]
	key, e := hex.DecodeString(owner["AVOPS_STORAGE_KEY"])
	if e != nil || len(key) != 32 || runtime["AVOPS_STORAGE_KEY"] != owner["AVOPS_STORAGE_KEY"] {
		return errors.New("persisted storage keys do not match")
	}
	for _, values := range []map[string]string{owner, runtime} {
		u, e := url.Parse(values["AVOPS_DATABASE_URL"])
		if e != nil || u.User == nil || u.Path != "/avops_monitor" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() != "5432" {
			return errors.New("persisted database configuration is not the embedded PostgreSQL")
		}
	}
	if envs["agent.env"]["AVOPS_AGENT_ENROLL_TOKEN"] != envs["center.env"]["AVOPS_AGENT_ENROLL_TOKEN"] || envs["probe-client.env"]["AVOPS_PROBE_ENROLL_TOKEN"] != envs["center.env"]["AVOPS_PROBE_ENROLL_TOKEN"] {
		return errors.New("persisted client enrollment credentials do not match center")
	}
	return nil
}
func Environment(private, runtime string, base []string) ([]string, error) {
	center, e := b.ReadEnv(filepath.Join(private, "center.env"))
	if e != nil {
		return nil, e
	}
	database, e := b.ReadEnv(filepath.Join(private, "database.env"))
	if e != nil {
		return nil, e
	}
	overrides := b.Overrides(base, "AVOPS_CENTER_LOCAL_ADDR", "AVOPS_ALLOWED_IPS", "AVOPS_DATABASE_WARNING_BYTES")
	if public := b.Map(base)["AVOPS_PUBLIC_URL"]; public != "" {
		u, e := url.Parse(public)
		if e != nil {
			return nil, errors.New("invalid public URL")
		}
		port := u.Port()
		if port == "" {
			port = "443"
		}
		overrides["AVOPS_CENTER_ADDR"] = "0.0.0.0:" + port
	}
	overrides["AVOPS_CENTER_CONFIG"] = filepath.Join(runtime, "center.yaml")
	overrides["AVOPS_TLS_CERT"] = filepath.Join(runtime, "server.pem")
	overrides["AVOPS_TLS_KEY"] = filepath.Join(runtime, "server-key.pem")
	return b.Merge(base, center, database, overrides), nil
}
