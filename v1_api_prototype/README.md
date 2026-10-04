# V1.0 API 契约原型

该目录用于评审《音视频（RTC/RTM）运维监控管理系统 PRD V1.0》的无界面交付方式。它不连接真实 RTC/RTM 节点，也不会执行真实 Docker 操作。

2026-10-04：PRD V0.6 已确认 RTC 试点：111.230.192.59 为业务/Agent，111.230.108.76 为中心/UI/Demo；轮询、单 HTTP/HTTPS 地址推送、HTTP Basic Auth、可运行页面与单中心纳入首期。当前 Mock 尚未实现这些新增能力及可靠恢复，[技术设计](../音视频运维监控管理系统_技术设计_v1.0.md) 说明目标行为；本目录仍用于基本接口演示，不能作为正式验收环境。

完整的原型目标、架构、状态模型、客户 UI 接入、演示流程和评审标准见 [统一原型设计文档](../音视频运维监控管理系统_原型设计_v1.0.md)。

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

已确认 RTC 目录模板见 [rtc_pilot.yaml.example](rtc_pilot.yaml.example)。它只定义 5 个候选服务，尚无真实容器节点；实际容器、版本、端口和机器架构需要核对，不能把模板或历史 services.yaml.example 当作真实试点台账。

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
