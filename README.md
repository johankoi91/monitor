# monitor

音视频运维监控项目，当前首期为RTC。Agent主动采集Docker状态、启动参数与cAdvisor资源，中心提供React页面、可视化台账、Readiness、受控重启及HTTPS Webhook通知，使用PostgreSQL持久化。

## 项目内容

| 路径 | 内容 |
|---|---|
| [PRD](1.0/音视频运维监控管理系统_PRD_v1.0_评审版.md) | 首期范围、健康口径与验收 |
| [技术设计](1.0/音视频运维监控管理系统_技术设计_v1.0.md) | 架构、协议与可靠恢复 |
| [原型设计](1.0/音视频运维监控管理系统_原型设计_v1.0.html) | 页面原型及截图，实际行为以当前代码为准 |
| [notification_receiver](notification_receiver/README.md) | 已开发的 HTTPS 通知接收服务，包含认证、可靠落盘、去重与部署脚本 |
| [monitor](monitor/README.md) | 已部署的真实 Agent/中心、启动配置采集、容器筛选及 YAML 基准页面 |
| [v1_api_prototype](v1_api_prototype/README.md) | OpenAPI、YAML 示例和本机 Mock |
| [本次改动汇总](monitor/RELEASE-NOTES-1.0.4.md) | 账号、Token、通知、分组、资源趋势及参数缓存 |
| [部署记录](monitor/OPTIMIZATIONS-2026-11-02.md) | 现场验证、版本及回退信息 |
| [离线交付](releases/README.md) | 全服务镜像包、校验和安装验收 |
| [后续规划](1.1/) | V1.1 RTC/RTM规划，不算当前已实现功能 |

## 当前状态

当前交付版本V1.0.4，API版本v1。管理者/使用者权限沿用原设计，同账号仅保留一处登录；REST重启使用有到期时间的Bearer Token，仍须通过现场白名单和状态校验。状态按分组→主机→容器展示，资源趋势保留7天；启动参数主动上报，最后有效记录加密保存。通知按接收地址和事件类型独立订阅。RTM、黑盒、主备及长期规模压测后置，隔离演练不代表客户真实业务验收。

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

全服务离线包在releases目录通过Git LFS托管，克隆后执行 `git lfs pull`。证书、私钥、真实凭证、客户运行数据和原始录音不入库、不进离线包。Web嵌入中心镜像，无运行时Node或CDN依赖；完整安装步骤见 [离线安装](monitor/deploy/OFFLINE-INSTALL.md)。
