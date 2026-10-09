// viewer exposes the receiver's read-only page on loopback with verified TLS.
package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	config := flag.String("credentials", "", "protected receiver env file")
	flag.Parse()
	data, err := os.ReadFile(*config)
	if err != nil {
		log.Fatal("diagnostic credential file unavailable")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	id, secret := values["AVOPS_RECEIVER_ID"], values["AVOPS_RECEIVER_SECRET"]
	if id == "" || secret == "" {
		log.Fatal("diagnostic credentials missing")
	}
	u, _ := url.Parse("https://114.132.190.201:18443")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
			r.Out.Header.Del("Forwarded")
			r.Out.Header.Del("X-Forwarded-For")
			r.Out.SetBasicAuth(id, secret)
		},
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "edge.rtcdevelopers.com"}, ResponseHeaderTimeout: 10 * time.Second},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "notification receiver unavailable", 502)
		},
	}
	const listen = "127.0.0.1:18087"
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != listen || r.Method != "GET" || (r.URL.Path != "/" && r.URL.Path != "/api/v1/notifications/recent") {
			http.Error(w, "read-only local viewer", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+listen {
			http.Error(w, "cross-origin denied", 403)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			http.Error(w, "cross-site denied", 403)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	log.Printf("receiver viewer: http://%s (read-only, HTTPS upstream verified)", listen)
	server := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
