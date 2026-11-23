# AVOPS Monitor V1.0

三个离线镜像，Docker Compose 编排，容器内自动初始化。客户主机只需 Docker 与 Compose v2.18+，不需要 Go、Python、Node 或宿主机初始化工具。

| 目录 | 职责 | 运行镜像 |
| --- | --- | --- |
| `center/` | 页面、API、台账、RTC 调度、PostgreSQL 17.11、通知 | `avops-center:V1.0` |
| `probe/` | Native/Web RTC SDK、浏览器、固定媒体 | `avops-probe:V1.0` |
| `agent/` | 主机发现、采集、日志、受控操作及 cAdvisor | `avops-agent:V1.0` |
| `shared/` | 协议、模型、存储、容器初始化和公共工具 | 只复用源码，不部署 |
| `cmd/avops-release/internal/` | 发布工具内部的三镜像校验、Compose 文件收集和离线组包实现 | 开发侧工具 |

解压后选择角色目录，复制 `.env.example` 为 `.env` 并填写部署参数；加载对应镜像后运行 `docker compose up -d --wait`。中心第一次启动自动生成配置与密码；后续沿用密钥和数据库。Agent/Probe 的接入文件从中心生成目录分发，身份由容器首次启动生成。

```sh
go test ./...
(cd center/frontend && npm ci && npm run build)
```

`centerctl`、`agentctl`、`probectl` 仅供开发侧独立构建各自镜像；根目录 `avops-release` 只做统一组包与校验。第三方 Go SDK 保持原样，位于 `probe/third_party/Agora-Golang-Server-SDK-2.6.6/`。

详见 [部署说明](部署说明.md)、[技术实现](技术实现.md)、[产品 PRD](产品PRD.md)。
