// fetch-cadvisor downloads the official pinned release with parallel HTTP ranges
// and verifies GitHub's published SHA256 before producing an executable artifact.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const source = "https://github.com/google/cadvisor/releases/download/v0.60.6/cadvisor-v0.60.6-linux-amd64"
const size = int64(39175092)
const checksum = "c381c2c911bc43d465d1e0eaff60f96d58c031c409bddf06da0316fdde8a9296"

func main() {
	out := flag.String("out", "", "new output artifact path")
	flag.Parse()
	if *out == "" {
		panic("out required")
	}
	if _, err := os.Stat(*out); !os.IsNotExist(err) {
		panic("refusing to overwrite an existing artifact")
	}
	f, err := os.CreateTemp(filepath.Dir(*out), ".cadvisor-download-")
	if err != nil {
		panic(err)
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if err = f.Truncate(size); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConnsPerHost: 32, MaxConnsPerHost: 32, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}}
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := size * int64(i) / workers
			end := size*int64(i+1)/workers - 1
			r, _ := http.NewRequestWithContext(ctx, "GET", source, nil)
			r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
			response, e := client.Do(r)
			if e != nil {
				errs <- fmt.Errorf("part %d unavailable", i)
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 206 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, size) {
				errs <- fmt.Errorf("part %d invalid range response", i)
				return
			}
			b, e := io.ReadAll(io.LimitReader(response.Body, end-start+2))
			if e != nil || int64(len(b)) != end-start+1 {
				errs <- fmt.Errorf("part %d incomplete", i)
				return
			}
			if _, e = f.WriteAt(b, start); e != nil {
				errs <- e
				return
			}
			fmt.Printf("Verified-size part %d/%d received\n", i+1, workers)
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if _, err = f.Seek(0, 0); err != nil {
		panic(err)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		panic(err)
	}
	if hex.EncodeToString(h.Sum(nil)) != checksum {
		panic("artifact SHA256 mismatch")
	}
	if err = f.Chmod(0755); err != nil {
		panic(err)
	}
	if err = f.Sync(); err != nil {
		panic(err)
	}
	if err = os.Rename(f.Name(), *out); err != nil {
		panic(err)
	}
	fmt.Println("Official cAdvisor v0.60.6 SHA256 verified")
}
