package center

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
	"github.com/johankoi91/monitor/rtc-probe/tests/testkit"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/testfixture"
	"github.com/johankoi91/monitor/runtime/internal/webui"
)

func TestRTCUIMockWorker(t *testing.T) {
	if os.Getenv("RTC_UI_WORKER") != "1" {
		return
	}
	pipe := os.NewFile(3, "evidence")
	err := testkit.Run(context.Background(), os.Stdin, pipe)
	pipe.Close()
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestRTCUIWorkflow(t *testing.T) {
	if os.Getenv("AVOPS_BROWSER_TEST") != "1" {
		t.Skip("opt-in browser verification")
	}
	config := hub.Config{}
	h, err := hub.New(t.TempDir(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	assets, err := fs.Sub(webui.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	store := setup(t)
	discovered := report("agora_udp_media_edge_1", "agora_udp_media_edge_2", "agora_udp_media_edge_3", "agora_aut_media_edge_1", "agora_aut_media_edge_2", "agora_aut_media_edge_3", "agora_web_media_edge_1", "agora_local_ap")
	discovered.Host.HostAddress = "127.0.0.1"
	discovered.Host.IPAddresses = []string{"127.0.0.1", "172.16.80.25"}
	for i := range discovered.Containers {
		port := 4001 + i
		if i >= 3 {
			port = 4701 + i - 3
		}
		if i == 6 {
			port = 4501
		}
		discovered.Containers[i].Startup.Config = model.StartupConfig{Args: []string{"--ap", "172.16.80.25", "--ip-for-client", "127.0.0.1", "--user-port", strconv.Itoa(port)}}
	}
	h.SetNativeInventoryProvider(store.NativeInventory)
	session := store.Connect("rtc-a")
	if err := store.Receive("rtc-a", session, discovered); err != nil {
		t.Fatal(err)
	}
	enrollToken := strings.Repeat("e", 32)
	server := httptest.NewServer(testfixture.AccountAuth((&Server{Store: store, Probes: h, ProbeEnrollmentToken: enrollToken, AdminID: "fixture", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler()))
	defer server.Close()
	done := []chan error{}
	hostDir := t.TempDir()
	host, err := probe.LoadHostIdentity(hostDir)
	if err != nil {
		t.Fatal(err)
	}
	host.Host.Name = "模拟服务器"
	host.Host.Address = "127.0.0.1"
	host.Host.Location = "模拟位置"
	hostData, _ := json.Marshal(host)
	if err = os.WriteFile(filepath.Join(hostDir, "server-identity-v2.json"), hostData, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"native", "web"} {
		id, secret := protocol.RuntimeIdentityKey(host.Secret, "rtc", kind)
		driver := "agora"
		if kind == "web" {
			driver = protocol.WebDriver
		}
		if err = h.EnrollProbe(protocol.ProbeEnrollment{Host: host.Host, HostSecret: host.Secret, RuntimeID: id, RuntimeSecret: secret, ServiceType: "rtc", ClientType: kind, Driver: driver}, "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	for range []int{0} {
		c, err := probe.New(probe.Config{URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/probes/connect", HostDir: hostDir, EnrollmentToken: enrollToken, ServiceType: "rtc", ClientType: "native", DataDir: t.TempDir(), Driver: "agora", AllowPlaintextLoopback: true, WorkerCommand: func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRTCUIMockWorker$")
			cmd.Env = append(os.Environ(), "RTC_UI_WORKER=1")
			return cmd
		}})
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan error, 1)
		done = append(done, ch)
		go func() { ch <- c.Run(ctx) }()
	}
	webID, _ := protocol.RuntimeIdentityKey(host.Secret, "rtc", "web")
	webSession := h.Session(webID)
	webHello := protocol.Msg("hello")
	webHello.Driver = protocol.WebDriver
	webHello.HostID = host.Host.ID
	webHello.ServiceType = "rtc"
	webHello.ClientType = "web"
	webHello.Capacity = 1
	webHello.Capabilities = []string{protocol.ProbeEdgeAddressCapability, protocol.WebMediaCapability, protocol.SharedServiceLogsCapability}
	if _, err := h.Receive(webID, webSession, webHello); err != nil {
		t.Fatal(err)
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				message := webHello
				message.Type = "heartbeat"
				_, _ = h.Receive(webID, webSession, message)
			}
		}
	}()
	defer func() {
		cancel()
		for _, ch := range done {
			select {
			case err := <-ch:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(8 * time.Second):
				t.Error("probe shutdown timed out")
			}
		}
	}()
	if stopFile := os.Getenv("AVOPS_CUA_STOP_FILE"); stopFile != "" {
		t.Logf("CUA fixture: %s", server.URL)
		timeout := time.NewTimer(20 * time.Minute)
		defer timeout.Stop()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-timeout.C:
				t.Fatal("manual UI verification timed out")
			case <-tick.C:
				if _, err := os.Stat(stopFile); err == nil {
					return
				}
			}
		}
	}
	script, err := filepath.Abs("../../tests/rtc-ui.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "AVOPS_UI_URL="+server.URL, "AVOPS_ISOLATED_UI_TEST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("RTC browser workflow: %v\n%s", err, out)
	}
	t.Log(string(out))
}
