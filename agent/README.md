# Agent

`avops-agent:V1.0` 同时运行 Agent 和 cAdvisor，使用 `deploy/compose.yaml` 部署。将中心导出的 `agent.env` 放到 config/，填写 `.env` 的本机公网 IP 和实际 Docker 数据根，加载镜像后执行 `docker compose up -d --wait`。

容器内读取私有接入文件、创建数据目录并管理采集器和 Agent；没有外部 Go 安装器。可选 restart-rules.yaml 缺省为空规则。身份和任务账本保存在数据目录，重建容器时继续挂载；不要跨主机复制身份。

`cmd/agent/` 处理 WSS、采集和任务；`internal/agent/` 负责身份、容器初始化、执行账本及日志；`internal/docker/` 读取 Docker 信息和受控操作。`deploy/` 与 `cmd/agentctl/` 仅负责本模块的开发侧构建、cAdvisor 校验下载及 Compose 文件。
