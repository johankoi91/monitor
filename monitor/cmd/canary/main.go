// canary is an isolated acceptance-test container, never an RTC business probe.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	started := time.Now().UTC()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "avops isolated acceptance canary; started=%s\n", started.Format(time.RFC3339Nano))
	})
	server := &http.Server{Addr: "127.0.0.1:18098", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
