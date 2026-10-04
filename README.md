# monitor

音视频（RTC/RTM）运维监控项目，当前首期为 RTC：节点基线、Agent 状态采集、Readiness、人工受控重启、HTTP/HTTPS 通知和最小演示页面。

## 项目内容

| 路径 | 内容 |
|---|---|
| [PRD](音视频运维监控管理系统_PRD_v1.0_评审版.md) | 首期范围、健康口径、功能、验收和实施事项 |
| [技术设计](音视频运维监控管理系统_技术设计_v1.0.md) | 架构、协议、鉴权、可靠恢复、通知和选型参考 |
| [原型设计](音视频运维监控管理系统_原型设计_v1.0.md) | 接口及演示目标，区分 Mock 与真实实现 |
| [notification_receiver](notification_receiver/README.md) | 已开发的 HTTPS 通知接收服务，包含认证、可靠落盘、去重与部署脚本 |
| [v1_api_prototype](v1_api_prototype/README.md) | OpenAPI、YAML 示例和本机 Mock |
| [会议改进项](dp/音视频运维监控管理系统_会议改进项.md) | 会议与产品评审的改进追踪 |
| [现场实施记录](dp/RTC试点_现场核对与实施记录_2026-10-04.md) | 已核对、已部署和待实现内容 |
| [APaaS 监控设计梳理](APaaS私有化监控_技术设计梳理_v3.10.0.md) | 交付包的指标采集、存储、Grafana 与复用分析 |
| [future_ui_concept](future_ui_concept/README.md) | 后续 UI 概念稿，不代表首期功能已完成 |

## 当前状态

通知接收服务已开发并完成独立部署验证；正式监控中心、RTC Agent 和可运行 Demo 尚未全部实现或上线。Mock 的固定数据不表示现场健康，文档中的计划与真实验证状态分开记录。

## 本地验证

使用匹配的 Go 1.21+ 工具链：

```bash
cd notification_receiver
go test -race ./...
go vet ./...
```

启动接口 Mock：

```bash
cd v1_api_prototype
go run ./cmd/mockserver
```

Mock 不执行真实 Docker 操作，不作为生产、安全或可靠恢复验收环境。

## 部署材料

证书、私钥、真实运行凭证、事件数据、离线镜像包和原始录音不托管在本仓库，按交付说明单独配置。示例 YAML 不包含真实密钥。APaaS 分析中引用的原交付包也不随仓库发布，保留分析与对应文件路径供核对。
