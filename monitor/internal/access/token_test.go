package access

import (
	"testing"
	"time"
)

func TestTokenExpiryRevocationAndRecovery(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := r.Issue(IssueRequest{Name: "api", Purpose: "RESTART", OwnerID: "owner", ExpiresHours: 2}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if k, ok := r.AuthenticateToken(issued.Token); !ok || k.OwnerID != "owner" || k.ExpiresAt == nil {
		t.Fatal("owner-bound expiring token rejected")
	}
	r.Close()
	r, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, ok := r.AuthenticateToken(issued.Token); !ok {
		t.Fatal("token did not survive recovery")
	}
	for _, bad := range []string{issued.Secret, "rt_", issued.Token + "x", "Basic " + issued.Token} {
		if _, ok := r.AuthenticateToken(bad); ok {
			t.Fatal("malformed token accepted")
		}
	}
	k := r.keys[issued.Key.ID]
	past := time.Now().Add(-time.Second)
	k.ExpiresAt = &past
	r.keys[k.ID] = k
	if _, ok := r.AuthenticateToken(issued.Token); ok {
		t.Fatal("expired token accepted")
	}
	k.ExpiresAt = nil
	r.keys[k.ID] = k
	if _, ok := r.AuthenticateToken(issued.Token); ok {
		t.Fatal("legacy non-expiring credential accepted as token")
	}
	future := time.Now().Add(time.Hour)
	k.ExpiresAt = &future
	r.keys[k.ID] = k
	if err := r.Revoke(k.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.AuthenticateToken(issued.Token); ok {
		t.Fatal("revoked token accepted")
	}
	for _, hours := range []int{-1, 721} {
		if _, err := r.Issue(IssueRequest{Name: "bad", Purpose: "RESTART", ExpiresHours: hours}, "owner"); err == nil {
			t.Fatal("invalid expiration accepted")
		}
	}
}
