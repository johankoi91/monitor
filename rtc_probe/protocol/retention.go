package protocol

import "errors"

type RetentionPolicy struct {
	Version        string `json:"version"`
	SuccessMinutes int    `json:"success_minutes"`
	FailureMinutes int    `json:"failure_minutes"`
	DiskMB         int    `json:"disk_mb"`
}

func DefaultRetention() RetentionPolicy {
	return RetentionPolicy{Version: "default-v1", SuccessMinutes: 60, FailureMinutes: 1440, DiskMB: 1024}
}
func (p RetentionPolicy) Validate() error {
	if p.Version == "" || p.SuccessMinutes < 1 || p.SuccessMinutes > 525600 || p.FailureMinutes < p.SuccessMinutes || p.FailureMinutes > 525600 || p.DiskMB < 16 || p.DiskMB > 65536 {
		return errors.New("探针保留策略无效：失败保留时长不得短于成功，磁盘预算为 16–65536 MiB")
	}
	return nil
}
