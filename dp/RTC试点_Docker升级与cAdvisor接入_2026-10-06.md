# RTC 试点 Docker 升级与 cAdvisor 接入记录

日期：2026-10-06；下文时间为北京时间。

## 结果

RTC 主机 111.230.192.59 的 Docker Engine 已从 18.09.0 升为 28.5.2，满足最新版 cAdvisor 的 Docker 25+ 要求。原 13 个运行容器全部按原 ID 恢复；镜像、环境变量、Entrypoint/Cmd、工作目录、用户、Healthcheck、挂载、端口、网络、重启策略及资源设置核对一致。Docker 将两个支撑容器的旧 `default` 网络别名规范为等价的 `bridge`，比较时只对此别名做标准化，其余字段严格核对。

本次业务停机及必要重启已获用户明确授权。22:11:38 开始切换，22:13:29 完成停机后一致备份，原容器在 22:13:54–22:13:56 启动；22:17:47 完成配置核对及管理入口收尾。实际容器停机约 2 分多钟，不把后续镜像下载时间算作停机时间。各容器原/新 StartedAt 见 [Docker 升级证明](RTC试点_Docker升级证明_2026-10-06.json)。

独立 cAdvisor v0.60.6 已作为 `avops-cadvisor.service` 部署，使用官方二进制，完整 SHA-256 校验通过。Agent 从 `127.0.0.1:18089` 读取，沿现有 WSS 上报中心。22:32 检查时，8 个 RTC 基线节点及演练节点均为 HEALTHY，资源指标均为 AVAILABLE、stale=false。

业务健康仍按既有 Container/Readiness 检查层级判断；本次未开展真实 RTC 通话黑盒验收。核心业务节点的日常重启白名单仍关闭，只有演练节点开放。

## 当前版本与部署位置

