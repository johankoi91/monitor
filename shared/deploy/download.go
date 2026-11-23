package deploy

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func digestBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func Fetch(ctx context.Context, url, destination, expected string) error {
	if actual, e := Digest(destination); e == nil && actual == expected {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(destination), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(destination), ".download-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	client := http.Client{Timeout: 5 * time.Minute}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	response, e := client.Do(request)
	if e != nil {
		return errors.New("dependency download unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("dependency download failed")
	}
	h := sha256.New()
	if _, e = io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, 2<<30)); e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("dependency SHA256 mismatch")
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), destination)
}
func ExtractSDK(archive, destination string) error {
	z, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer z.Close()
	for _, f := range z.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if (filepath.ToSlash(clean) != "agora_sdk" && !strings.HasPrefix(filepath.ToSlash(clean), "agora_sdk/")) || !Inside(destination, filepath.Join(destination, clean)) || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("unexpected vendor SDK archive entry")
		}
	}
	for _, f := range z.File {
		target := filepath.Join(destination, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(target, 0755); e != nil {
				return e
			}
			continue
		}
		in, e := f.Open()
		if e != nil {
			return e
		}
		e = CopyReader(in, target, f.Mode().Perm())
		in.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
