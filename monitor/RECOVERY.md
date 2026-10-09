# 单中心故障处理与恢复

## 判断故障层级

先查看中心 /health/live、/health/ready、/api/v1/agents 和存储容量页面；GET 使用账号会话或受保护管理curl配置，不能把管理密码写命令历史。READY 不表示RTC业务可用；节点 UNKNOWN 可能是Agent离线或采集过期。读取节点 reason_code/check_level/collected_at/stale，再检查对应主机。

## Agent 离线或采集持续失败

在目标主机检查 `systemctl status avops-agent`、`journalctl -u avops-agent --since '10 minutes ago'`、Docker服务/Socket及到中心WSS的连通、证书域名、时钟。Agent连接失败按1–30秒退避重连，异常退出由systemd拉起；宿主机故障不能靠Agent自恢复解决。不要先重启业务容器。

进程卡死或连续采集失败不一定导致退出。修复依赖后，确认中心没有执行中或未知操作，再评估重启Agent；如果有未知执行，先核对Agent本机任务账本和Docker实际启动时间，交由Agora现场核实，不重新发送Docker restart。禁止删除tasks.jsonl解除锁。当前没有自动卡死watchdog，人工判断过程应记录故障证据。

## 中心或PostgreSQL故障

查看 `systemctl status avops-center`、PG容器健康/磁盘和受保护日志。数据库写者连接断开后中心不静默获得新执行权；按停止中心→修复或恢复PG→启动中心→等待Agent新快照的顺序处理。恢复旧快照不能当作新健康证据。

核对基线版本和应有节点数、账号/角色/Key、操作/审计、未知锁及通知配置；客户端受理结果不确定时只重试原request_key，不生成新键。角色或Key无法替代双端批准规则，缺失/过期/身份变化节点仍禁止重启。

## 容量预警与备份

数据库容量提示和数据盘余量告警出现时，先用pg_dump做一致备份并独立备份加密密钥、部署配置和证书；确认dump可列出且能在隔离数据库恢复，再评估扩容或受控归档。每小时会话清理只清理过期超过24小时的token，不解决业务历史无限增长。

操作幂等记录、未知锁、待发通知和审计不能随意删除。磁盘已满或可靠写入失败时不允许继续新重启；不要清空数据库/WAL“解锁”。日志和接收端事件文件容量与PG数据盘不是同一预算。

## 可安全执行的恢复验收

在隔离环境或批准的canary验证：断开Agent→UNKNOWN→恢复新快照；中心重启→台账/锁/原request_key保留；DB写者断联→可靠操作拒绝→按顺序恢复；接收端暂不可用→有限重试/失败→轮询仍可读；TCP失败3次/成功2次；消息重复去重。资源测试应包含Agent+cAdvisor及中心+PG的总占用。

不能用本机TCP证明UDP或真实媒体链路；没有研发明确的探测方法和重启批准时，保留CONTAINER层级、关闭核心重启。主备、ARM、RTM与黑盒SDK需要独立后续验证，不把本手册当成它们已实现。
