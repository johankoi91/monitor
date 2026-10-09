package postgres

import (
	"context"
	"database/sql"
	"sync"
	"syscall"
	"time"
)

type MaintenanceStatus struct {
	CheckedAt       *time.Time `json:"checked_at"`
	DeletedSessions int64      `json:"deleted_sessions"`
	Error           string     `json:"error"`
}

type TableUsage struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type StorageStatus struct {
	Backend            string            `json:"backend"`
	DatabaseBytes      int64             `json:"database_bytes"`
	WarningBytes       int64             `json:"warning_bytes"`
	Tables             []TableUsage      `json:"tables"`
	DiskPath           string            `json:"disk_path"`
	DiskCapacityBytes  uint64            `json:"disk_capacity_bytes"`
	DiskAvailableBytes uint64            `json:"disk_available_bytes"`
	Warnings           []string          `json:"warnings"`
	Sessions           MaintenanceStatus `json:"sessions"`
	CheckedAt          time.Time         `json:"checked_at"`
}

type maintenanceState struct {
	mu     sync.Mutex
	status MaintenanceStatus
}

// No operations, idempotency evidence, pending notifications or audits are
// deleted. Expired tokens retain a further 24 hours and cannot authenticate.
func (d *DB) CleanupSessions() error {
	var deleted int64
	err := d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM avops.sessions WHERE token_hash IN
			(SELECT token_hash FROM avops.sessions WHERE expires_at < now()-interval '24 hours' ORDER BY expires_at LIMIT 1000)`)
		if err != nil {
			return err
		}
		deleted, err = result.RowsAffected()
		return err
	})
	d.maintenance.mu.Lock()
	defer d.maintenance.mu.Unlock()
	now := time.Now().UTC()
	d.maintenance.status = MaintenanceStatus{CheckedAt: &now, DeletedSessions: deleted}
	if err != nil {
		d.maintenance.status.Error = "SESSION_CLEANUP_FAILED"
	}
	return err
}

func (d *DB) RunMaintenance(ctx context.Context) {
	_ = d.CleanupSessions()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.CleanupSessions()
		}
	}
}

func (d *DB) StorageStatus(ctx context.Context, warningBytes int64, diskPath string) (StorageStatus, error) {
	if warningBytes <= 0 {
		warningBytes = 1 << 30
	}
	if diskPath == "" {
		diskPath = "/var/lib"
	}
	result := StorageStatus{Backend: "postgresql", WarningBytes: warningBytes, DiskPath: diskPath, Tables: []TableUsage{}, Warnings: []string{}, CheckedAt: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := d.Pool.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&result.DatabaseBytes); err != nil {
		return result, err
	}
	rows, err := d.Pool.QueryContext(ctx, `SELECT c.relname, pg_total_relation_size(c.oid) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='avops' AND c.relkind='r' ORDER BY pg_total_relation_size(c.oid) DESC`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var table TableUsage
		if err := rows.Scan(&table.Name, &table.Bytes); err != nil {
			return result, err
		}
		result.Tables = append(result.Tables, table)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if result.DatabaseBytes >= warningBytes {
		result.Warnings = append(result.Warnings, "DATABASE_SIZE_WARNING")
	}
	var disk syscall.Statfs_t
	if err := syscall.Statfs(diskPath, &disk); err != nil {
		result.Warnings = append(result.Warnings, "DISK_USAGE_UNAVAILABLE")
	} else {
		result.DiskCapacityBytes = disk.Blocks * uint64(disk.Bsize)
		result.DiskAvailableBytes = disk.Bavail * uint64(disk.Bsize)
		if result.DiskAvailableBytes < 2<<30 || result.DiskAvailableBytes < result.DiskCapacityBytes/10 {
			result.Warnings = append(result.Warnings, "DISK_SPACE_LOW")
		}
	}
	d.maintenance.mu.Lock()
	result.Sessions = d.maintenance.status
	d.maintenance.mu.Unlock()
	if result.Sessions.Error != "" {
		result.Warnings = append(result.Warnings, result.Sessions.Error)
	}
	return result, nil
}
