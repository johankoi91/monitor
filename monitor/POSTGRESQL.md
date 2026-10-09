# PostgreSQL 存储与账号权限

2026-10-07，中心正式存储使用 PostgreSQL 17.11；单中心数据库为 `avops_monitor`，schema 为 `avops`。中心内存用于实时判断和执行协调，持久事实、身份和查询使用数据库。Agent 执行 WAL 继续保存在本机，中心/数据库不可达时不能靠远端记录重试 Docker 操作。

## 数据落点

| 表 | 内容与一致性 |
|---|---|
| documents、baseline_versions、nodes | 当前基准、历史版本、YAML、变更和结构化节点；保存同一事务提交 |
| operations、operation_audit | 当前操作、唯一 request_key、节点锁、执行阶段/证据；状态与审计原子提交 |
| access_keys、key_requests、key_node_scopes、access_audit | Key 哈希、申请、用途、账号归属、授权节点、签发/停用记录 |
| notification_events、notification_facts、notification_state、notification_audit | 状态比较事实、可靠待发/重试、投递结果 |
| agent_snapshots、containers | 最新完整脱敏快照、启动配置、指标与生命周期；重启恢复后先标为离线/过期 |
| users、roles、permissions、user_roles、role_permissions | 注册状态、密码哈希、角色及功能权限 |
| sessions、system_audit | 仅存 token 哈希、过期/停用，账号/角色/登录和配置操作审计 |
| schema_migrations、import_batches | 数据库版本、旧数据原子迁移摘要与计数 |

常用筛选使用结构化字段及索引；definition/payload/extensions 使用 JSONB，关键负载具备 GIN 索引，为后续扩展字段和查询提供基础。当前按一个系统一个数据库隔离，不声明中心主备或多租户已实现。

数据库唯一约束保证 request_key 唯一、同节点最多一个 node_locked 操作；中心还保留既有幂等/并发/冷却检查。写者使用数据库会话 advisory lock，另一中心无法成为同一数据库的写者。连接断开即停用该写者，不静默重新获取连接继续执行；修复数据库后重启中心并按账本恢复。查询数据库失败返回 503，可靠写入失败拒绝新重启，最新健康缓存不被伪造为成功。

## 账号与权限

### 通用凭证模型

机器凭证按 `kind` 和作用域隔离：`AGENT` 和 `RESTART` 使用 Basic Auth；`NOTIFICATION_RECEIVER` 仅用于中心出站 Webhook 原始请求体 HMAC 签名。通知 Secret 使用独立存储密钥 AES-256-GCM 加密；入站重启 Key 只保存哈希。凭证不跨用途复用，生命周期规则一致，传输协议不强制统一。通知规则见 [WEBHOOK.md](WEBHOOK.md)。

注册 → PENDING → Agora 人员核实开通并分配角色 → 账号/密码登录。注册默认客户运维角色但待开通，不能登录或获得 API 访问；注册不填写申请原因。密码用 bcrypt（6–72 字节），不存明文；会话为加密随机 token，数据库只存 SHA-256 哈希，有效期 8 小时。

| 内置角色 | 权限 |
|---|---|
| Agora 人员（agora） | 全部模块；容器发现/启动配置、台账/导出/历史、操作与审计、Key 直接签发/管理、账号开通/角色分配仅此角色可用 |
| 客户运维（customer_ops） | 服务状态与资源详情、Agent状态、通知配置与结果、持本人独立Key受控重启及本人结果、自己的密码与会话 |

角色固定为上述两类，每个账号只能选择一个角色，不提供自定义角色或权限编辑。账号开通/角色分配由 Agora 人员操作，保护最后一个可用 Agora 账号。旧 admin 迁移为 agora，其余旧角色默认迁移为 customer_ops；旧角色和分配快照记入账号安全记录，迁移撤销旧会话且仅执行一次。

每个 API 请求在后端校验权限，不能靠隐藏按钮作为控制。停用账号、改变账号角色、修改角色权限及改密使旧会话失效；密码修改需要原密码，退出登录撤销当前会话。登录失败/注册按源 IP 限流；来源网络与跨站写入控制继续生效。会话只保存在当前页面内存，不写 URL、cookie 或浏览器存储，刷新须重新登录。

重启/未知结果核实同时要求账号有权限、独立 RESTART Key 覆盖节点、中心/Agent 精确白名单和现场状态/身份/互斥/冷却检查。账号会话调用使用 Bearer token，加 `X-AVOPS-Restart-Key-ID`、`X-AVOPS-Restart-Key-Secret`。绑定个人账号的 Key 只能由该账号在页面使用，所属账号停用/权限不足时机器调用也拒绝。

Agent 和受控机器接口继续使用独立 Basic Auth；旧 Key 未自动伪造账号归属，保留原用途和节点范围；未绑定账号的 Key 仅可读取状态/资源和配置通知，不能绕过 Agora 专属模块。机器调用台账/全局操作/审计须绑定有权限Agora账号；客户的重启Key可调用批准节点的重启及该Key发起的结果查询。账号会话配独立重启 Key 仍可使用原 Key。页面不提供系统 Key 登录或部署初始化管理凭证入口。Agent/通知认证独立于个人会话。

## 接口

