# RTC 运维监控 V1.0

2026-10-07 已切换 PostgreSQL 17.11 存储，并增加账号注册、登录、角色权限、账号开通/停用和改密。账号安全记录保留在数据库内部，不提供独立“系统审计”页面；用户可见审计集中在操作与 Key 记录。初始账号为 admin，密码在本机私有交付文件 `/Users/hanxiaoqing/.cache/avops-rtc-pilot/postgres/account-handoff.txt`；不在文档展示。详情见 [PostgreSQL 与权限](POSTGRESQL.md)。下文早期文件存储/Key 交付记录保留历史，当前正式存储以该说明为准。

2026-10-05 已实现并部署 RTC V1.0：真实采集、Docker 启动配置、React 筛选生成 YAML、健康查询、人工原容器重启、可靠幂等/任务恢复/审计和单 HTTPS Webhook 状态通知。旧 `v1_api_prototype` 保持独立 Mock，不作为现场数据来源。

试点 Agent 在 111.230.192.59，中心/React UI 在 111.230.108.76，通知接收端在 114.132.190.201。用户已保存的 8 个节点及基准版本完整保留；实际 RTC 核心节点未开放重启。完整链路使用独立 avops-v1-canary 验收，没有停止/重启既有业务容器。运行及维护细节见 [OPERATIONS.md](OPERATIONS.md)，实际证据见 [V1.0 验收记录](../dp/RTC试点_V1.0_完整功能验收记录_2026-10-05.md)。

## 使用页面

2026-10-08 客户运维新增受控重启和“我的重启”，必须持绑定本人的独立Key；全局操作审计/台账/Key签发仍仅Agora。通知页新增PG容量与磁盘余量提示，后台每小时清理过期超过24小时的会话。故障处理见 [RECOVERY](RECOVERY.md)，当前实施及边界见 [缺口补齐记录](../dp/客户重启与会议缺口补齐_2026-10-08.md)。最新离线包为 build/release/monitor-1.0.1-linux-amd64.tar.gz。

2026-10-08 页面完成 Ant Design 5.29 组件化：Layout/Tabs/Dropdown 导航，Form/Input/Select/Radio/Checkbox/Switch 表单，Table/Pagination 数据列表，Card/Statistic/Descriptions/Progress 资源概览，Collapse/Tabs/Timeline 资源详情，以及 Modal/Alert/Popconfirm 反馈。旧原生控件样式已清理；仅保留异常边界的原生恢复按钮。详细验证见 [完整组件改造记录](../dp/运维监控_AntDesign完整组件改造_2026-10-08.md)。

