# APaaS 私有化监控技术设计梳理（3.10.0）

整理日期：2026-10-04。

分析对象：`shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727`。包内 BUILD_INFO 声明产品版本 3.10.0、架构 x86、生成时间 2026-07-07 07:27:36 UTC，包含 Prometheus 镜像。

本报告依据包内文档、Shell 脚本、Compose、采集配置、Grafana provisioning/JSON 和离线镜像 manifest；抽查 vmagent 镜像配置为 linux/amd64、入口 /vmagent-prod，未声明 VOLUME。没有执行部署脚本、启动容器、修改交付包或连接线上服务。下文区分配置直接证据、组件能力和待运行验证事项。

## 1. 技术定位

这是一套“主机与应用白盒指标采集 + 时序存储 + Grafana 可视化”的离线监控交付包。

业务机本地拉取指标，主动向中心写入；中心提供历史查询和大盘。默认不需要中心 Prometheus，Grafana 直接通过 Prometheus 兼容数据源查询 VictoriaMetrics。

包内未发现 RTC/RTM 业务黑盒探针、应有容器基线、统一节点健康聚合 API、受控重启接口、操作审计、告警规则或通知接收配置。不能因为名称是“监控”、挂载了 Docker Socket，或文档叫“告警排查场景”，就认为这些能力已经实现。

## 2. 架构与数据流

```text
每台 APaaS 业务宿主机

宿主机 CPU / 内存 / 磁盘 / 网络
             |
       node-exporter :9100 ---- /metrics -----+
                                             |
Docker Engine ---- 标签发现 -----------------+--> vmagent :8429
                                             |       |
业务应用 ---- /metrics 或配置的指标路径 -----+       |
                                                     | HTTP remote_write
                                                     v
中心监控机                                  VictoriaMetrics :8428
                                                     ^
                                                     | 查询指标
浏览器 ---------------- Grafana :3100 ----------------+

可选 Prometheus :9090 ---- remote_write -------------> VictoriaMetrics
```

### 2.1 链路分工

| 链路 | 技术方式 | 包内配置证据 |
|---|---|---|
| 主机指标产生 | node-exporter 读取宿主机系统信息 | host 网络、host PID、根目录只读挂载 |
| 本机主机采集 | vmagent HTTP scrape | 127.0.0.1:9100，15 秒间隔、10 秒超时 |
| 应用目标发现 | Docker service discovery | Docker Socket，30 秒刷新，monitor.scrape=true |
| 应用指标采集 | HTTP 拉取应用指标端点 | 默认 /metrics，可由 monitor.path/port 改写 |
| 指标上报 | Prometheus remote_write | VictoriaMetrics /api/v1/write |
| 图表查询 | Grafana Prometheus 类型数据源 | 两个数据源 UID 均指向 VictoriaMetrics |

这里同时使用 Pull 与 Push：vmagent 在本机 Pull exporter/应用指标，再 Push 到中心。remote_write 是指标写入协议，不是下发运维任务的双向 WebSocket，也不是发送业务告警的 Webhook。

## 3. 组件、端口与持久化

| 组件 | 配置版本 | 默认端口 | 部署方式与角色 |
|---|---|---|---|
| node-exporter | v1.11.1 | 9100 | 每业务机一个容器，暴露主机指标 |
| vmagent | v1.143.0 | 8429 | 每业务机一个容器，本机采集、发现和远程写入 |
| VictoriaMetrics | v1.143.0 | 8428 | 中心单节点时序存储 |
| Grafana | 12.0.2 | 宿主机 3100 → 容器 3000 | 中心大盘查询与展示 |
| Prometheus | v2.55.1 | 9090 | 可选 profile，用于兼容/调试，不是默认链路必需组件 |
| Alpine | 3.20 | 无常驻端口 | 临时修复中心数据目录属主 |

中心数据目录：

| 宿主机目录 | 容器目录 | 内容 |
|---|---|---|
| data/victoriametrics | /victoria-metrics-data | 时序指标，显式 retentionPeriod=30d |
| data/grafana | /var/lib/grafana | Grafana 运行数据及其本地数据库 |
| data/prometheus | /prometheus | 可选 Prometheus 本地 TSDB |

