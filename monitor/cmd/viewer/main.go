// viewer serves a loopback UI through a certificate-verified HTTPS upstream.
// It keeps DNS unchanged and never embeds or injects management credentials.
package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:18086", "loopback listener")
	target := flag.String("upstream", "https://111.230.108.76:19443", "fixed HTTPS center URL")
	serverName := flag.String("tls-server-name", "edge.rtcdevelopers.com", "verified certificate identity")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		log.Fatal("viewer listener must be loopback")
	}
	u, err := url.Parse(*target)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		log.Fatal("upstream must be a credential-free HTTPS origin")
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
			r.Out.Header.Del("X-Forwarded-For")
			r.Out.Header.Del("Forwarded")
			if r.In.Header.Get("Origin") != "" {
				r.Out.Header.Set("Origin", u.Scheme+"://"+u.Host)
			}
		},
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: *serverName}, ResponseHeaderTimeout: 15 * time.Second},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "HTTPS center unavailable; verify source access and certificate routing", http.StatusBadGateway)
		},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != *addr {
			http.Error(w, "invalid local host", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+*addr {
			http.Error(w, "cross-origin request denied", http.StatusForbidden)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && origin != "" && r.Header.Get("X-AVOPS-Request") != "1" {
			http.Error(w, "local write requires request header", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	log.Printf("local RTC viewer: http://%s (HTTPS certificate verification enabled)", *addr)
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
