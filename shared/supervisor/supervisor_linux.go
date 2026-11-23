// Package supervisor keeps each deployment unit's required children alive together.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}
type Group struct {
	Context  context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	children []*child
	failure  error
	stopping bool
}

func New(ctx context.Context) *Group {
	ctx, cancel := context.WithCancel(ctx)
	return &Group{Context: ctx, cancel: cancel}
}

func (g *Group) Start(name, path string, args, environment []string, uid, gid uint32) error {
	cmd := exec.Command(path, args...)
	cmd.Env = environment
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM, Credential: &syscall.Credential{Uid: uid, Gid: gid}}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start %s", name)
	}
	c := &child{cmd: cmd, done: make(chan struct{})}
	g.mu.Lock()
	g.children = append(g.children, c)
	g.mu.Unlock()
	go func() {
		err := cmd.Wait()
		g.mu.Lock()
		if !g.stopping && g.Context.Err() == nil {
			if err == nil {
				err = errors.New("unexpected exit")
			}
			g.failure = fmt.Errorf("%s stopped: %w", name, err)
			g.cancel()
		}
		close(c.done)
		g.mu.Unlock()
	}()
	return nil
}

// Stop shuts applications down before their database/collector. Adopted workers
// are reaped only after direct children have been waited, so Wait cannot race.
func (g *Group) Stop() {
	g.mu.Lock()
	g.stopping = true
	children := append([]*child{}, g.children...)
	g.mu.Unlock()
	for i := len(children) - 1; i >= 0; i-- {
		c := children[i]
		select {
		case <-c.done:
			continue
		default:
		}
		syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-c.done:
		case <-time.After(12 * time.Second):
			syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			<-c.done
		}
	}
	g.cancel()
	// PID 1 adopts grandchildren; all process groups have now exited.
	for {
		var status syscall.WaitStatus
		pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 {
			break
		}
	}
}

func (g *Group) Wait() error {
	<-g.Context.Done()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.failure
}

// EnvFile accepts the same unquoted KEY=value format as docker --env-file.
// No contents or parser input is ever included in an error.
func EnvFile(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New("private environment file unavailable")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private environment file must be mode 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("private environment file unreadable")
	}
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != key || key == "" {
			return nil, errors.New("invalid private environment file")
		}
		result = append(result, line)
	}
	return result, nil
}
func Value(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], key+"=") {
			return strings.TrimPrefix(env[i], key+"=")
		}
	}
	return ""
}
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func Dir(path string, uid, gid int) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("cannot create persistent directory")
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return errors.New("cannot set persistent directory ownership")
	}
	return nil
}
