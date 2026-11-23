package bootstrap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	b "github.com/johankoi91/monitor/runtime/shared/bootstrap"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "bootstrap-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	private, _ := x509.MarshalPKCS8PrivateKey(key)
	certPath := filepath.Join(root, "server.pem")
	keyPath := filepath.Join(root, "server-key.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600)
	return Options{Private: filepath.Join(root, "private"), Runtime: filepath.Join(root, "runtime"), InitSQL: filepath.Join(root, "init"), State: filepath.Join(root, "state"), Postgres: filepath.Join(root, "postgres"), Receiver: filepath.Join(root, "receiver"), URL: "https://127.0.0.1:29443", SiteCode: "test", SiteName: "测试站点", Cert: certPath, Key: keyPath, Owner: -1}
}
func TestFirstStartAndRestartKeepAllSecrets(t *testing.T) {
	o := fixture(t)
	if e := o.Initialize(); e != nil {
		t.Fatal(e)
	}
	before := map[string]string{}
	for _, name := range required {
		body, e := os.ReadFile(filepath.Join(o.Private, name))
		if e != nil {
			t.Fatal(e)
		}
		before[name] = string(body)
	}
	if e := o.Initialize(); e != nil {
		t.Fatal(e)
	}
	for name, expected := range before {
		body, _ := os.ReadFile(filepath.Join(o.Private, name))
		if string(body) != expected {
			t.Fatal("restart changed initialization material", name)
		}
	}
	center, _ := b.ReadEnv(filepath.Join(o.Private, "center.env"))
	database, _ := b.ReadEnv(filepath.Join(o.Private, "database.env"))
	if database["AVOPS_BOOTSTRAP_USERNAME"] != "admin" || database["AVOPS_BOOTSTRAP_PASSWORD"] != "1111" {
		t.Fatal("default initial account is not admin/1111")
	}
	handoff, _ := os.ReadFile(filepath.Join(o.Private, "account-handoff.txt"))
	if !strings.Contains(string(handoff), "初始密码：1111") {
		t.Fatal("initial account file disagrees with bootstrap password")
	}
	if center["AVOPS_CENTER_ADDR"] != "0.0.0.0:29443" {
		t.Fatal("public URL port and listener diverged")
	}
	env, e := Environment(o.Private, o.Runtime, []string{"PATH=/usr/bin", "AVOPS_PUBLIC_URL=" + o.URL})
	if e != nil {
		t.Fatal(e)
	}
	values := b.Map(env)
	owner, _ := b.ReadEnv(filepath.Join(o.Private, "owner.env"))
	if values["AVOPS_DATABASE_URL"] == owner["AVOPS_DATABASE_URL"] {
		t.Fatal("owner DSN leaked to center process")
	}
}
func TestPartialOrMissingConfigNeverRegeneratesSecrets(t *testing.T) {
	o := fixture(t)
	if e := o.Initialize(); e != nil {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(o.Private, "database.env"))
	before, _ := os.ReadFile(filepath.Join(o.Private, "owner.env"))
	if e := o.Initialize(); e == nil {
		t.Fatal("partial initialization accepted")
	}
	after, _ := os.ReadFile(filepath.Join(o.Private, "owner.env"))
	if string(before) != string(after) {
		t.Fatal("partial initialization replaced original key")
	}
}
func TestDataWithoutPrivateMaterialIsRejected(t *testing.T) {
	o := fixture(t)
	os.Mkdir(o.Postgres, 0700)
	os.WriteFile(filepath.Join(o.Postgres, "PG_VERSION"), []byte("17"), 0600)
	if e := o.Initialize(); e == nil {
		t.Fatal("existing database accepted new unrelated passwords")
	}
	if _, e := os.Stat(filepath.Join(o.Private, "owner.env")); !os.IsNotExist(e) {
		t.Fatal("new secret created for existing database")
	}
}
func TestCertificateMismatchFailsBeforeCreatingConfig(t *testing.T) {
	o := fixture(t)
	o.URL = "https://192.0.2.1:19443"
	if e := o.Initialize(); e == nil {
		t.Fatal("certificate mismatch accepted")
	}
	if _, e := os.Stat(o.Private); !os.IsNotExist(e) {
		t.Fatal("invalid certificate created config")
	}
}
