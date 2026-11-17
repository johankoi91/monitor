// Package durable supplies bounded atomic files for probe state and diagnostic data.
package durable

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func Write(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Bytes(path, b)
}
func Bytes(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Read(path string, max int64, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > max {
		return errors.New("state size limit exceeded")
	}
	return json.Unmarshal(b, value)
}
