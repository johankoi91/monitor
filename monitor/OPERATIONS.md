# V1.0 安装、配置与维护

2026-10-07 中心正式采用 PostgreSQL 17.11 与账号/RBAC。新增安装先生成 pgprovision 私有材料、启动数据库并用 owner 执行 dbmigrate，再用受限 runtime 配置启动中心。当前流程与密钥/备份说明以 [POSTGRESQL](POSTGRESQL.md) 为准。

首期正式支持已核对的 Linux x86_64、Kylin V10、Docker 18.09+、systemd 243+。中心和 Agent 为 Go 静态二进制；React 资源嵌入中心，运行时不访问 CDN、不要求 Node，也不依赖公网镜像拉取。ARM 未现场验收，不承诺首期交付。

## 配置与首次安装

为新环境生成私有配置：`go run ./cmd/provision -out /私有目录`。这属于部署材料生成器，不提供产品 CLI 管理能力。生成器不会覆盖既有密钥；实际机器、地址、来源和证书按环境修改。

中心私有 YAML 示例（实际凭证不应放进源代码或交付公共包）：

```yaml
site:
  code: rtc-site
  name: RTC 私有化站点
agents:
  - id: rtc-host-01
    secret: REPLACE_WITH_INDEPENDENT_RANDOM_SECRET
    host_name: rtc-host-01
    host_address: 192.0.2.10
restart_rules: []
notifications:
  enabled: false
  url: https://receiver.example.com/api/v1/notifications
  tls_server_name: receiver.example.com
  auth_scheme: webhook_hmac
  secret: REPLACE_WITH_WEBHOOK_SIGNING_SECRET
  timeout_seconds: 10
  max_attempts: 4
  max_pending: 1000
```

中心环境必填 AVOPS_ADMIN_ID/SECRET、AVOPS_CENTER_CONFIG、AVOPS_DATA_DIR、AVOPS_TLS_CERT/KEY；公网监听 AVOPS_CENTER_ADDR=0.0.0.0:19443，AVOPS_ALLOWED_IPS 为批准的 IP 列表。可选 AVOPS_CENTER_LOCAL_ADDR 只接受回环，供现场访问；当前试点使用 TLS 校验的 viewer，SSH 不支持端口转发。

Agent 必填 AVOPS_AGENT_ID/SECRET、WSS 的 AVOPS_CENTER_URL，配置 AVOPS_AGENT_DATA_DIR=/var/lib/avops-agent；可选 AVOPS_TLS_SERVER_NAME 仅调整验证域名，不关闭证书校验。Docker Socket 默认 /var/run/docker.sock。AVOPS_COLLECT_SECONDS=15（10–60），AVOPS_PUBLIC_ENV 默认为空，AVOPS_PUBLIC_ARGS 只列经过确认的非敏感标志值。Agent 不开放入站端口。

中心安装材料目录包含 center、center.env、center.yaml、server.pem、server-key.pem、avops-center.service，执行 [install-center.sh](deploy/install-center.sh)。Agent 目录包含 agent、agent.env、avops-agent.service，执行 [install-agent.sh](deploy/install-agent.sh)。密钥/凭证独立交付；不要把模板占位符当作正式凭证。

可选 cAdvisor 资源采集：独立容器只发布本机 127.0.0.1:18089，Agent 设置 `AVOPS_CADVISOR_URL=http://127.0.0.1:18089`，重启 Agent 生效。Agent 通过 `/api/v2.0/stats/?type=docker&count=2&recursive=true` 读取最新两个样本，按完整 ID 关联当前 Docker 清单，沿现有 WSS 上报。详见 [读取、换算和部署说明](CADVISOR.md)。

当前独立 systemd cAdvisor 服务启用 cpu/memory/network/disk/diskIO/oom_event，Agent 并行读取 v2 stats 与 /metrics（仅 OOM 数字），总超时 3 秒。Docker inspect 同时提供资源限额和生命周期；`AVOPS_DOCKER_DATA_ROOT` 默认 /var/lib/docker，用于 statfs 的容量/inode。更改该路径须对应实际 Docker 数据目录。OOM 计数从采集器启动以来累计，未取得 I/O 设备计数时不显示假零，映射盘及物理盘分开显示。

## 重启规则

客户运维可从服务状态发起受控重启，并在“我的重启”查看自己的最近结果；全局操作/审计、未知结果核实及Key签发仍仅Agora。客户须用绑定本人账号的Key，签发可选择已开通客户账号，双端白名单和冷却/互斥不变。升级首次撤销客户旧会话，重新登录取得新权限。

