# Probe

`avops-probe:V1.0` 包含原版 SDK、FFmpeg、Node、Chromium 和固定媒体。使用 `deploy/compose.yaml` 部署，将中心导出的 probe-client.env 放到 config/，填写 `.env` 的地址与两类容量后执行 `docker compose up -d --wait`。

容器入口先读取私有接入文件并准备权限，再降到 UID 65532 管理 Native/Web 客户端。首次生成共享身份；保留 host/clients 挂载即可继续使用原身份和 outbox。已有客户端数据却缺少服务器身份时拒绝重新生成。

- `agoranative/`：原版 Go SDK 调用与音视频证据。
- `websdk/`：浏览器运行页、统计、清理与固定素材。
- `probe/`：任务、worker、并发、落盘和补报。
- `internal/server/`：容器初始化、降权、健康检查与独立客户端监督。
- `internal/telemetry`、`internal/routeproof`、`native/`：API/回调记录及路径采集核验。
- `deploy/`、`cmd/probectl/`：开发侧 SDK/媒体/浏览器依赖与镜像构建、Compose 文件；不再提供外部安装升级器。
- `third_party/`：未修改的原版 SDK。

调度属于 center/rtchub，公共协议与落盘工具位于 shared/。运行镜像始终一个，Web 构建依赖镜像只用作构建中间层。