30 天是 VictoriaMetrics 指标保留配置，不是操作审计期限，也不是磁盘容量上限。Compose 未显式设置 Prometheus 保留时间/容量或容器资源配额；Grafana 与 VictoriaMetrics 均是单实例，并非高可用集群。

## 4. Agent 侧实现

### 4.1 node-exporter

启动脚本配置 host 网络和 PID 命名空间，将宿主机根目录挂载到 /host，并通过 --path.rootfs=/host 读取系统指标。覆盖 CPU、内存、swap、文件系统、磁盘 I/O、网络和系统运行时间等。

默认监听 :9100，即并不只是 loopback；文档中的 127.0.0.1/metrics 是访问示例。需要网络范围限制，包内没有 exporter 鉴权配置。

脚本支持覆盖镜像、容器名和监听地址；部署时先 docker pull，再删除同名旧容器，重新 docker run，设置 --restart always。重跑脚本是重建监控组件，不是原容器受控 restart，不能直接用于生产业务恢复。

### 4.2 vmagent

部署前先用临时容器执行 prom scrape 配置 dry run；通过后再替换旧 vmagent。运行时使用 host 网络、Docker Socket 和只读 scrape.yml，设置 restart always。

HOST_ID 默认使用 hostname -f，失败回退 hostname。REMOTE_WRITE_URL 支持环境覆盖，但脚本实际默认是固定的内网地址 http://172.17.80.252:8428/api/v1/write，不是 README 中的通用占位符。

主机 job 生成 env=private、role=physical-host、host=HOST_ID、instance=HOST_ID:9100。这里的 role 是指标标签，不是用户权限角色。

### 4.3 应用指标的 Docker 标签约定

| Docker 标签 | 用途 |
|---|---|
| monitor.scrape=true | 开启目标发现及采集 |
| monitor.port | 将发现地址改写成实际指标端口 |
| monitor.path | 覆盖默认 /metrics |
| monitor.job | 生成 job 标签 |
| monitor.service | 生成 service 标签 |
| monitor.env | 生成 env 标签 |

实例标签按 HOST_ID/容器名生成，host 按 HOST_ID 固定。业务指标本身还可能需要 application、status、uri、method 等标签，Docker 标签并不会自动创造这些业务维度。

发现逻辑读取 Docker 元数据，最终动作是抓取指标端点。本包没有配置 cAdvisor、Docker 状态 exporter 或遍历容器 inspect 后上报运行状态的专用 Agent。因此应用指标必须由应用自身埋点/已有 exporter 暴露；这不等于自动获得任意容器的 CPU、内存、退出码和 Healthcheck。

启动 dry run 验证配置能解析，不验证每个业务端点可达或指标名符合大盘。strictParse=false 也不能视为完整配置检查。

### 4.4 断联与缓存边界

vmagent 具备 remote_write 缓冲/重试能力，但本脚本未显式配置 remoteWrite.tmpDataPath、磁盘缓存容量、宿主机缓存目录或恢复策略。不能据此承诺容器被替换后待发指标不丢失。

需要运行验证其实际默认缓存位置、上限、积压指标和长期断网行为。进程拉起、容器重建与宿主机持久化是三个不同层次；--restart always 只提供 Docker 进程级恢复，不等同于数据可靠性或宿主机修复。

## 5. 中心存储与可选 Prometheus

### 5.1 VictoriaMetrics

中心通过 Compose 暴露 8428，提供 remote_write 写入和 Prometheus 兼容查询。Grafana 使用它作为实际查询后端，所以无需再建一个持续抓取所有业务机的中心 Prometheus。

存储显式挂载到宿主机，重建中心容器可保留挂载数据；但包中没有备份、磁盘配额、集群副本、写入鉴权或 TLS 配置。它保存的是指标时间序列，不是容器基线、操作任务和审计事务。

### 5.2 Prometheus

Compose 的 prometheus profile 默认不启动，通过 --with-prometheus 或兼容的 profile 参数启用。

