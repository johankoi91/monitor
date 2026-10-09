# cAdvisor 与 Agent 的协作及读取方式

## 链路

```text
本机 Docker / cgroup / 文件系统
           ↓ 只读采集
独立 avops-cadvisor 服务（官方二进制 / 可选容器）
           ↓ HTTP，仅 127.0.0.1:18089
本机 avops-agent（默认每 15 秒读取）
           ↓ 已有 WSS + 独立 Agent Basic Auth
中心：最新快照、45 秒新鲜度判断
           ↓ 已有受鉴权保护的 API
React：服务节点、发现容器的资源指标
```

cAdvisor 负责资源指标，Docker Unix API 负责身份、启动配置、运行态及既有 Healthcheck；TCP 探测与重启仍走原链路。资源指标失败不改变 Docker 快照完整性、业务健康或重启权限。

## 实际 HTTP 请求

Agent 私有环境配置增加：

```ini
AVOPS_CADVISOR_URL=http://127.0.0.1:18089
```

未配置则禁用此扩展。Agent 每个完整 Docker 采集周期发一次 GET：

```bash
curl --fail --max-time 3 \
  'http://127.0.0.1:18089/api/v2.0/stats/?type=docker&count=2&recursive=true'
```

返回对象的 key 是容器 cgroup 路径，例如 `/docker/<64位容器ID>`，value 是最近两次采样。Agent 也识别 systemd 的 `docker-<64位ID>.scope`。它只处理本次 Docker 清单中的完整 ID；不靠容器名、短 ID 或标签猜测，不把额外 cAdvisor 对象加入业务台账。

只接受无凭证、无查询串的 HTTP 回环基础地址；localhost 固定解析为 127.0.0.1。禁用代理及重定向，请求超时 3 秒，响应上限 16 MiB；最多接受 4096 个 cgroup 条目，每条最多两个样本。cAdvisor 不接收中心、Agent 或用户的 Key；上报通道继续使用原独立 Agent 凭证。

代码：[读取与换算](internal/cadvisor/client.go)、[Agent 接入](cmd/agent/main.go)、[中心新鲜度](internal/center/resources.go)、[页面展示](frontend/src/Resources.tsx)。

## 字段与计算

| cAdvisor 原字段 | Agent 上报字段 | 含义 |
|---|---|---|
| timestamp | sampled_at | 实际指标采样时间；不能用 HTTP 获取时间替代 |
| cpu.usage.total | cpu_usage_cores | 两次 CPU 累计纳秒的差 / 时间差纳秒；1.0 表示一个逻辑核 |
| memory.usage | memory_usage_bytes | 当前内存使用量，包含相应缓存口径 |
| memory.working_set | memory_working_set_bytes | 当前内存工作集，不等同于进程 RSS |
| filesystem[].usage | filesystem_usage_bytes | cAdvisor 报告的容器文件系统使用量之和；不能当成宿主机或挂载卷总占用 |
| network.interfaces[].rx_bytes | network_receive_bytes_per_second | 同名接口累计接收字节差 / 实际采样秒数，排除 lo |
| network.interfaces[].tx_bytes | network_transmit_bytes_per_second | 同名接口累计发送字节差 / 实际采样秒数，排除 lo |

例如：两次采样间隔 5 秒，CPU 累计使用增长 2.5 秒，则 `cpu_usage_cores=0.5`。网络累计接收增长 10,240 字节，则接收速率为 2,048 B/s。

host 或 `container:` 网络共享命名空间，无法保证流量属于单个容器。此时 `network_scope=HOST_SHARED`，单容器接收/发送速率返回 null，页面说明无法归属，不能把主机流量复制到每个容器后求和。

只上传这些数值字段、时间、状态及原因码，不转发 cAdvisor 原始标签、环境变量、自定义应用指标或接口名称。无效、缺失或无法计算的值使用 null；有效零值保留为 0。

## 扩展指标与生命周期（2026-10-06 追加）

| 分组 | 新增上报与展示 |
|---|---|
| CPU | 用户态/内核态核数、声明配额、受限周期比例、节流时间增长 |
| 内存 | 声明限额、工作集/限额占比、RSS、缓存、Swap、峰值、分配失败累计 |
| OOM | cAdvisor 事件计数及独立读取时间，Docker 最近状态中的 OOMKilled |
| 磁盘 I/O | 按 major/minor 匹配的每设备读写吞吐与 IOPS，不重复汇总映射盘和物理盘 |
| 网络 | 每秒收发包、丢包和错误；共享命名空间仍不归属单容器 |
| 文件系统 | 容器文件系统使用量；宿主机 Docker 数据盘容量、可用/空闲空间、使用率、inode 总数/剩余/使用率 |
| 生命周期 | Docker 创建、本次启动/结束、最后观测时间，运行时长、自动重启次数、退出码、Healthcheck |

CPU/内存限额从当前 Docker inspect 的 HostConfig 读取：NanoCpus 或 CpuQuota/CpuPeriod、Memory。已确认未限制时限额为 null，`limits_known=true`；未取得 HostConfig 为未知。内存占比使用 working_set / 显式 Memory 限额，不拿宿主机内存冒充容器限额。CPU 节流率为相邻受限周期增量 / 总周期增量，未设置配额或没有有效周期时留空。

