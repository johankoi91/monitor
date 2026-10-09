package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfficialRawBodyVectorsAndNoDowngrade(t *testing.T) {
	body := []byte(`{"eventType":10,"noticeId":"4eb720f0-8da7-11e9-a43e-53f411c2761f","notifyMs":1560408533119,"payload":{"a":"1","b":2},"productId":1}`)
	header := http.Header{}
	header.Set("Agora-Signature", "5a3bb6a6d9fad2ea9ae3fb707a14c9d7f3136df1")
	header.Set("Agora-Signature-V2", "de96da5acf03b0021ac3b4fa2225e7ae6f3533a30d50bb02c08ea4fa748bda24")
	if !verifySignature("secret", body, header) {
		t.Fatal("official signatures rejected")
	}
	if verifySignature("secret", append(body, ' '), header) {
		t.Fatal("changed raw body accepted")
	}
	header.Set("Agora-Signature-V2", "invalid")
	if verifySignature("secret", body, header) {
		t.Fatal("invalid V2 downgraded to V1")
	}
	header.Set("Agora-Signature-V2", "")
	if verifySignature("secret", body, header) {
		t.Fatal("empty provided V2 downgraded to V1")
	}
	header.Del("Agora-Signature-V2")
	if !verifySignature("secret", body, header) {
		t.Fatal("official SHA1 fallback rejected")
	}
}

func TestBasicDoesNotAuthorizeWebhook(t *testing.T) {
	store, err := openStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer store.file.Close()
	r := httptest.NewRequest("POST", "/api/v1/notifications", bytes.NewBufferString(`{}`))
	r.SetBasicAuth("receiver", "secret")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(store, "receiver", "secret").ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("Basic authorized callback without signature")
	}
}
