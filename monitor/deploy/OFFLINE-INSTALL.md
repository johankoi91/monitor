# 运维监控 V1.0.3 离线 Docker 交付

适用于Linux x86_64、Docker25+。Web随中心镜像提供，无需Node/CDN或独立前端容器，API仍为V1.0。包含最新中心/Agent镜像、PG17.11镜像、迁移/配置工具、cAdvisor、通知接收服务与当前源码。证书/私钥、密码、Key和运行数据独立交付。

## 新环境安装

1. 在包根目录执行 `sha256sum -c SHA256SUMS`，加载 `images/avops-postgres-17-linux-amd64.tar`。无需Compose或公网拉取。
2. 执行 `bin/pgprovision -out /私有配置目录`，按实际回环PG地址/端口调整owner.env/database.env。以999:999拥有的新目录持久PG数据，端口只发布127.0.0.1。PG运行参数见source/monitor/deploy/start-postgres.sh，已有数据库不得重新初始化。
3. 在新PG执行生成的init.sql，创建受限runtime和默认读写权限；从私有owner.env加载环境，用owner依次执行 `bin/dbmigrate -compact-operations`、`bin/dbmigrate -remove-key-scopes`，先完成DDL再启动中心。不授予runtime ALTER权限。
4. 准备center.env、database.env、center.yaml、server.pem和server-key.pem，登记实际Agent和证书身份、HTTPS/WSS监听；批准规则默认空。执行 `bash deploy/docker-install.sh center /私有配置目录 /持久中心目录 avops-center`。
5. 每台主机准备agent.env和restart-rules.yaml（默认空数组），配置主动WSS地址、Agent凭证和证书域名。执行 `bash deploy/docker-install.sh agent /私有配置目录 /持久Agent目录 avops-agent`。Agent使用本机Docker Socket和host网络，独立账本目录；不能双实例共用账本。可选配置本机cAdvisor和实际Docker数据盘路径。
6. 初始化账号登录，核对Agent在线，从发现清单选择真实容器保存基准/YAML。新节点默认禁重启。Key签发只填名称，自动绑定当前Agora账号。启动参数点击后WSS现场读取，不存数据库。

安装脚本遇同名容器拒绝覆盖，镜像从包内加载、使用pull=never；Docker限制CPU/内存/PID、只读根文件系统及轮转日志。HTTPS域名/DNS和客户来源限制按现场配置，不跳过证书验证。Socket读取及受控restart权限需在交付环境确认。

## 隔离干净安装验收

Linux Docker主机上，端口25433/29444/28085空闲、已有有效TLS证书时：

```bash
python3 deploy/clean-install-check.py \
  --bundle /绝对路径/monitor-1.0.3-linux-amd64 \
  --work /新的私有测试目录 \
  --cert /证书.pem --key /私钥.pem --tls-name 证书域名
```

脚本创建全新PG目录、凭证、容器和端口，验证owner迁移/受限runtime、HTTPS账号登录、Agent WSS发现、保存台账、实时参数且不落库、Key自动归属、Basic REST演练重启/幂等/审计和中心重启恢复。它仅纳管自己新建的canary，结束移除测试容器，保留私有数据和proof.json。需要root、Python3标准库、Docker及有效证书，不需要Go/Node/Compose或网络下载。

## 备份与升级回退

升级前查询活动/未知操作并完成核实。中心做pg_dump一致备份，密钥/配置/证书独立保护；Agent停服备份账本。保留旧镜像和数据，安装工具不能覆盖旧实例；明确停旧写者后启动新容器，不执行整个栈down或删除volume。

操作schema已精简、Key范围表已移除，旧二进制不能直接回滚到新数据库结构。只有兼容程序可以原库回退；恢复旧DB备份必须核对备份后新增操作和未知执行，不能丢失幂等证据。启动后核对台账/账号/Key/审计/新快照。

不含客户Docker升级包，不自动升级业务Docker。cAdvisor需要Docker25+。RTM、ARM服务端、主备、黑盒与长期压测不属于本次打包验收。