OOM 总数另外读取本机 `/metrics` 中的 `container_oom_events_total`；只保留完整容器 ID 与数字，忽略其他指标/标签。它从 cAdvisor 启动以来累计，采集器重启会重置，不能当作跨重启历史事件总数。该读取失败不破坏 v2 stats；两次 HTTP 请求并行，共享 3 秒总超时。

```bash
curl --fail --max-time 3 'http://127.0.0.1:18089/metrics'
```

Docker 数据盘容量和 inode 通过 Agent 的本机 statfs 读取，默认目录 `/var/lib/docker`，可用 `AVOPS_DOCKER_DATA_ROOT` 指定实际绝对路径；上报 `source=agent_statfs`、`scope=HOST_DOCKER_STORAGE`。同机容器共享这些数据，不能累加，也不能替代所有挂载卷监控。cAdvisor 容器层里的 `available=0` / `has_inodes=false` 不被伪装成磁盘已满。

磁盘 I/O 仅从 diskio.io_service_bytes/io_serviced 取得，按设备分别算增量；同一笔 I/O 可以出现在 dm-0 和底层物理盘，因此多设备只给 disk_devices，不给错误的汇总值。内核不提供或计数重置时相应读数为 null。不会拿 filesystem 的宿主机累计扇区数冒充单容器 I/O。

生命周期来自 Docker inspect：缺失/零时间返回 null，只有 exited/dead 显示退出码；运行中的退出码不可解释为最近失败。RestartCount 是 Docker 自动重启次数，不能替代监控系统的操作记录。断联/过期的生命周期标出 stale，暂停计算“当前运行时长”。

## 新鲜度与失败

- 指标实际时间超过 45 秒、未来超过 5 秒、早于当前容器 StartedAt，则标记不可用/过期。
- 速率必须有两个有效、时间递增且间隔不超过 45 秒的样本；不能跨容器重启或计数器重置计算。
- 网络接口增减、计数器回退时速率留空，等待下一对有效样本。
- exited/dead/paused 等非运行容器不展示旧资源数据，Docker 运行态照常上报。
- HTTP 失败/超时只使资源指标不可用，下一周期重新尝试，不导致容器被误判缺失。
- 中心在读取时重新计算指标时间及 Agent 新鲜度；断联后的旧指标不会继续作为当前值展示。

状态为 AVAILABLE、PARTIAL、UNAVAILABLE；它们描述指标采集，不是 HEALTHY/UNHEALTHY。CPU 或内存高不自动触发重启。

## 部署与维护

Docker 28.5.2 满足最新 cAdvisor v0.60.6 的 Docker 25+ 要求。RTC 使用静态官方 Docker 包，运行时放在 `/opt/avops-docker/28.5.2`，systemd 覆盖配置指定该 dockerd 和 PATH；原系统 RPM 二进制保留用于回退。现有 Agent 使用 Docker API v1.39，Docker 28.5.2 仍兼容这一 API。

[avops-cadvisor.service](deploy/avops-cadvisor.service) 使用官方 v0.60.6 独立二进制，路径 `/opt/avops-monitor/cadvisor`，仅监听 `127.0.0.1:18089`。官方二进制 SHA-256 为 `c381c2c911bc43d465d1e0eaff60f96d58c031c409bddf06da0316fdde8a9296`，来自 GitHub 发布元数据；下载工具 `cmd/fetch-cadvisor` 对 HTTP Range 长度及完整 SHA-256 校验后才生成可执行产物。采样间隔固定 5 秒，启用 cpu、memory、network、disk、diskIO、oom_event；内存上限 256 MiB、CPU 50%，不配置 InfluxDB 或其他存储驱动。

容器部署可选用 [start-cadvisor.sh](deploy/start-cadvisor.sh)，镜像为 `ghcr.io/google/cadvisor:v0.60.6`，发布同一回环端口，日志限制 3×10 MiB。服务和容器二选一，避免重复采集或端口冲突。新的采集器由本系统单独管理；旧 APaaS 采集链路的当前状态见现场记录，不把本次 Agent 上报当作旧 InfluxDB 写入链路的替代验收。

cAdvisor 为获取宿主机 cgroup 和容器文件系统，独立服务使用受控 root 进程及只读文件系统限制；容器方案需要受控 privileged 及本机挂载。数据只供本机采集，不上报原始文件；Docker socket 本身不提供 HTTP 方法级只读能力，`:ro` 挂载也不等于禁止 Docker API 写操作。采集器属于受信任运维组件，不开放公网管理接口。

当前在中心内存与PostgreSQL最新快照中保存指标，中心重启后先标离线/过期并等待新采集，不增加历史曲线、时序数据库或告警规则。后续历史指标可另外接入 Prometheus/VictoriaMetrics。

Docker 升级前须有业务停机授权，按 [upgrade-rtc-docker.sh](deploy/upgrade-rtc-docker.sh) 的前置检查、在线预拷贝、停机一致备份、原 ID 恢复及配置对比执行。[rollback-rtc-docker.sh](deploy/rollback-rtc-docker.sh) 用一致旧数据回退，并保留升级后的目录供诊断；不能只换回旧二进制读取已迁移数据。
