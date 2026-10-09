package center

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/johankoi91/monitor/runtime/web"
)

// Ant Design inserts style elements at runtime. Authorize those elements with
// a fresh nonce while retaining self-only scripts; never cache the HTML shell.
func serveLoginShell(w http.ResponseWriter, r *http.Request) {
	body, err := web.Files.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "UI assets unavailable", http.StatusServiceUnavailable)
		return
	}
	nonce := ID()
	body = bytes.ReplaceAll(body, []byte("__AVOPS_STYLE_NONCE__"), []byte(nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'nonce-"+nonce+"'; style-src-attr 'unsafe-inline'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}
