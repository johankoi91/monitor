package postgres

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ImportDirectory is offline and atomic. A different nonempty destination is
// rejected; a repeated identical import returns the existing manifest.
func (d *DB) ImportDirectory(dir string) (map[string]int, error) {
	type file struct {
		name, kind string
		data       []byte
	}
	files := []file{}
	digest := sha256.New()
	counts := map[string]int{}
	for _, entry := range []struct{ name, kind string }{{"current-baseline.json", "baseline"}, {"notification-settings.json", "notification_settings"}, {"operations.jsonl", "operations"}, {"access-keys.jsonl", "access"}, {"notifications.jsonl", "notifications"}} {
		b, err := os.ReadFile(filepath.Join(dir, entry.name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(b) > 64<<20 {
			return nil, errors.New("legacy storage exceeds import budget")
		}
		digest.Write([]byte(entry.name))
		digest.Write(b)
		files = append(files, file{entry.name, entry.kind, b})
	}
	fingerprint := hex.EncodeToString(digest.Sum(nil))
	err := d.transact(2*time.Minute, func(ctx context.Context, tx *sql.Tx) error {
		var previous []byte
		e := tx.QueryRowContext(ctx, "SELECT manifest FROM avops.import_batches WHERE source_digest=$1", fingerprint).Scan(&previous)
		if e == nil {
			return json.Unmarshal(previous, &counts)
		}
		if e != sql.ErrNoRows {
			return e
		}
		var existing int
		if e = tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM avops.documents)+(SELECT count(*) FROM avops.operations)+(SELECT count(*) FROM avops.access_keys)+(SELECT count(*) FROM avops.notification_events)").Scan(&existing); e != nil {
			return e
		}
		if existing != 0 {
			return errors.New("destination contains data; import refuses to overwrite")
		}
		for _, f := range files {
			if f.kind == "baseline" || f.kind == "notification_settings" {
				if !json.Valid(f.data) {
					return errors.New("invalid legacy document")
				}
				if e = d.saveDocument(ctx, tx, f.kind, f.data); e != nil {
					return e
				}
				counts[f.name] = 1
				continue
			}
			if len(f.data) > 0 && f.data[len(f.data)-1] != '\n' {
				return errors.New("incomplete legacy journal tail")
			}
			scanner := bufio.NewScanner(bytes.NewReader(f.data))
			scanner.Buffer(make([]byte, 4096), 128<<10)
			for scanner.Scan() {
				b := scanner.Bytes()
				if !json.Valid(b) {
					return errors.New("invalid legacy journal")
				}
				if e = projectLog(ctx, tx, f.kind, b); e != nil {
					return e
				}
				counts[f.name]++
			}
			if e = scanner.Err(); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO avops.import_batches(source_digest,manifest) VALUES($1,$2)", fingerprint, jsonText(counts))
		return e
	})
	return counts, err
}