该组件每 15 秒采集自身 prometheus:9090 和一个 demo-service（host.docker.internal:16101），然后也写入 VictoriaMetrics；没有规则文件或 Alertmanager 配置。Linux 目标机对 host.docker.internal 的解析需要验证，Compose 中未提供 extra_hosts。

“离线包包含 Prometheus”与“正常部署运行 Prometheus”是不同事实。两个 Grafana 数据源即使名为 prometheus，也没有指向这个可选 Prometheus。

## 6. Grafana 数据源与仪表盘

### 6.1 自动 provisioning

| 数据源 UID | 类型 | 实际查询 URL | 对应大盘 |
|---|---|---|---|
| prometheus | prometheus | http://victoriametrics:8428 | Infrastructure |
| vm-single-apaas | prometheus | http://victoriametrics:8428 | Business |

数据源使用 access=proxy，即浏览器通过 Grafana 后端查询，不直接连业务机。UID 预绑定到大盘 JSON，解决环境切换后手工选数据源问题。

大盘按本地文件 provisioning 自动导入，刷新文件周期为 30 秒，允许编辑及删除。这个 30 秒是模板文件扫描周期，不是所有图表的数据刷新周期；node 大盘 refresh=1m，两个业务大盘 refresh 为空。

### 6.2 三套内置大盘

| 大盘 | 对象与代表指标 | 标签/口径要求 |
|---|---|---|
| Node Exporter Full | CPU、内存、磁盘、网络、系统、exporter 自身指标 | job、instance、nodename；依赖实际 collector 和系统能力 |
| 灵动会议应用监控 | JVM、GC、线程、HTTP 请求、5xx、延迟、进房间指标、Logback 计数 | application、env、instance、status、uri 等，需应用埋点 |
| Conference API | API QPS、P99/P90/P50、请求大小、Go 内存/CPU/goroutine | service 默认 conference-service；path、method、code 等 |

递归分析 JSON 得到 140、45、11 个 panel 节点（含 row），274、69、12 条不同查询表达式。这是模板规模，不表示实际部署时全部图表都会有数据。JVM/Micrometer 和 Go 埋点栈由指标名推断，应用源码与实际 /metrics 输出不在本监控包里。

### 6.3 指标是业务观测，不是黑盒合成验证

HTTP 请求成功率和延迟来自已有请求的埋点。没有流量时可能无数据；它没有主动加入 RTC 频道、发布接收媒体或收发 RTM 测试消息。因此即使有“进房间 SLA”图，也不能称为独立业务黑盒探针。

Logback events 图只是日志事件计数；包中没有日志正文采集、存储或检索组件。

## 7. 部署、离线和自运维

中心先启动 VictoriaMetrics/Grafana，再在业务机启动 node-exporter 和 vmagent。中心脚本兼容 Docker Compose v2 与旧 docker-compose，创建数据目录，用 Alpine 临时容器修复 Grafana UID/GID 472:472，Prometheus 启用时修复 65534:65534。

Grafana 的初始账号仅对新的本地数据库生效；修改环境变量不会重置已有密码。Compose/脚本保留默认管理员口令并只警告，未强制首次配置。

Grafana 使用环境变量关闭插件预安装、更新检查、遥测和新闻，以适应离线场景。包内存在 grafana.ini，但 Compose 没有将它挂到配置路径；当前实际能依赖的是环境配置，具体开关被 Grafana 12.0.2 识别的结果需运行验证。

镜像导入脚本只执行解包与 docker load，没有 retag。导出脚本仍需要原仓库 release/Makefile 完成完整包上传流程，该构建入口未包含在本包中；客户离线运行不应依赖其 OSS 上传步骤。

部署与排障说明按 Grafana → 数据源 → VictoriaMetrics → vmagent → exporter/应用端点逐层定位，适合借鉴。脚本中的强制删除、递归清理和 Grafana 重新初始化属于具体运维操作，不应直接作为 RTC 系统“安全重启”的实现。

## 8. 配置核对发现与技术缺口

以下是静态核对发现，原包没有被修改，也未通过实机测试宣布修复。

