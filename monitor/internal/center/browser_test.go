package center

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/web"
)

func TestBrowserSelectionWorkflow(t *testing.T) {
	if os.Getenv("AVOPS_BROWSER_TEST") != "1" {
		t.Skip("opt-in browser test requires Node/Playwright/Chrome")
	}
	s := setup(t)
	names := []string{"agora_local_ap"}
	for i := 1; i < 55; i++ {
		names = append(names, "unused-"+strconv.Itoa(i))
	}
	connect(t, s, "rtc-a", names...)
	assets, err := fs.Sub(web.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler())
	defer server.Close()
	script, err := filepath.Abs("../../tests/ui.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AVOPS_UI_URL="+server.URL, "AVOPS_ISOLATED_UI_TEST=1")
	cmd.Env = append(cmd.Env, "AVOPS_CURL_CONFIG=", "AVOPS_UI_SCREENSHOT=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser verification failed: %v\n%s", err, output)
	}
	t.Log(string(output))
	if nodeCount(s.Baseline()) != 1 {
		t.Fatal("filtered/paginated page overwrote baseline")
	}
}
