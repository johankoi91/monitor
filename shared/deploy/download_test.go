package deploy_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedDownloadRejectsWrongContentWithoutPublishingIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("wrong")) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "dependency")
	if e := d.Fetch(context.Background(), server.URL, path, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); e == nil {
		t.Fatal("wrong digest accepted")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("invalid dependency published")
	}
	sum := sha256.Sum256([]byte("wrong"))
	if e := d.Fetch(context.Background(), server.URL, path, hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
}
func TestSDKExtractionAcceptsRootDirectoryAndRejectsTraversal(t *testing.T) {
	for _, bad := range []bool{false, true} {
		root := t.TempDir()
		archive := filepath.Join(root, "sdk.zip")
		f, _ := os.Create(archive)
		z := zip.NewWriter(f)
		z.Create("agora_sdk/")
		name := "agora_sdk/include/header.h"
		if bad {
			name = "agora_sdk/../../escaped"
		}
		w, _ := z.Create(name)
		w.Write([]byte("fixture"))
		z.Close()
		f.Close()
		e := d.ExtractSDK(archive, filepath.Join(root, "sdk"))
		if bad && e == nil {
			t.Fatal("SDK traversal accepted")
		}
		if !bad && e != nil {
			t.Fatal(e)
		}
	}
}
