package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func bytesDigest(b []byte) (string, error) {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func downloadDemo(ctx context.Context, path string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".demo-")
	if e != nil {
		return e
	}
	defer f.Close()
	defer os.Remove(f.Name())
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://download.agora.io/demo/test/server_sdk_test_data_202410252135.zip", nil)
	if e != nil {
		return e
	}
	client := http.Client{Timeout: 5 * time.Minute}
	r, e := client.Do(request)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("demo download failed")
	}
	if _, e = io.Copy(f, io.LimitReader(r.Body, 1<<30)); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
