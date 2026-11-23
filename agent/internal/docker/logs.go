package docker

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/agoralogs"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

func (c *Collector) VerifyLogSource(ctx context.Context, task model.LogTask) error {
	var raw inspect
	if err := c.get(ctx, "/containers/"+url.PathEscape(task.ContainerID)+"/json", &raw); err != nil {
		return errors.New("无法核验 agora_local_ap 容器")
	}
	s := startup(raw, time.Now().UTC(), c.policy)
	source, err := agoralogs.Source(s.Config)
	if err != nil || raw.ID != task.ContainerID || strings.TrimPrefix(raw.Name, "/") != "agora_local_ap" || s.Incomplete || s.Truncated || s.SnapshotID != task.SnapshotID || source != task.Source {
		return errors.New("agora_local_ap 身份或日志挂载已改变，请等待启动参数更新后重试")
	}
	for _, service := range task.Services {
		var target inspect
		if !model.ValidProcesses(service.Processes) || c.get(ctx, "/containers/"+url.PathEscape(service.ContainerID)+"/json", &target) != nil {
			return errors.New("无法核验所选容器的进程 PID")
		}
		startup := startup(target, time.Now().UTC(), c.policy)
		path, err := agoralogs.Source(startup.Config)
		if err != nil || path != task.Source || target.ID != service.ContainerID || strings.TrimPrefix(target.Name, "/") != service.ContainerName || startup.SnapshotID != service.SnapshotID || startup.Incomplete || startup.Truncated || target.State == nil || target.State.Status != "running" {
			return errors.New("所选容器身份或日志挂载已改变，请刷新后重试")
		}
		current, err := c.processes(ctx, target)
		if err != nil {
			return errors.New("无法核验所选容器的进程 PID")
		}
		for _, reported := range service.Processes {
			found := false
			for _, actual := range current {
				if actual == reported {
					found = true
					break
				}
			}
			if !found {
				return errors.New("容器进程 PID 已改变，请等待 Agent 更新发现信息后重试")
			}
		}
	}
	return nil
}