| 项目 | 已确认的证据 | 影响与建议 |
|---|---|---|
| 离线镜像标签不一致 | archive manifest 六个镜像仅带 hub.agoralab.co/adc/docker-io 前缀；Compose/Agent 脚本使用无前缀上游标签，load 脚本不 retag | 导入后启动引用的标签仍可能不存在；统一引用或明确 retag，再做断网部署验收 |
| node-exporter 强制 pull | deploy.sh 在启动前无条件 docker pull | 即使本地有镜像，完全离线环境也可能在拉取处失败；本地检查优先，离线不强制 pull |
| 固定 remote_write 默认地址 | vmagent 脚本默认 172.17.80.252 | 未配置容易发错中心；改为必须配置或明确环境模板 |
| vmagent 缓存未显式持久化 | 只有 scrape.yml 与 Docker Socket 挂载，无缓存目录或磁盘上限参数 | 不承诺重建后缓存恢复；补持久缓存/额度及断联演练 |
| 未交付告警/通知链路 | 无 vmalert、Alertmanager、规则、Grafana alert provisioning 或 Webhook 配置；模板无旧式 panel alert | Grafana 产品支持告警，不等于包已实现通知；告警要单独设计 |
| 没有应有节点基线 | Docker 标签发现仅选择当前发现的采集目标 | 已删除/退出目标可能消失，不直接提供 MISSING/应有节点数量；另加明确基线 |
| 没有受控恢复入口 | 仅部署/排障脚本，无任务、白名单、幂等/审计接口 | 不能直接替代 RTC 运维控制服务 |
| 默认无指标通道认证/TLS | node-exporter、vmagent、VM 为 HTTP，未配置访问认证；Grafana 登录只保护其页面 | Grafana 登录不能保护 8428 写入或 8429/9100；按网络/网关落实边界 |
| Docker Socket 的 ro 非 API 权限隔离 | Socket 文件以 ro 挂载 | ro 挂载不能保证 Docker API 只能读；按实际权限/代理限制设计 |
| 资源、容量与健康检查未配置 | 无显式 CPU/内存上限、VM 磁盘额度、Compose healthcheck；restart always 和 depends_on 已配置 | 不等于就绪确认或容量保护，补运行测量与故障行为 |
| grafana.ini 未挂载 | Compose 只挂 data、provisioning、dashboards | 不应把 ini 文件存在当成配置已生效；核对环境变量或挂载 |

### 8.1 业务查询口径问题

1. 灵动会议“HTTP - 进房间 SLA”：两条查询的分子和分母都是同一入口、同一 status=2xx/4xx 的计数。在分母非零时结果为 100，不能区分失败率；无请求时可能无值/NaN。应确认真正的成功数/总请求数和有效状态定义。
2. “HTTP - 平均延迟”使用 avg(http_server_requests_seconds_max)，还带 >0.01 过滤，计算的不是按请求加权的平均耗时。平均耗时通常按同一指标族的请求耗时 sum/count 计算；当前另一张 AVG 图的分子/分母前缀也不同，需核对实际埋点。
3. 灵动会议成功率把 4xx 计入成功；是否合理要按业务定义决定，不能直接理解为“业务调用成功”。部分失败数未带 instance 条件，可能混入其他实例。
4. Conference API 的 QPS 查询没有 service 筛选，其他面板有；可能混算多个服务。code!=200 被视为非成功，会包含 201/204 等，需要核对协议语义。
5. 大盘混用 ad_scenario_*、apaas_apaas_* 和通用指标前缀，vmagent 配置没有对应的 metric relabel 改名规则；可能是历史埋点，也可能导致无数据，必须对照真实 /metrics，不能仅从 JSON 判定指标不存在。

## 9. 与 RTC 首期方案的关系

