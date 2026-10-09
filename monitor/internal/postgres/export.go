package postgres

import (
	"errors"
	"os"
	"path/filepath"
)

// ExportLegacy is a controlled compatibility rollback export. Accounts remain
// backed up in PostgreSQL; legacy file-mode binaries cannot implement RBAC.
func (d *DB) ExportLegacy(dir string) error {
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		return errors.New("export directory must not exist")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	write := func(name string, b []byte) error {
		f, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if _, e = f.Write(b); e != nil {
			return e
		}
		return f.Sync()
	}
	for name, file := range map[string]string{"baseline": "current-baseline.json", "notification_settings": "notification-settings.json"} {
		b, ok, e := d.LoadDocument(name)
		if e != nil {
			return e
		}
		if ok {
			if e = write(file, b); e != nil {
				return e
			}
		}
	}
	for name, file := range map[string]string{"operations": "operations.jsonl", "access": "access-keys.jsonl", "notifications": "notifications.jsonl"} {
		rows, e := d.ReadLog(name, "", 0)
		if e != nil {
			return e
		}
		var body []byte
		for _, r := range rows {
			body = append(body, r...)
			body = append(body, '\n')
		}
		if e = write(file, body); e != nil {
			return e
		}
	}
	return nil
}