当前本机预览：[运维监控](http://127.0.0.1:18086)。入口由本地 viewer 转发至中心 HTTPS，并验证 `edge.rtcdevelopers.com` 证书；不修改 DNS、不跳过验证、不注入凭证。页面使用账号登录，凭证不写入仓库或网页。

页面只提供账号/密码登录，注册默认客户运维，提交后由 Agora 人员开通；角色固定为 Agora 人员 / 客户运维，不提供自定义角色。登录后使用可撤销 Bearer 会话。Agent 和受控机器接口继续使用各自 Basic 凭证，不能作为页面登录方式。凭证仅在当前页面内存，刷新须重新登录；所有数据/配置接口继续受后端鉴权、角色权限及来源限制保护。

预览进程停止后，在本目录运行：

```bash
go run ./cmd/viewer
```

中心来源 IP 白名单按用户要求暂时关闭（`AVOPS_ALLOWED_IPS=`）；账号/RBAC、Agent身份、独立重启Key、TLS及同源写入仍生效。正式交付按管理网络重新确认来源条件。现场 SSH 禁用端口转发，因此 viewer 使用直接 HTTPS。公网主入口是 `https://111.230.108.76:19443`，证书 ServerName 为 `edge.rtcdevelopers.com`，直接用浏览器打开 IP 会出现域名不匹配；正常远程访问应落实证书域名解析。

页面标题“运维监控”；容器发现按 IP、名称、镜像和运行态筛选，多选后只填写集群编码和可选多个 TCP 端口。服务归属自动生成，已有归属保留；保存生效并下载 YAML。已纳管节点可单独配置多个端口，通知页面可编辑 HTTPS Webhook 地址并即时保存。

2026-10-06 提供 Agora 人员使用的“接入密钥”页。Agora 人员直接签发重启安全 Key，Secret 仅显示一次并支持复制，可停用。重启 Key 单独填写在重启/未知结果核实框。签发不绕过中心/Agent 白名单。退出登录清空当前会话，凭证不写浏览器存储。

RTC 主机现保留独立演练容器 avops-v1-canary：现场停止后经注册 Key 和双端规则恢复成功，原 8 个业务节点依旧禁重启。台账现为 9 个节点的新版本，既有成员绑定保留。详细时间、拒绝场景及操作 ID 见 [真实重启与接入密钥验收](../dp/RTC试点_真实重启与接入密钥验收_2026-10-06.md)。

2026-10-06 追加 cAdvisor 资源指标：独立采集器仅提供本机 HTTP，Agent 按完整 Docker ID 读取两个样本并换算 CPU/网络速率，沿现有 WSS 上报；服务节点及容器发现页展示最新资源数据。请求 URL、字段和失败处理见 [CADVISOR.md](CADVISOR.md)。本次 Docker 18.09→28.5.2 升级已获得必要业务停机授权，原 13 个容器按原 ID 恢复，启动配置/挂载核对一致；这次升级不开放核心业务的日常重启白名单。

资源扩展已增加 CPU 节流/配额、内存限额占比/RSS/缓存/Swap/峰值、OOM、每设备磁盘 I/O/IOPS、网络包/丢包/错误、Docker 数据盘容量/inode 和容器生命周期。页面展开“资源与生命周期详情”查看，未限额、未知、计数重置及共享口径分别说明。现场验证见 [扩展指标记录](../dp/RTC试点_资源指标与生命周期扩展_2026-10-06.md)。

## 实现与边界

| 部分 | 实现 |
|---|---|
| Agent | Docker Unix API v1.39（兼容 Docker 18.09）；all=true 清单与 inspect；仅批准的原容器可通过 restart API 操作，不执行任意命令/重建 |
| 采集周期 | 默认 15 秒，配置范围 10–60 秒；中心新鲜度 45 秒；采集失败不刷新数据、不把部分清单当作缺失证据 |
| 通道 | Agent 主动 WSS + 独立 Basic Auth；Agent ID/主机由中心登记绑定；会话和序号防旧连接覆盖 |
| 启动配置 | 镜像/ID、Entrypoint/Cmd、Path/Args、环境、目录/用户、挂载、端口、网络/重启策略；指纹、时间、脱敏/不完整标记 |
| 页面/API | 账号登录采用可撤销 Bearer 会话和后端 RBAC，所有数据/写入接口受保护；来源限制、跨站防护及文本渲染 |
| 保存 | 明确 additions/removals、expected_revision、容器身份/配置快照校验；落盘后立即生效，失败保留旧基准 |
| 存储 | PostgreSQL 事务保存当前/历史台账、操作/审计、Key、通知、账号/角色/会话、最新脱敏快照；结构化索引与 JSONB 扩展字段 |
| 恢复 | 重启加载最后成功的基准；最新发现/启动快照从 Agent 重建，首份有效报告前 UNKNOWN |
| 状态 | 实际容器、已有 Healthcheck；选配本机 TCP，连续 3 次失败/2 次成功；容器消失保留 MISSING |
| 重启 | 两端持久任务、执行意图落盘、精确 ID/配置核验、60 秒复查；同节点互斥、同键复用、未知结果锁定及现场核实 |
| 审计 | 已鉴别系统来源与自填 operator 分开记录，API/React 页面可查看阶段、证据和结果 |
| 通知 | 独立 HTTPS Webhook 目标、TLS 校验、独立认证、固定事件 ID 重试、持久待发、容量及失败可查；不跟随重定向 |
| React | React 19 + TypeScript + Vite；离线静态资源嵌入 Go 中心，无运行时 CDN 或 Node 依赖 |

环境变量值默认全部隐藏；命令参数值默认隐藏，只有 `AVOPS_PUBLIC_ARGS` 明确允许的非敏感标志值保留。敏感名称即使误入允许列表也隐藏；URL 凭证、查询值和片段隐藏，shell `-c` 内容不上传。`AVOPS_PUBLIC_ENV` 默认为空，不应包含任何凭证。不传原始 inspect、标签或挂载文件内容。脱敏配置指纹不承诺检测敏感值变化，也不用于还原部署脚本。

当前每周期上报完整的脱敏启动配置，保证中心重连后可重建；按指纹只发差量是后续优化。单 Agent 512 容器、单配置 64 KiB、单报告 16 MiB，超限/读取失败明确标注，不静默截断全量发现清单；基准最多 2048 节点，提交记录 2 MiB。PostgreSQL保留台账历史，可分页查看；未提供自动历史版本回滚。

新纳管节点 `restart_enabled=false`。重新归类/绑定发生变化不继承重启许可；执行中/结果未知的节点不可移出或改绑定。开放权限需要部署配置中的精确 restart_rules 和批准依据，并与 Agent 本地规则一致；选择页面不能授权重启。

TCP 不从 EXPOSE 自动推导。页面可输入现场确认的本机 TCP 端口，API 支持最多 8 个本机检查、超时 1–5000ms。纯 UDP 服务不配置 TCP 作为业务证明。

## 接口

账号接口受 Bearer/RBAC 保护；机器接口按用途使用 Basic，Agent凭证不能访问管理接口。中心来源限制当前按用户要求暂关。

| 方法与路径 | 用途 |
|---|---|
| GET /api/v1/agents | 机器下拉筛选；在线/新鲜度，不含凭证 |
| GET /api/v1/containers | 发现清单；agent_id/name/image/runtime_status/page/page_size（1–200） |
| GET /api/v1/containers/{agent_id}/{container_id} | 启动配置详情 |
| GET /api/v1/baseline | 完整基准成员及版本，含缺失/离线节点 |
| POST /api/v1/baseline/selection | 按版本保存明确增删，立即生效 |
| GET /api/v1/baseline/yaml?revision=... | 下载所见生效版本；版本变化返回 409 |
| GET /api/v1/services/status | 实际纳管服务/节点状态 |
| POST /api/v1/operations/restart | 异步受控原容器重启 |
| GET /api/v1/operations、GET /api/v1/operations/{id} | 最近操作及单次状态；active=true 返回全部锁定操作 |
| GET /api/v1/operations/{id}/audit | 持久阶段审计 |
| POST /api/v1/operations/{id}/resolve | 有现场证据后关闭未知结果，保持健康事实独立 |
| GET /api/v1/notifications/status | 投递队列、失败、丢弃及最近事件 |
| GET /health/live、GET /health/ready、GET /version | 中心自身状态及当前能力 |

Agent 握手使用独立密钥；消息包含 checks/snapshot、PREPARE/EXECUTE/QUERY、task_result/result_ack。类型见 [model](internal/model/operations.go)，接口见 [OpenAPI](../v1_api_prototype/openapi.yaml)。执行前先记录准备，确认同一 Agent 账本的未执行阶段后才发送执行；未知执行不重试 Docker。

## 构建与部署

Go 1.21+，依赖固定版本 gorilla/websocket、yaml.v3；运行二进制不依赖公网或 Node。

React 源码在 [frontend](frontend/package.json)。修改前端后执行 `npm ci && npm run build`，产物进入 web/static 并随中心编译；现有静态产物可直接构建 Go。

```bash
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o build/center ./cmd/center
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o build/agent ./cmd/agent
```

本机环境曾出现 GOROOT 指向 Go 1.24 而 go 为 1.21 的不匹配，验证使用 `GOROOT=/usr/local/go /usr/local/go/bin/go`。不修改用户的全局 Go 配置。

通过 `go run ./cmd/provision -out /绝对路径/私有配置目录` 为新环境生成独立凭证，既有文件拒绝覆盖。该工具输出试点模板，其他环境按实际机器/域名调整。Agent 登记、secret、host_name、host_address 位于中心私有 YAML；增减 Agent 当前需要重启中心，日常基准保存不需要重启。生成材料不能提交 Git。

中心：`avops-center.service`，专用 avops-monitor 用户，MemoryMax=256M、CPUQuota=50%，HTTPS 19443；附加 HTTP 18084 仅回环。Agent：`avops-agent.service`，root 读取本机 Docker Socket，MemoryMax=128M、CPUQuota=25%，不开放入站端口。安装材料见 [deploy](deploy/install-center.sh)，不停止或重启任何业务容器。

数据目录 `/var/lib/avops-monitor` 0700；凭证 `/etc/avops-monitor/*.env` root 0600，中心登记及私钥 root:avops-monitor 0640。PostgreSQL保存最新脱敏快照，恢复后先标离线/过期，基准不含启动秘密。目录同步后发生异常时尝试回滚；回滚失败关闭就绪和后续写入，需运维修复，不声称所有故障下均能自动恢复。

## 验证

已通过 Docker Unix Socket 兼容采集/停止容器/部分失败、脱敏、同名跨机器、基准并发冲突、候选变化/离线、保存失败、缺失成员保留、文件锁、损坏数据拒绝及恢复、鉴权/跨站保护、WSS 身份隔离、TCP 防抖测试。多机器用模拟 Agent 验证；现场只有 RTC 单台 Agent，不以此宣称已实测多台 RTC。

浏览器验证需本机 Node、Playwright 和 Chrome，默认不纳入纯 Go 单元测试：

```bash
# NODE_PATH 指向本机 Playwright 安装目录；AVOPS_CHROME_PATH 可指定 Chrome 路径。
AVOPS_BROWSER_TEST=1 go test -v ./internal/center -run TestBrowserSelectionWorkflow
```

React 已在内置浏览器的独立模拟环境验证登录、筛选、归类、保存、过滤不误删及四个页面入口；真实重启/通知通过隔离 Docker 容器验收。历史记录保持原时间事实，当前结论以 V1.0 验收记录为准。
