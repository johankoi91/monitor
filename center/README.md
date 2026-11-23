# Center

`avops-center:V1.0` 包含中心、前端、PostgreSQL 17.11 和通知接收器。`deploy/compose.yaml` 编排单个容器；填写 `.env` 的中心 URL 和证书路径后，首次启动自动生成私有配置、数据库账号及客户端接入文件。新站点默认页面账号为 `admin`，初始密码为 `1111`；已有账号密码不重置。

- `cmd/container/`：容器入口、健康检查、数据库/应用进程监督。
- `internal/bootstrap/`：首次初始化、完整性与证书检查、重复启动保留秘密、运行文件权限。
- `service/`：HTTP/WSS、发现、台账、状态、资源、操作和日志。
- `rtchub/`：RTC 配置、调度、覆盖、证据和历史。
- `frontend/`：React 源码、static 构建产物和 Go embed。
- `internal/`：账号、权限、数据库、通知及资源历史。
- `deploy/`、`cmd/centerctl/`：本模块镜像的开发侧构建工具及 Compose 模板，不再提供宿主机安装/初始化器。

完整性文件存在时沿用原配置和存储密钥；半初始化或已有数据缺少匹配配置时拒绝重建密码。中心使用受限数据库账号，owner 仅在初始化时使用。
