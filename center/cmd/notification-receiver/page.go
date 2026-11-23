package main

import (
	_ "embed"
	"net/http"
)

//go:embed page.html
var pageHTML []byte

func receiverPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		jsonResponse(w, 404, map[string]string{"code": "NOT_FOUND"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		jsonResponse(w, 405, map[string]string{"code": "METHOD_NOT_ALLOWED"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	_, _ = w.Write(pageHTML)
}

// Viewing sources can read the page and records but cannot send callbacks.
func receiverSourceGuard(next http.Handler, callbackIPs, viewIPs string) (http.Handler, error) {
	callbacks, err := sourceGuard(next, callbackIPs)
	if err != nil {
		return nil, err
	}
	if viewIPs == "" {
		return callbacks, nil
	}
	view, err := sourceGuard(next, viewIPs)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/api/v1/notifications/recent") {
			view.ServeHTTP(w, r)
			return
		}
		callbacks.ServeHTTP(w, r)
	}), nil
}