| 方法 / 路径 | 用途 |
|---|---|
| POST /api/v1/auth/register | 注册申请，未登录，返回 PENDING |
| POST /api/v1/auth/login | 登录，返回 token、账号权限及 expires_at |
| GET /api/v1/auth/me | 当前账号与权限 |
| POST /api/v1/auth/logout | 撤销当前会话 |
| POST /api/v1/auth/password | 原密码 + 新密码，撤销所有旧会话 |
| GET /api/v1/users | status/search/limit/offset 分页查询 |
| POST /api/v1/users/{user_id} | 状态/角色、expected_version、reason |
| GET /api/v1/roles | 查询两类固定角色，仅 Agora 可访问 |
| POST /api/v1/roles、/api/v1/roles/{role_id} | 兼容入口固定拒绝，409 FIXED_ROLES |
| GET /api/v1/permissions | 权限目录 |
| GET /api/v1/baseline/history | 历史定义/变更分页，未提供自动回滚 |
| GET /api/v1/operations | status/node_id/source/limit/offset 查询；active=true 为全部锁定操作 |
| GET /api/v1/access-keys/owners | Key 管理端选择已开通账号 |

## 部署与密钥

中心机 PostgreSQL 独立容器 `avops-postgres`，仅发布 **127.0.0.1:5432**，宿主数据目录 `/var/lib/avops-postgres`。内存 512 MiB、CPU 0.75、shared_buffers 64 MiB、max_connections 40；镜像通过现场已使用的 DaoCloud 镜像源拉取官方 postgres:17，实测 17.11。没有修改原日志平台的 ES/Kafka 等组件。

部署生成器 `cmd/pgprovision -out /私有目录` 生成 PostgreSQL owner、受限 runtime 用户、主存储加密密钥及迁移材料；账号首次密码仅用于初始化账号交付，拒绝覆盖，全部 0600。不打印凭证，不提交 Git。数据库 runtime 只获 schema 使用及表/序列读写，DDL/导入由 owner 执行。

中心额外 `/etc/avops-monitor/database.env`（root 0600）含：

```ini
AVOPS_DATABASE_URL=postgres://avops_runtime:PRIVATE_PASSWORD@127.0.0.1:5432/avops_monitor?sslmode=disable
AVOPS_STORAGE_KEY=PRIVATE_64_HEX_CHAR_KEY
AVOPS_BOOTSTRAP_USERNAME=admin
AVOPS_BOOTSTRAP_PASSWORD=PRIVATE_INITIAL_PASSWORD
```

系统初始化账号只在 users 为空时创建，后续启动不重置密码。公网访问仍 HTTPS；远端 PostgreSQL URL 强制 sslmode=verify-full，本机回环允许关闭数据库 TLS。通知鉴权配置使用独立存储密钥 AES-256-GCM 加密后保存，普通配置响应不返回 Secret；数据库备份必须另行安全备份该密钥。

正式中心缺少 AVOPS_DATABASE_URL 时拒绝启动。`AVOPS_STORAGE_MODE=file-dev` 仅供隔离开发/旧测试，不应作为正式部署的数据库失效回退。

## 容量与会话维护

GET /api/v1/storage/status 返回数据库总占用、各表含索引占用、所在数据盘容量/可用空间及最近会话清理结果。页面“通知投递”中展示容量卡片。AVOPS_DATABASE_WARNING_BYTES 默认 1 GiB，为提示阈值而非硬配额；数据盘可用不足 2 GiB 或10%提示，AVOPS_STORAGE_DISK_PATH 默认 /var/lib，须配置为实际 PostgreSQL 数据所在文件系统上、服务用户可读取的路径。远端数据库/独立挂载盘不能把中心本地目录的空间当作数据库空间，应单独配置并核对。

中心启动后及每小时最多清理1000条已过期超过24小时的会话，保留仍有效和近期过期/撤销会话；登录、账号变更、Key变更和访问拒绝记录仅作为内部安全记录保存，不提供独立查询页面。写者租约失效时停止写入。该机制不是完整归档策略：通知、业务审计、台账历史和操作幂等/未知锁仍保留，不自动按天裁剪。清理与容量读取失败明确返回状态/原因，不声称已执行硬配额或自动通知容量告警。

## 迁移、备份与回退（操作）

迁移前确认无 active 操作，停止中心后备份旧程序、私有配置和整个旧数据目录。`cmd/dbmigrate -import-dir /一致备份目录` 在一个事务中导入台账、操作/审计、Key 和通知，记录来源摘要；相同来源重复导入幂等，非空不同目标拒绝覆盖。任一记录损坏/尾部不完整则回滚，不清空旧文件。

首次部署流程见 `deploy/migrate-center-postgres.sh`。数据目录原文件留作备份，PG 模式不继续更新它们。当前初始账号私有交付文件位于本机 `/Users/hanxiaoqing/.cache/avops-rtc-pilot/postgres/account-handoff.txt`，首次登录后修改密码。

`deploy/backup-postgres.sh /新私有目录/monitor.dump` 用 pg_dump -Fc 创建一致备份，并用 pg_restore --list 检查；文件 0600，原加密密钥独立保护。本次已恢复到单独的 avops_restore_test 数据库验证，不向正式数据库覆盖。

数据库重启/故障恢复：停止中心 → 恢复 PostgreSQL → 启动中心 → 核对台账版本/Key/操作/审计/会话及 Agent 新快照。原容器不会因中心/数据库重启被重复操作。

回退优先恢复兼容的 PostgreSQL 版本与数据。`cmd/dbmigrate -export-dir /新私有目录` 可导出最新兼容文件格式，必须在中心停止后执行；角色账号不能在旧文件模式程序中表达，降级会失去 RBAC，不能把旧二进制和迁移前过期文件当作无损回退。PostgreSQL 数据/备份必须保留。

验收见 [迁移记录](../dp/运维监控_PostgreSQL与账号权限实施记录_2026-10-07.md)。
