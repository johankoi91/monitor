# 工作台部署记录

2026-11-18 12:39（Asia/Shanghai）完成演示环境发布。

- 地址：https://demo.edge.rtcdevelopers.com:19443/
- 源码提交：bf1d93b（展开式导航与工作台实现）。
- 镜像：avops-center:workbench-20261118-123440
- 中心程序 SHA-256：5a358d17c271e3541a1cf775f0e2a85abfca9055d1a308fa530843bf9587f0f7
- 旧镜像：avops-center:rtc11-service-logs-20261118
- 旧容器：avops-center-before-workbench-20261118-123440
- 远端发布目录：/var/tmp/avops-workbench-20261118-123440
- 远端备份：/var/backups/avops-workbench-20261118-123440

## 发布与核对

发布前完成 TypeScript 检查、前端生产构建及 Linux amd64 中心程序构建。由现有升级脚本在原中心镜像上替换中心程序，并继承运行环境、挂载、资源限制、只读根文件系统、日志和重启策略。

发布前保存容器与接口快照、配置目录，并完成 PostgreSQL 自定义格式备份及 pg_restore 清单校验。备份包含私有信息，仅保存在远端受限目录，不写入仓库。

发布后核对：

- 中心 readiness 为 READY。
- 2 / 2 Agent、1 / 1 RTC 探针在线；11 个纳管节点。
- 1 个 RTC 目标、1 条 RTC 任务；基准、目标/任务/保留策略及通知接收配置与发布前一致。
- 容器环境、用户、启动入口、挂载及读写属性、网络、资源限制、日志和重启策略保持一致。Docker 返回的挂载列表顺序不同，按实际源、目的地和权限核对一致。
- 公网 TLS 校验成功；JS /assets/index-DDogp8zW.js 和 CSS /assets/index-EmZKrX3F.css 的 SHA-256 与本地构建一致。
- 公网浏览器可呈现登录页面；认证后的业务流程已在相同静态产物的隔离服务中完成回归。没有使用或修改现有用户浏览器会话。

本次替换监控中心容器，未更新 Agent、RTC 探针或业务容器，未执行人工重启、立即探测或修改业务配置。

## 回退材料

保留旧容器与镜像，可停止当前中心、保留为另名，再将旧容器恢复为 avops-center 并启动，之后复核 readiness 与 Agent / 探针重连。UI 回退沿用当前数据库和持久目录；不要为回退页面而覆盖发布后的运营数据。