| 组件 | 版本 / 位置 |
|---|---|
| Docker Engine / CLI | 28.5.2，服务端 API 1.51、最低 API 1.24 |
| containerd | 1.7.28 |
| runc | 1.3.3 |
| Docker 运行时 | `/opt/avops-docker/28.5.2`；systemd 覆盖配置 `docker.service.d/avops-upgrade.conf` |
| CLI | `/usr/local/bin/docker`，原系统 RPM 二进制保留 |
| 独立 cAdvisor | v0.60.6，`/opt/avops-monitor/cadvisor`，`avops-cadvisor.service` |
| cAdvisor 来源 | 官方 GitHub Release，SHA-256 `c381c2c911bc43d465d1e0eaff60f96d58c031c409bddf06da0316fdde8a9296` |
| 指标监听 | **仅 `127.0.0.1:18089`**，未开放公网端口 |
| Agent 配置 | `AVOPS_CADVISOR_URL=http://127.0.0.1:18089` |
| 中心/UI | 111.230.108.76；[本机预览](http://127.0.0.1:18086) |

官方容器镜像下载速度较慢，当前采用同版本官方二进制独立服务，和 Agent 分进程管理。镜像拉取已停止，未运行第二个新采集容器。仓库同时提供可选容器部署脚本，但不可与独立服务同时占用 18089。

最终复核发现，旧 APaaS `agora_cadvisor` 于 **22:29:07** 出现 kill/die/destroy 事件并已被移除；本次执行的命令没有删除该容器，事件未提供调用者身份，不推断来源也不擅自重建。当前剩余 12 个 Docker 容器均运行且资源指标 AVAILABLE。新独立服务提供 cAdvisor→Agent→中心链路，旧容器的 cAdvisor→InfluxDB 写入已不再由它提供；不宣称原 InfluxDB 采集链路仍然完整。

## Agent 怎么读取

每次完整 Docker 采集之后（默认每 15 秒），Agent 发一次只读请求：

```bash
curl --fail --max-time 3 \
  'http://127.0.0.1:18089/api/v2.0/stats/?type=docker&count=2&recursive=true'
```

`type=docker` 选择 Docker 对象，`recursive=true` 获取清单，`count=2` 返回最近两次样本。响应 key 如 `/docker/ffc3bb256ab2...`；Agent 按完整 64 位 ID 匹配同周期 Docker 发现对象，不通过名字或标签猜测，也不自动加入业务台账。

| 数据 | 处理方式 |
|---|---|
| CPU | 两次 `cpu.usage.total` 累计纳秒之差 ÷ 实际采样时间差纳秒，得到使用核数 |
| 内存 | 读取 `memory.usage` 与 `memory.working_set`，以字节上报 |
| 文件系统 | 读取 `filesystem[].usage`，按唯一设备汇总，不宣称覆盖挂载卷总占用 |
| 网络 | 同名接口 rx/tx 字节差 ÷ 实际时间差秒数；排除 lo，接口变化或计数器回退则留空 |
| host/shared 网络 | `network_scope=HOST_SHARED`，单容器网络速率为 null，不重复计入主机流量 |

CPU 1.0 表示使用一个逻辑核，不是整机 100%。实际 cAdvisor 调度与磁盘扫描会使间隔超过配置的 5 秒，计算使用 timestamp 的真实差值；本次观察约 5–9 秒，不按固定 5 秒硬算。

Agent 把数值写入 `container.resources`，随原 snapshot 消息经过 WSS/独立 Agent Basic Auth 上报。中心的容器清单/详情和服务节点状态接口返回 resources，React 页面展示 CPU、内存工作集、容器文件系统及可归属的网络速率。原始 cAdvisor 标签、环境变量及其他不必要字段不上传。

实现代码：[采集与换算](../monitor/internal/cadvisor/client.go)、[Agent 接入](../monitor/cmd/agent/main.go)、[中心指标新鲜度](../monitor/internal/center/resources.go)、[前端显示](../monitor/frontend/src/Resources.tsx)。完整说明见 [CADVISOR](../monitor/CADVISOR.md)。

## 实测与边界

- cAdvisor 服务 active，NRestarts=0；接口在本机回环监听。采集器限制 256 MiB、CPUQuota 50%，检查时内存约 14 MiB。
- 中心现有 9 个基线节点均有新鲜资源指标；核心节点 restart_enabled=false，演练节点为 true。
- 最终发现清单为 12 个 Docker 容器，全部资源指标 AVAILABLE；Docker 升级完成时原 13 个都曾恢复，随后旧 cAdvisor 移除是单独观察到的事件。
- 例如 local_ap 的实测内存工作集约 99 MB，CPU 约 0.003 核，文件系统口径约 17.5 GB；数值随时间变化，不作为固定验收阈值。
- host 网络的 RTC 节点均未错误展示单容器网络吞吐。发现页的 bridge 容器使用各自可取得的网络速率；采集器没报告的容器显示不可用，不以 0 冒充测量。
- cAdvisor 尚未启动时，Agent 已上报 CADVISOR_UNAVAILABLE，业务节点仍保持 HEALTHY；启动后自动恢复 AVAILABLE，验证指标失败不破坏 Docker 状态链路。
- 没有新增资源告警、自动重启、历史曲线、时序数据库或个人角色功能；当前只保存最新内存快照。

`go test -race -timeout 60s ./...`、`go vet ./...`、React TypeScript/Vite 构建、脚本语法及 OpenAPI YAML 解析通过。新增测试覆盖完整 ID 匹配、CPU/网络换算、host 共享口径、过期、跨重启样本、计数器回退、cAdvisor 故障隔离和来源限制。模拟浏览器验证了资源指标列及共享网络说明，真实中心接口验证了现场指标。

## 备份、收尾与维护

停机前完成在线预拷贝，停机后执行最终一致同步。备份为 `/var/backups/avops-docker-20261006`（0700），包含旧 Docker 数据目录、配置、私有 inspect 及公开验证结果，当前保留用于回退。私有 inspect 包含原环境变量，禁止上传 Git 或作为普通附件；公开 proof.json 已剔除这些内容。

升级后发现 20220 的运行时防火墙开放规则未保留，已通过原有 22 端口接入并恢复 20220 的运行时/永久规则，20220 再次验证可用；未修改 SSH 鉴权或配置。Agent/中心凭证、通知配置、Key 注册记录、基线及重启账本保留。

主机安装了发行版提供的 jq/oniguruma，用于升级前后 JSON 核对。Docker 静态版本由实施维护，不随原 18.09 RPM 的自动更新切换；原 RPM 仍留作回退材料，实际运行版本以 `docker version` 为准。

升级及回退脚本位于 `monitor/deploy`；本次未执行回退。回退须恢复停机一致的旧数据目录并保留迁移后的目录，不能只把 dockerd 二进制换回 18.09。历史离线包未包含本次升级及指标功能，需按最新源码另行构建。
