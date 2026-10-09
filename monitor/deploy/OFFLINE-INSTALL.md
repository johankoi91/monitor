# RTC 运维监控 V1.0 离线交付

本包面向 Linux x86_64/Kylin V10/Docker 18.09+/systemd 243+。bin 为静态程序，中心已内嵌 React；images 包含 Docker 离线镜像，source 包含源码、锁文件、OpenAPI、模板及完整文档。没有真实凭证、私钥、客户运行数据或原始音视频。

1. 用 SHA256SUMS 核对程序/镜像文件。
2. 按 source/monitor/OPERATIONS.md 准备各机的独立接入凭证、证书、Agent 登记和来源限制；实际配置独立交付，不复制占位值。
3. 先按 source/monitor/POSTGRESQL.md 生成私有材料、加载PG17.11镜像、初始化受限runtime账号与schema；已有环境先备份再迁移。随后按systemd方式安装，把bin/center或bin/agent与对应私有配置/service单元放入材料目录。中心还须database.env和center-postgres.conf，再运行install-center.sh或install-agent.sh。
4. 可选 `docker load -i images/avops-images-linux-amd64.tar`。中心镜像为非 root，按所选 UID 设置配置/数据权限；Agent 挂载本机 Socket、独立任务目录和批准规则，采用 host 网络。不要将试点的空/禁止白名单改为全开放。
5. 先按 source/monitor/POSTGRESQL.md 生成PG私有材料，加载images/avops-postgres-17-linux-amd64.tar，启动PG17.11并迁移数据库；数据库不可用时正式中心不能回退文件鉴权。页面使用已开通账号/密码，采用会话和RBAC；机器/Agent接口仍按用途使用Basic，查询异常业务节点返回HTTP200，通过body判断。

升级前由Agora查询 /api/v1/operations?active=true 并完成/核实所有占用节点操作。中心使用pg_dump一致备份并独立保存加密密钥/配置/证书；Agent停服一致备份执行WAL。只能回滚兼容当前PG数据与角色权限的程序，不能回退旧文件鉴权绕过RBAC，禁止删任务日志解锁。

详细说明：[运行维护](source/monitor/OPERATIONS.md)、[功能说明](source/monitor/README.md)、[PRD](source/音视频运维监控管理系统_PRD_v1.0_评审版.md)、[真实验收记录](source/dp/RTC试点_V1.0_完整功能验收记录_2026-10-05.md)。真实 RTC 核心节点的重启批准不由本包自动授予。
# PostgreSQL 依赖更新（2026-10-07）

2026-10-08重新打包的1.0.1包含当前中心/Agent、React、账号/RBAC、PG17.11离线镜像与工具、cAdvisor v0.60.6二进制、Webhook接收服务/页面及本地查看代理。镜像标签avops-center:1.0.1和avops-agent:1.0.1；API仍属于V1.0/v1。Docker升级包未附带，使用cAdvisor需现场Docker25+，不要自动升级客户业务Docker。

当前中心正式存储必须 PostgreSQL；历史离线包不包含本次账号/RBAC 功能，不能作为当前发布版本。新包须带 postgres 17 镜像、dbmigrate/pgprovision 工具和 schema，私有凭证另行生成。按 [POSTGRESQL](../POSTGRESQL.md) 先初始化 DB/受限 runtime，再迁移或建立 schema，最后启动中心；加密密钥独立备份。