2026-10-08 用户要求暂时关闭中心来源 IP 白名单，当前试点 `AVOPS_ALLOWED_IPS=` 为空。账号会话、角色权限、Agent 身份、独立重启 Key、HTTPS 证书校验及同源写入限制继续生效。通知接收端来源限制独立配置，未改动。后续恢复中心来源限制时填写明确批准的 IP；变更前配置在中心独立 `/var/backups/avops-source-disabled.*` 目录保留。

已登录 Agora 人员在“接入密钥”直接签发重启安全 Key。重启安全 Key 只允许明确节点范围的重启、结果/审计查询和未知结果核实，不能登录控制台。重启确认框必须另填 RESTART Key ID/Secret。

Secret 仅签发响应返回一次，由管理端安全交付；丢失则重新签发并停用旧 Key，不支持找回。页面输入只保存在当前内存，退出/刷新须重新登录，不写浏览器存储。停用阻止后续请求，不会撤销已受理的执行。凭证签发不打开物理节点规则；核心单点仍须研发批准。

签发、停用保存在中心 access-keys.jsonl（0600，16 MiB 上限），只存 Secret SHA-256 哈希。来源/签发依据和 Key ID 可追溯，不是个人身份认证。离线备份及升级恢复须包含该文件。Agent/通知凭证独立配置，不走这个页面。

默认所有新增成员禁止重启；唯一 AP 等核心单点在研发确认前不配置批准项。开放时中心及 Agent 本地规则都包含以下精确绑定，Agent 文件路径由 AVOPS_RESTART_RULES_FILE 指定：

```yaml
- agent_id: rtc-host-01
  container_name: exact_container_name
  service_code: rtc-web-edge
  image: registry.example.com/rtc/web:EXACT_VERSION
  approved: true
  approval_ref: 实际研发确认或交付依据编号
```

规则不能使用前缀或通配；镜像不匹配时拒绝。中心启动将批准规则应用到已有基准并持久化权限变化；新选择仍为 false，页面不能直接授权。增减 Agent/批准规则需要重启对应服务；日常台账、端口和通知地址保存立即生效。部署前须确认没有执行中或未知操作。

可重启状态为存在且配置完整的 running/exited/dead；paused/restarting/unknown、失联、过期、缺失和身份变化均拒绝。状态 UNHEALTHY 本身不一概禁止原容器恢复。中心全局最多 16 个占用节点的操作，Agent 最多 2 个执行；同节点互斥，完成后冷却 120 秒。

流程为持久受理 → Agent 持久准备且核验身份 → 中心持久执行意图 → Agent 持久 EXECUTING → 精确 ID 的 Docker restart（停止宽限 10 秒，仅一次请求）→ 执行证据 → 60 秒复查。准备/执行证据窗口 40 秒；没有发送执行的准备超时可明确拒绝，已可能执行的超时保留节点锁。

SUCCESS 只表示原容器身份/配置不变、实际启动时间更新，且当前配置的检查通过。容器退出后 TCP 恢复需两份新的有效成功证据。UNKNOWN/UNCERTAIN 不重试 Docker；人工在现场核查后，通过页面填写结论、原因和证据关闭操作锁，记录 MANUALLY_CLOSED，不冒充业务恢复或 SUCCESS。

## 通知

当前以 [WEBHOOK.md](WEBHOOK.md) 为准：原始体双 HMAC 签名；200 JSON 且 10 秒内确认；3 次重试；启用/变更前健康检查。页面可生成/复制签名密钥，并测试已保存目标。接收端通知 POST 不使用 Basic，诊断 GET 保留独立 Basic。

通知页面按角色保存 HTTPS Webhook 地址、开关、TLS 身份及独立鉴权；配置进入 PostgreSQL 加密文档并优先于 YAML 初始值，重启保持。新地址/TLS 身份不能保留旧密钥，原目标未完成投递终止，新事件使用新版本。GET 不返回密钥。

中心仅对已纳管节点状态改变发送 JSON，不发送服务聚合通知，接收方按 cluster + node_id 定位；node_id 为主机 IP-容器名。初次观察建立比较基准，持续同状态不重复创建事件，显式移出不发伪恢复。Agent 失联/采集过期由中心每秒扫描识别，不依赖客户查询。

