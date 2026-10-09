# V1.0 API 契约原型

该目录提供《音视频（RTC/RTM）运维监控管理系统 PRD V1.0》的目标接口契约及基础 Mock。它不连接真实 RTC/RTM 节点，也不会执行真实 Docker 操作。

PRD V0.8 沿用 2026-10-04 已确认的 RTC 试点：111.230.192.59 为业务/Agent，111.230.108.76 为中心/UI/Demo；轮询、单 HTTPS Webhook 通知、Basic Auth、可运行页面与单中心纳入首期。本次新增 Agent 脱敏启动配置和跨机器可视化筛选生成 YAML 基线。当前 Mock 未实现容器发现/配置详情/基线保存、正式鉴权、推送及可靠恢复；[技术设计](../音视频运维监控管理系统_技术设计_v1.0.md) 说明目标行为，不能以契约文件作为功能已实现证明。

完整的原型目标、架构、状态模型、客户 UI 接入、演示流程和评审标准见 [统一原型设计文档](../音视频运维监控管理系统_原型设计_v1.0.md)。

2026-10-05：新增真实实现见 [monitor](../monitor/README.md)，容器发现/启动配置/可视化基准已在试点部署；本目录 Mock 保持独立，重启模拟不等于真实中心支持操作。

后续 V1.0 实施已补齐 React、真实受控重启/幂等/恢复/审计和中心通知，详见 [完整验收](../dp/RTC试点_V1.0_完整功能验收记录_2026-10-05.md)。本目录 Mock 不同步为生产进程，统一契约按 PRD V0.9 维护。

## 原型组成

```text
v1_api_prototype/
├── openapi.yaml                 客户集成契约
├── services.yaml.example       RTC/RTM 应有节点台账示例
└── cmd/mockserver/main.go       可运行的 Mock API
```

## 启动 Mock API

```bash
cd deploy_console/v1_api_prototype
go run ./cmd/mockserver
```

默认监听：

```text
http://127.0.0.1:18083
```

RTC 目录模板见 [rtc_pilot.yaml.example](rtc_pilot.yaml.example)，只定义 5 个候选服务且无节点；[现场记录](../dp/RTC试点_现场核对与实施记录_2026-10-04.md) 保留实际核对结果。模板不是已纳管台账；正式功能实现后，用户从 Agent 发现清单筛选并归类，中心按既有 YAML 结构生成、生效、持久化基线。

## 新增目标接口（Mock 尚未实现）

| 接口 | 用途 |
|---|---|
| GET /api/v1/agents | 已登记机器供主机下拉筛选，不返回凭证 |
| GET /api/v1/containers | 跨机器容器发现，包含未纳管/停止实例，支持筛选和分页 |
| GET /api/v1/containers/{agent_id}/{container_id} | 脱敏启动配置、时间及完整性标记 |
| GET /api/v1/baseline | 全部已保存成员及版本，含缺失/失联节点 |
| POST /api/v1/baseline/selection | 按版本校验明确新增/移出，生成并立即生效 YAML |
| GET /api/v1/baseline/yaml | 下载生效版本，可用 revision 参数防止版本错配 |

筛选和分页不覆盖基线，新增节点默认禁重启；保存失败保留旧版本。启动配置独立保存且在 Agent 脱敏，不写入可下载 YAML 或健康通知。新接口受统一 Basic Auth 保护，正式实现后才能执行对应验收。

## 状态查询

```bash
curl http://127.0.0.1:18083/api/v1/services/status
curl 'http://127.0.0.1:18083/api/v1/services/status?service_code=rtm-core-forwarder'
```

## 模拟重启

```bash
curl -X POST http://127.0.0.1:18083/api/v1/operations/restart \
  -H 'Content-Type: application/json' \
  -d '{
    "node_id": "rtm-zw-01/forwarder0",
    "operator": "demo",
    "reason": "故障演练",
    "request_key": "restart-demo-001"
  }'
```

Mock Server 返回 `202 Accepted` 和 `operation_id`，约两秒后操作状态由 `RUNNING` 变为 `SUCCESS`。使用返回的 ID 查询结果：

```bash
curl http://127.0.0.1:18083/api/v1/operations/OPERATION_ID
```

缺失节点 `rtm-zw-01/forwarder1` 会被拒绝重启，用于验证服务端保护逻辑。

## 客户 UI 接入

客户系统按 OpenAPI 的 HTTP Basic Auth 接入：Authorization: Basic Base64(接入ID:密钥)。采用本系统自有配置凭证，不使用声网云服务密钥；不建设个人角色或审批。公网管理 API/页面与 Agent 跨机连接使用 HTTPS/WSS。当前 curl 示例没有凭证，只适用于尚未更新的本机 Mock；operator 不等同于已验证个人身份。

正式服务中，`request_key`、`operation_id` 和操作结果/审计必须可靠恢复，确保中心重启不重复执行重启；存储选型待 OPEN-008 冻结。当前 Mock 数据保存在内存，进程重启会丢失，不能验证跨进程幂等和执行中断恢复。

```text
客户监控平台 / 客户系统 / API 工具 -> V1.0 API
```
