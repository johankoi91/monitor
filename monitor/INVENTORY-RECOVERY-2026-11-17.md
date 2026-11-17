# 111.230.192.59 容器台账采集恢复

2026-11-17；在 RTC 1.1 发布验收完成后排查并修复。

## 原因与证据

1. Agent 已连到中心，但 Docker 采集持续失败。中心已有容器快照停在 2026-11-09 17:25:18 UTC；Agent 每 15 秒输出 collection incomplete。在线仅代表 WSS 通道连接，不能证明 Docker 采集成功。
2. 主机 Docker 已为 29.9.3，API 最高 1.56、最低 1.40。旧 Agent 在读取与受控重启代码中固定请求 v1.39。现场同一 Unix Socket：v1.39/containers/json 返回 HTTP 400，明确提示 client version 1.39 is too old；v1.40 同一查询返回 HTTP 200 和 24 个原有容器。
3. 资源指标另有独立故障：Agent 配置读取 http://127.0.0.1:18089，但 avops-cadvisor.service 虽 enabled 却 inactive/dead，端口无人监听。可见日志不足以确认是谁停止了服务，不将其直接归因于某次操作。

## 修复

- 新增 Docker /version 协商，优先保留兼容 API 1.39，按服务端最低/最高范围调整；该机选择 1.40。缓存每分钟刷新，读请求遇到版本拒绝时重新协商一次。
- 受控重启使用相同协商结果，保持写操作不自动重试；未触发业务容器重启。
- Agent 采集失败日志补充受控错误原因。
- 更新监控容器 avops-agent 为 avops-agent:docker-api-20261117，保留原环境、机器凭证、挂载、网络、资源限制和重启规则。旧容器为 avops-agent-before-docker-api-20261117，私有回退资料在 /opt/avops-monitor/docker-api-20261117。
- 校验原有 cAdvisor 二进制 SHA-256 c381c2c911bc43d465d1e0eaff60f96d58c031c409bddf06da0316fdde8a9296，启动既有 avops-cadvisor.service，仍只监听回环 18089。

## 验证结果

- 中心发现列表：25 个容器，15 个运行中，25 个均为新鲜数据。多出的 1 个为保留的旧监控 Agent 回退容器。
- 15 个运行中容器的资源状态均 AVAILABLE/OK，CPU、内存等指标恢复；已停止的容器按规则不提供实时资源指标。
- 当前已有台账纳管 11 个容器，其余在发现列表中，按需选取纳管；没有把全部运行容器自动加入基准。
- 除监控 Agent 外，前后容器 ID、启动时间和运行状态全部一致。
- 原台账版本仍为 2112bd3ae34c1556dc409e5ed01f1c38；RTC 1.1 功能探测继续 HEALTHY。

Docker 版本选择、旧版兼容、运行时升级后重新协商、重启写操作不重试，以及 Agent/中心真实协议回归测试通过。部署脚本为 [upgrade-agent-binary.py](deploy/upgrade-agent-binary.py)。本次没有升级/重启 Docker daemon，没有停止或重建业务容器。
