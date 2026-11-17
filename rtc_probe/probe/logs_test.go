package probe

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeLogsRemainSeparateBinaryFiles(t *testing.T) {
	c := &Client{config: Config{DataDir: t.TempDir(), Secret: "probe-secret-not-in-log"}}
	dir := filepath.Join(c.runDir("run"), "sdk")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, second := []byte{0, 255, 254, 1, 2, 3}, []byte("separate API log")
	for name, data := range map[string][]byte{"agorasdk.log": first, "agoraapi.log": second, "agoradns.dat": []byte("not a diagnostic log")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	files, err := c.appendSDKLogs(w, "run")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || len(z.File) != 2 {
		t.Fatal("native logs or auxiliary data merged", files)
	}
	expected := map[string][]byte{"sdk/agorasdk.log": first, "sdk/agoraapi.log": second}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, expected[f.Name]) {
			t.Fatal("native binary log changed", f.Name)
		}
	}
}
