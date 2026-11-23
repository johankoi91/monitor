// Center container: PostgreSQL first, schema second, center and receiver last.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	bootstrap "github.com/johankoi91/monitor/runtime/center/internal/bootstrap"
	"github.com/johankoi91/monitor/runtime/shared/supervisor"
	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "health" {
		if health() != nil {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	options := bootstrap.FromEnvironment()
	if err := options.Initialize(); err != nil {
		return err
	}
	applicationEnv, err := bootstrap.Environment(options.Private, options.Runtime, os.Environ())
	if err != nil {
		return err
	}
	g := supervisor.New(ctx)
	defer g.Stop()
	private := supervisor.Env("AVOPS_PRIVATE_DIR", "/private")
	pg, err := supervisor.EnvFile(filepath.Join(private, "postgres.env"))
	if err != nil {
		return err
	}
	owner, err := supervisor.EnvFile(filepath.Join(private, "owner.env"))
	if err != nil {
		return err
	}
	pgdata := supervisor.Env("PGDATA", "/postgres")
	if err = supervisor.Dir(pgdata, 999, 999); err != nil {
		return err
	}
	if err = supervisor.Dir(supervisor.Env("AVOPS_DATA_DIR", "/state"), 65532, 65532); err != nil {
		return err
	}
	pgEnv := append([]string{"PATH=" + os.Getenv("PATH"), "HOME=/var/lib/postgresql", "PGDATA=" + pgdata, "LANG=C.UTF-8", "PG_MAJOR=17"}, pg...)
	if err = g.Start("PostgreSQL", "/usr/local/bin/docker-entrypoint.sh", []string{"postgres", "-c", "listen_addresses=127.0.0.1", "-c", "shared_buffers=128MB"}, pgEnv, 999, 999); err != nil {
		return err
	}
	db, err := sql.Open("postgres", supervisor.Value(owner, "AVOPS_DATABASE_URL"))
	if err != nil {
		return errors.New("invalid owner database configuration")
	}
	defer db.Close()
	ready, cancel := context.WithTimeout(g.Context, 90*time.Second)
	defer cancel()
	for {
		attempt, done := context.WithTimeout(ready, time.Second)
		err = db.PingContext(attempt)
		done()
		if err == nil {
			break
		}
		select {
		case <-ready.Done():
			return errors.New("PostgreSQL startup did not become ready")
		case <-time.After(250 * time.Millisecond):
		}
	}
	migration := exec.CommandContext(g.Context, "/dbmigrate")
	migration.Env = owner
	migration.Stdout = os.Stdout
	migration.Stderr = os.Stderr
	if migration.Run() != nil {
		return errors.New("database schema initialization failed")
	}
	// The owner and PostgreSQL passwords are confined to their respective children.
	if err = g.Start("center", "/center", nil, applicationEnv, 65532, 65532); err != nil {
		return err
	}
	receiverPath := filepath.Join(private, "receiver.env")
	if _, err = os.Stat(receiverPath); err == nil {
		receiver, err := supervisor.EnvFile(receiverPath)
		if err != nil {
			return err
		}
		dir := supervisor.Env("AVOPS_RECEIVER_DATA_DIR", "/receiver")
		if err = supervisor.Dir(dir, 65532, 65532); err != nil {
			return err
		}
		receiver = append(receiver, "AVOPS_RECEIVER_DATA_DIR="+dir,
			"AVOPS_RECEIVER_TLS_CERT="+supervisor.Env("AVOPS_RECEIVER_TLS_CERT", "/config/server.pem"),
			"AVOPS_RECEIVER_TLS_KEY="+supervisor.Env("AVOPS_RECEIVER_TLS_KEY", "/config/server-key.pem"))
		if address := os.Getenv("AVOPS_RECEIVER_ADDR"); address != "" {
			receiver = append(receiver, "AVOPS_RECEIVER_ADDR="+address)
		}
		if err = g.Start("notification receiver", "/notification-receiver", nil, receiver, 65532, 65532); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return errors.New("receiver configuration unavailable")
	}
	log.Print("center container started with embedded PostgreSQL")
	return g.Wait()
}

func health() error {
	options := bootstrap.FromEnvironment()
	environment, err := bootstrap.Environment(options.Private, options.Runtime, os.Environ())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", supervisor.Value(environment, "AVOPS_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	address := supervisor.Value(environment, "AVOPS_CENTER_LOCAL_ADDR")
	if address == "" {
		address = supervisor.Value(environment, "AVOPS_CENTER_ADDR")
	}
	return checkListener(address)
}
func checkListener(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), time.Second)
	if err == nil {
		conn.Close()
	}
	return err
}
