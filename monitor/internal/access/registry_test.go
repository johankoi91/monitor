package access

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectIssueAndRevocationRecover(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := r.Issue(IssueRequest{Name: "direct restart", Purpose: "RESTART", AllowedNodes: []string{"a/canary"}, ApprovalReason: "isolated test approval"}, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	key, ok := r.Authenticate(issued.Key.ID, issued.Secret)
	if !ok || !r.AllowsNode(key, "a/canary") || r.AllowsNode(key, "a/ap") {
		t.Fatal("key node restriction failed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "access-keys.jsonl"))
	if strings.Contains(string(data), issued.Secret) {
		t.Fatal("plaintext secret persisted")
	}
	r.Close()
	r, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, ok = r.Authenticate(issued.Key.ID, issued.Secret); !ok {
		t.Fatal("issued key not recovered")
	}
	if err = r.Revoke(key.ID, "bootstrap"); err != nil {
		t.Fatal(err)
	}
	if _, ok = r.Authenticate(issued.Key.ID, issued.Secret); ok {
		t.Fatal("revoked key accepted")
	}
}
func TestOnlyRestartKeysCanBeIssued(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.Issue(IssueRequest{Name: "console", Purpose: "CONSOLE", ApprovalReason: "test"}, "bootstrap"); err == nil {
		t.Fatal("console key was still issued")
	}
	if _, err = r.Issue(IssueRequest{Name: "restart", Purpose: "RESTART", AllowedNodes: []string{"a/canary"}, ApprovalReason: "test"}, "bootstrap"); err != nil {
		t.Fatal(err)
	}
}
