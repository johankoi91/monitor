package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/access"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/identity"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"github.com/johankoi91/monitor/runtime/internal/postgres"
	"github.com/johankoi91/monitor/runtime/internal/storage"
	"github.com/johankoi91/monitor/runtime/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}
func main() {
	id, secret := os.Getenv("AVOPS_ADMIN_ID"), os.Getenv("AVOPS_ADMIN_SECRET")
	if id == "" || len(secret) < 24 {
		log.Fatal("independent admin ID and 24+ character secret required")
	}
	config, err := center.ReadConfig(env("AVOPS_CENTER_CONFIG", "center.yaml"))
	if err != nil {
		log.Fatal("cannot load center configuration")
	}
	for _, agent := range config.Agents {
		if agent.ID == id || agent.Secret == secret {
			log.Fatal("management and Agent credentials must be independent")
		}
		if config.Notifications.Enabled && config.Notifications.Secret == agent.Secret {
			log.Fatal("notification credentials must be independent from Agent credentials")
		}
	}
	if config.Notifications.Enabled && config.Notifications.Secret == secret {
		log.Fatal("notification credentials must be independent from management credentials")
	}
	var backend storage.Backend
	var database *postgres.DB
	var accounts *identity.Service
	if dsn := os.Getenv("AVOPS_DATABASE_URL"); dsn != "" {
		key, e := hex.DecodeString(os.Getenv("AVOPS_STORAGE_KEY"))
		if e != nil {
			log.Fatal("invalid storage encryption key")
		}
		db, e := postgres.Open(dsn, key, false)
		if e != nil {
			log.Fatal(e)
		}
		defer db.Close()
		backend = db
		database = db
		accounts, e = identity.New(db.Pool)
		if e != nil {
			log.Fatal("cannot initialize accounts")
		}
		if e = accounts.Bootstrap(env("AVOPS_BOOTSTRAP_USERNAME", "admin"), os.Getenv("AVOPS_BOOTSTRAP_PASSWORD")); e != nil {
			log.Fatal("cannot initialize bootstrap account")
		}
	} else if os.Getenv("AVOPS_STORAGE_MODE") != "file-dev" {
		log.Fatal("AVOPS_DATABASE_URL required; file-dev is only for isolated development")
	}
	store, err := center.NewStore(env("AVOPS_DATA_DIR", "data"), config, backend)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err = store.ApplyRestartRules(); err != nil {
		log.Fatal("cannot persist deployment restart permissions")
	}
	notifier, err := notify.New(env("AVOPS_DATA_DIR", "data"), config.Notifications, backend)
	if err != nil {
		log.Fatal(err)
	}
	defer notifier.Close()
	accessRegistry, err := access.New(env("AVOPS_DATA_DIR", "data"), backend)
	if err != nil {
		log.Fatal(err)
	}
	defer accessRegistry.Close()
	assets, _ := fs.Sub(web.Files, "static")
	allowed := []string{}
	for _, ip := range strings.Split(os.Getenv("AVOPS_ALLOWED_IPS"), ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			if net.ParseIP(ip) == nil {
				log.Fatal("invalid allowed source IP")
			}
			allowed = append(allowed, ip)
		}
	}
	storageWarning, err := strconv.ParseInt(env("AVOPS_DATABASE_WARNING_BYTES", "1073741824"), 10, 64)
	if err != nil || storageWarning <= 0 {
		log.Fatal("invalid database warning threshold")
	}
	handler := (&center.Server{Store: store, AdminID: id, AdminSecret: secret, Assets: http.FileServer(http.FS(assets)), AllowedIPs: allowed, Notifier: notifier, Access: accessRegistry, Accounts: accounts, Database: database, StorageWarningBytes: storageWarning, StorageDiskPath: env("AVOPS_STORAGE_DISK_PATH", "/var/lib")}).Handler()
	addr := env("AVOPS_CENTER_ADDR", "127.0.0.1:19443")
	cert, key := os.Getenv("AVOPS_TLS_CERT"), os.Getenv("AVOPS_TLS_KEY")
	dev := os.Getenv("AVOPS_DEV_HTTP") == "1"
	if dev && !loopback(addr) {
		log.Fatal("dev HTTP listener must be loopback")
	}
	if !dev && (cert == "" || key == "") {
		log.Fatal("TLS certificate and private key required")
	}
	makeServer := func(address string) *http.Server {
		return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	server := makeServer(addr)
	servers := []*http.Server{server}
	errorsCh := make(chan error, 2)
	go func() {
		if dev {
			errorsCh <- server.ListenAndServe()
		} else {
			errorsCh <- server.ListenAndServeTLS(cert, key)
		}
	}()
	// Optional SSH-tunnel entry point. Plain HTTP is never exposed on public interfaces.
	if local := os.Getenv("AVOPS_CENTER_LOCAL_ADDR"); local != "" {
		if !loopback(local) {
			log.Fatal("local UI listener must be loopback")
		}
		extra := makeServer(local)
		servers = append(servers, extra)
		go func() { errorsCh <- extra.ListenAndServe() }()
	}
	log.Printf("monitor center listening on %s", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtimeCtx, stopRuntime := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(2)
	if database != nil {
		workers.Add(1)
		go func() { defer workers.Done(); database.RunMaintenance(runtimeCtx) }()
	}
	go func() { defer workers.Done(); notifier.Run(runtimeCtx) }()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runtimeCtx.Done():
				return
			case <-ticker.C:
				store.TickOperations(time.Now().UTC())
				revision, facts := store.NotificationFacts()
				notifier.Observe(revision, facts)
			}
		}
	}()
	defer workers.Wait()
	defer stopRuntime()
	var listenerError error
	select {
	case <-ctx.Done():
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			listenerError = err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(shutdown)
	}
	stopRuntime()
	workers.Wait()
	if listenerError != nil {
		log.Fatalf("center listener stopped: %v", listenerError)
	}
}
