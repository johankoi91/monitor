package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (h *Hub) ServeRegistryAdmin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/probes/status" && r.Method == "GET" {
		reply(w, 200, h.Snapshot())
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/probes/"), "/")
	if len(parts) != 3 || parts[0] != "runtimes" || parts[2] != "enabled" || r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.Enabled == nil || decoder.Decode(&struct{}{}) != io.EOF {
		reply(w, 400, map[string]string{"message": "enabled 必填"})
		return
	}
	if err := h.EnableProbe(parts[1], *body.Enabled); err != nil {
		reply(w, 409, map[string]string{"message": err.Error()})
		return
	}
	reply(w, 202, map[string]bool{"accepted": true})
}
