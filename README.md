# monitor

音视频（RTC/RTM）运维监控项目，当前首期为 RTC：可视化选择生成 YAML 节点基线、Agent 状态与脱敏启动配置采集、Readiness、人工受控重启、HTTPS Webhook 通知和可运行页面。

## 项目内容

| 路径 | 内容 |
|---|---|
| [PRD](音视频运维监控管理系统_PRD_v1.0_评审版.md) | 首期范围、健康口径、功能、验收和实施事项 |
| [技术设计](音视频运维监控管理系统_技术设计_v1.0.md) | 架构、协议、鉴权、可靠恢复、通知和选型参考 |
| [原型设计](音视频运维监控管理系统_原型设计_v1.0.md) | 接口及演示目标，区分 Mock 与真实实现 |
| [notification_receiver](notification_receiver/README.md) | 已开发的 HTTPS 通知接收服务，包含认证、可靠落盘、去重与部署脚本 |
| [monitor](monitor/README.md) | 已部署的真实 Agent/中心、启动配置采集、容器筛选及 YAML 基准页面 |
| [v1_api_prototype](v1_api_prototype/README.md) | OpenAPI、YAML 示例和本机 Mock |
| [会议改进项](dp/音视频运维监控管理系统_会议改进项.md) | 会议与产品评审的改进追踪 |
| [现场实施记录](dp/RTC试点_现场核对与实施记录_2026-10-04.md) | 已核对、已部署和待实现内容 |
| [APaaS 监控设计梳理](APaaS私有化监控_技术设计梳理_v3.10.0.md) | 交付包的指标采集、存储、Grafana 与复用分析 |
| [future_ui_concept](future_ui_concept/README.md) | 后续 UI 概念稿，不代表首期功能已完成 |

## 当前状态

RTC V1.0 已实现并部署：真实采集、React/YAML 台账、状态、受控原容器重启、持久幂等/恢复/审计和 HTTPS Webhook 通知。用户既有 8 个台账节点保留，业务核心重启权限默认关闭。完整链路用独立容器验收，见 [V1.0 验收记录](dp/RTC试点_V1.0_完整功能验收记录_2026-10-05.md)。[打开页面](http://127.0.0.1:18086)，登录及维护见 monitor 说明。RTM/黑盒/主备仍后置，旧 Mock 固定数据不代表现场健康。

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
