# PostgreSQL 与账号权限实施记录

日期：2026-10-07。中心/数据库为 111.230.108.76，RTC Agent 为 111.230.192.59。根据用户要求，中心从本地 JSON/JSONL 持久化切换到 PostgreSQL，并增加账号注册登录/RBAC。当前部署 PostgreSQL **17.11**、schema_version=1；数据库只监听 **127.0.0.1:5432**。

本文保留初次数据库迁移时的验收证据。后续角色已按用户要求收敛为 Agora 人员 / 客户运维，不再使用下文初始三角色与自定义角色方案；当前权限、旧角色迁移与部署验证见[两类角色调整记录](运维监控_两类角色权限调整_2026-10-07.md)。

## 已完成

- PostgreSQL 事务保存当前/历史台账、节点、操作与阶段审计、Key/范围/申请、通知队列/结果/比较事实、最新脱敏容器快照、账号/角色/会话、系统审计。
- 操作表唯一 request_key、活动节点锁唯一索引；状态和审计原子提交；写者 advisory lock 防同库第二中心。
- 结构化列/索引用于状态、节点、来源及时间筛选，JSONB/GIN 和 extensions 为扩展字段及复杂查询保留接口。
- 注册默认 PENDING，由管理员开通并分配角色。内置管理员/运维/只读，自定义角色可勾选权限；后端 API 检查权限，授予范围/最后管理员/版本校验保护。
- bcrypt 保存密码，随机 8 小时会话只存哈希，退出/停用/授权变更/改密撤销旧会话；Key 仍只存哈希，通知凭证 AES-256-GCM 加密。
- 人工重启仍要角色权限、独立 RESTART Key、范围、中心/Agent 精确白名单及现场状态；账号不会打开核心节点重启开关。
- Agent tasks.jsonl 继续本机执行 WAL，避免网络/数据库不可用导致重复 Docker 写操作。

## 迁移与保留证据

迁移前确认 active operations 为空，停止中心并备份 `/var/lib/avops-monitor`、旧二进制及私有配置到 `/var/backups/avops-center-pg-20261007`（0700）。导入在同一数据库事务中完成，manifest 记录：

| 原文件 | 导入条目 |
|---|---:|
| current-baseline.json | 1 |
| access-keys.jsonl | 8 |
| operations.jsonl | 7 |
| notifications.jsonl | 769 |

正式表验证：**9 个活动节点、2 个操作、7 条阶段审计、4 个 Key**；台账版本保持 `1aeebba24378ee0b24dfe0a3603d544e`。8 个核心 RTC 节点禁重启，演练节点开放，既有通知接收配置及独立认证恢复。源文件不删除，PG 模式不继续写旧文件。

具体 manifest/验证 JSON 位于上述私有备份目录，不包含密码的 verification.json 记录账号登录、10 项管理员权限、3 个角色、台账/Key/操作数量及退出会话撤销。初始化管理员 `admin`；初始密码已通过私有文件交付到本机 `/Users/hanxiaoqing/.cache/avops-rtc-pilot/postgres/account-handoff.txt`（0600），文档不展示密码。

## 验证

独立 `avops_test` PostgreSQL 数据库执行真实集成测试：

| 条件 | 结果 |
|---|---|
| 相同来源重复导入 | 幂等，无重复数据 |
| 迁移后旧 request_key 重放 | 原 operation_id，保留幂等 |
| 旧 Key 认证 | 哈希/启停/范围保留 |
| 通知设置 | 数据库存密文，可正确恢复 |
| 第二中心写者 | advisory lock 拒绝 |
| PENDING 账号登录 | 拒绝 |
| 只读写台账、携带重启 Key 发起重启 | 403 |
| 运维角色但无重启 Key | 403 |
| 运维角色 + 独立 Key 重放已有操作 | 202，原操作，未触发新 Docker 执行 |
| 停用账号、修改密码 | 旧 token 拒绝 |
| 停用已绑定 Key 所属账号 | Key 请求拒绝 |
| 删除最后管理员权限/停用 | 事务拒绝 |
| 内置角色覆写 | 拒绝；自定义角色创建成功 |
| 强制终止数据库写者连接 | 不静默重连继续写，修复/重启后操作锁保留 |
| 兼容文件导出 | 成功，账号仍保存在 PG |

正式 `pgverify` 用私有凭证进行账号登录、角色目录、台账版本/成员、操作、Key、通知状态查询与 logout→401 验证。PostgreSQL 容器和中心按次序重启后再次验证成功，Agent 新快照恢复，8 个 RTC + 演练节点均 HEALTHY。

`pg_dump -Fc` 备份经 pg_restore --list 校验，并实际恢复到独立 `avops_restore_test`，恢复验证为 9 个节点、2 个操作、1 个初始化账号；未覆盖正式数据库。加密密钥独立保护。全量 Go race/vet、React TypeScript/Vite 构建通过。浏览器检查正式账号登录及注册表单；账号权限行为使用真实数据库和 HTTP 集成测试验证。

## 当前边界及维护

仍是单中心，数据库写者租约断联后必须恢复数据库并重启中心，不提供自动主备切换。最新采集虽持久化，中心恢复前标为离线/过期；旧快照不冒充健康。没有增加无限历史指标曲线或事件告警中心。

旧未绑定账号 Key 保留系统凭证语义，用途/节点范围独立；兼容 Basic 初始化管理入口可继续管理，但不能直接重启。新个人 Key 可绑定账号并受其状态/权限约束。角色/会话不替代 Agent 本机控制。

初始备份为 `monitor-after-migration.dump`，旧二进制 `center-before-postgres`、旧数据 `legacy-state` 均在私有备份目录。降级到旧文件模式程序会失去账号/RBAC，不能当作无损恢复；优先恢复兼容 PG 版本、数据及密钥。测试数据库保留供复核，不用于正式服务。

部署采用已拉取的官方 postgres:17 镜像经 DaoCloud 现场镜像源提供，摘要 `sha256:ae69c452f483507a6b99fb654cf93aad7fe156ffd2c56247707eef4e36d3c12b`，实际 17.11。runtime 数据库账号仅有表/序列读写及 schema 使用权限，owner 执行迁移。没有升级/停止 RTC 业务容器或修改日志平台组件。

详见 [PostgreSQL 存储与权限设计](../monitor/POSTGRESQL.md)、[schema](../monitor/internal/postgres/schema.sql)、[真实集成测试](../monitor/internal/postgres/integration_test.go)。