| 能力 | APaaS 包 | 已确认 RTC 试点 |
|---|---|---|
| 采集形态 | exporter + vmagent，本机拉取后 remote_write | 自有 Agent，状态快照与受控任务通过主动长连接 |
| 主机/应用指标 | 已有多类指标大盘，依赖应用 /metrics | 首期先做 Docker/Readiness 状态，指标扩展后置 |
| 当前节点健康 | 指标和 scrape 成功信号，不是完整基线 API | 应有节点、MISSING、UNKNOWN 与服务聚合 |
| 历史存储 | VictoriaMetrics 30 天指标时序 | 操作/幂等可靠恢复，存储选型与时间序列用途不同 |
| UI | Grafana 查询大盘 | 可运行 Demo，含状态和重启结果 |
| 通知 | 包内未配置 | 已确认单 HTTP/HTTPS 地址状态通知 |
| 运维控制 | 未提供受控重启链路 | 原容器重启、白名单、幂等、并发和审计 |
| 业务验证 | 流量埋点观测，无合成探针 | 黑盒 V1.1 独立实施 |

### 9.1 值得复用的部分

- 每机采集、中心存储的部署分界和明确的端口/数据目录。
- 配置 dry run、稳定 host/instance/service 标签和模板 UID 固定。
- Grafana 自动 provisioning、离线大盘和插件联网控制思路。
- VictoriaMetrics 作为未来指标历史后端，或复用客户已有指标平台。
- 数据源、网络、上报、采集端逐层排查的交付文档结构。

### 9.2 不应直接替代的部分

vmagent 不是 RTC 重启任务 Agent，VictoriaMetrics 时序写入也不是操作事务/幂等数据库。Grafana 查询大盘不能自动生成 RTC 的节点基线、业务状态规则和受控重启；其产品通知能力也不能替代未配置的通知方案。

对当前两机 RTC 试点，建议保留既定 Agent/API/Basic Auth/HTTP 通知/Demo 的核心链路；如需要历史指标，再增加 Prometheus 格式指标出口，复用 vmagent → VictoriaMetrics → Grafana。是否引入这套栈应按资源和实际需求评估，本报告不自动修改 RTC PRD 或增加组件依赖。

## 10. 核验建议与证据索引

后续运行验证应覆盖：全断网镜像导入及启动、正确的中心地址、Docker 标签发现与真实 /metrics、稳定实例标签、HTTPS/鉴权边界、中心断联积压、vmagent 重建恢复、磁盘容量、Grafana UID 绑定和三个业务口径问题。

| 证据 | 包内文件 |
|---|---|
| 交付包声明及部署边界 | [BUILD_INFO](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/BUILD_INFO.txt)、[README](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/README.md) |
| 主机采集及强制 pull | [node-exporter deploy.sh](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/agent/node-exporter/deploy.sh) |
| remote_write、权限、无持久缓存挂载 | [vmagent deploy.sh](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/agent/vmagent/deploy.sh) |
| 间隔、Docker 标签发现与 relabel | [scrape.yml](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/agent/vmagent/scrape.yml) |
| 中心镜像、端口、数据与保留 | [docker-compose.yml](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/docker-compose.yml) |
| 权限修复及 Prometheus profile | [中心 deploy.sh](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/deploy.sh) |
| 兼容调试采集 | [prometheus.yml](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/prometheus/prometheus.yml) |
| 数据源 UID 与文件扫描 | [datasources](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/grafana/provisioning/datasources/victoriametrics.yml)、[dashboards](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/grafana/provisioning/dashboards/default.yml) |
| 主机图表 | [Node Exporter Full](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/grafana/dashboards/infrastructure/node-exporter-full.json) |
| JVM/HTTP 及 SLA 公式 | [灵动会议应用监控](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/grafana/dashboards/business/lingdong-meeting-monitoring.json) |
| Go/Conference 指标与筛选 | [Conference API](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/stack/victoria-metrics-grafana/grafana/dashboards/business/conference-api.json) |
| 离线镜像与实际 RepoTags | [导出脚本](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/scripts/export-images.sh)、[导入脚本](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/scripts/load-images.sh)、[镜像归档](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/monitor-images.tar.gz) 内 images.txt / manifest.json |
| 手册与排障链路 | [部署文档](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/部署文档.md)、[排障场景](shengwang_apaas_private_monitor_3.10.0_x86_20260707_0727/TROUBLESHOOTING-SCENARIOS.md) |
