// Package journal provides bounded, exclusive, fsynced append-only records.
package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrCapacity = errors.New("journal capacity reached")

type Journal struct {
	mu          sync.Mutex
	file        *os.File
	size, limit int64
	failed      bool
}

func Open(path string, limit int64, restore func(json.RawMessage) error) (*Journal, error) {
	if limit <= 0 {
		return nil, errors.New("invalid journal limit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	closeError := func(err error) (*Journal, error) { f.Close(); return nil, err }
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return closeError(errors.New("journal already has a writer"))
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return closeError(err)
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return closeError(err)
	}
	info, err := f.Stat()
	if err != nil {
		return closeError(err)
	}
	if info.Size() > limit {
		return closeError(ErrCapacity)
	}
	if info.Size() > 0 {
		last := []byte{0}
		if _, err = f.ReadAt(last, info.Size()-1); err != nil || last[0] != '\n' {
			return closeError(errors.New("incomplete journal tail; preserve for repair"))
		}
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 128<<10)
	for scan.Scan() {
		line := scan.Bytes()
		if !json.Valid(line) {
			return closeError(errors.New("corrupt journal record"))
		}
		if restore != nil {
			if err = restore(append(json.RawMessage(nil), line...)); err != nil {
				return closeError(err)
			}
		}
	}
	if err = scan.Err(); err != nil {
		return closeError(err)
	}
	return &Journal{file: f, size: info.Size(), limit: limit}, nil
}
func (j *Journal) Append(value any, reserve int64) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 128<<10 {
		return errors.New("journal record too large")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed {
		return errors.New("journal unavailable")
	}
	if j.size+int64(len(data))+reserve > j.limit {
		return ErrCapacity
	}
	before := j.size
	n, err := j.file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = j.file.Sync()
	}
	if err != nil {
		j.failed = true
		if j.file.Truncate(before) == nil {
			j.file.Sync()
		}
		return errors.New("journal write failed")
	}
	j.size += int64(len(data))
	return nil
}
func (j *Journal) Healthy() bool         { j.mu.Lock(); defer j.mu.Unlock(); return !j.failed }
func (j *Journal) Usage() (int64, int64) { j.mu.Lock(); defer j.mu.Unlock(); return j.size, j.limit }
func (j *Journal) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file != nil {
		syscall.Flock(int(j.file.Fd()), syscall.LOCK_UN)
		j.file.Close()
		j.file = nil
		j.failed = true
	}
}

func (j *Journal) Scan(visit func(json.RawMessage) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return errors.New("journal closed")
	}
	scanner := bufio.NewScanner(io.NewSectionReader(j.file, 0, j.size))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		if err := visit(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