通知独立认证，不转发 API/Agent 凭证；固定 10 秒超时、最多 4 次投递（3 次重试），重试间隔 0/1/5 秒、队列最多 1000。发送前持久记录尝试，同一事件重放保持 ID 和载荷；跨中心重启最多继续剩余尝试。仅 HTTPS 可配置，验证证书，禁止跟随重定向。

试点目标 https://114.132.190.201:18443/api/v1/notifications，TLS ServerName=edge.rtcdevelopers.com，认证使用独立 Webhook HMAC 签名密钥。URL 和 ServerName 配成一组，不忽略证书错误。投递失败/容量丢弃在通知页面可见，轮询仍提供最新事实。

## 数据、容量与故障

中心新增容量API及页面提示、过期会话小时清理，配置与范围见 [POSTGRESQL](POSTGRESQL.md)。数据库配额不沿用旧JSONL参数；提示默认1 GiB，数据盘余量不足2 GiB或10%提示。通知/审计/操作记录仍无自动归档，不把队列1000条限制当作历史数据上限。

| 数据 | 默认路径/容量 | 行为 |
|---|---|---|
| 基准 | PostgreSQL documents/baseline_versions/nodes，单提交 2 MiB，最多 2048 节点 | 定义/YAML/版本/节点同一事务，保留历史 |
| 操作/审计 | PostgreSQL operations/operation_audit | 提交后下发；唯一 request_key/锁约束，失败拒绝新任务 |
| 通知 | PostgreSQL notification_*，设置秘密 AES-GCM 加密 | 比较事实/待发/投递结果持久保存，旧目标规则保留 |
| 账号/Key | PostgreSQL users/roles/sessions/access_* | 密码/token/Key只存哈希，角色变更及停用可审计 |
| Agent 任务 | Agent tasks.jsonl，16 MiB | 账本 ID、准备、执行、结果/确认；执行中进程恢复标为未知，绝不重放 Docker |
| 最新采集 | PostgreSQL agent_snapshots/containers + 内存 | 恢复旧快照仍离线/过期，等待新数据；采集预算保留 |

目录 0700、文件 0600；中心配置/证书按专用服务用户只读 0640。各数据文件单写者锁，损坏/不完整尾部拒绝启动并保留供修复。禁止删除 WAL 来“解锁”：这样会丢失幂等依据。默认不自动删除业务审计/任务账本；容量预警后应停服备份并评估保留策略，不引入首版无界历史管理。

PostgreSQL 事务提交失败拒绝新执行，未知结果仍锁定；写者连接断开不会静默恢复租约。修复数据库后重启中心，账户会话依赖数据库，存储不可用时不能以文件回退绕过鉴权。

systemd 使用 Restart=on-failure，中心 MemoryMax=256M/CPUQuota=50%，Agent 128M/25%；目录模式 0700，日志写 journald，按宿主 journald 策略轮转，并限制每 30 秒最多 60 条。生产查询不记录 Authorization 或原始 inspect。达到 WAL 配额时不以删除记录恢复服务。

## 备份、升级与回滚

升级前查询 `/api/v1/operations?active=true`，执行中/未知操作必须完成或按现场核实机制关闭。中心用 pg_dump 自洽备份，存储加密密钥/配置/证书独立保护；Agent tasks.jsonl 停服备份。运行中的业务 Docker 容器不随监控升级重启。

安装脚本保留上一份二进制及私有配置为 .previous，用 .next + rename 切换二进制；数据目录不清空。回滚使用 [rollback.sh](deploy/rollback.sh) 的 center/agent 参数，恢复上一份二进制与匹配配置，保留当前持久数据及任务 ID。只回滚兼容当前数据格式的版本；格式损坏需要先修复/恢复一致备份，不强行忽略。

首次版本以 schema_version=1 的基准和本次日志记录格式为准；后续升级需要显式兼容/迁移。现场验收用独立环境验证重启后同键复用；正式既有 RTC 业务未用于升级/回滚实验。

## Docker 与离线包

交付包提供 center/agent 静态二进制、内嵌 React、部署脚本、源代码/锁文件、OpenAPI、模板和 Docker 离线镜像。通过 `docker load -i avops-images-linux-amd64.tar` 加载，不使用强制 pull。

Docker 中心以非 root 运行（镜像默认 UID/GID 65532），配置/私钥必须对所选 UID 只读、数据目录可写。Agent 需本机 Socket、host 网络用于本机探测，以及独立可写任务目录；不要让两个 Agent 共用同一任务账本。完整参数及私有 env 由对应安装方式注入。系统试点采用 systemd 二进制部署，Docker 方式在独立验收环境检查。
