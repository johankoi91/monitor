# 运维监控 V1.0.4 离线 Docker 交付

适用于Linux x86_64、Docker25+。Web随中心镜像提供，无需Node/CDN或独立前端容器，API仍为V1.0。包含中心/Web、Agent、通知接收端、cAdvisor 0.60.6 四个应用镜像及PostgreSQL 17.11镜像，附迁移/配置工具、源码和SHA256校验。证书/私钥、密码、Token和运行数据独立交付。镜像标签及ID见images/image-manifest.json；PG沿用包内固定镜像，不从公网更新17标签。

## 新环境安装

1. 在包根目录执行 `sha256sum -c SHA256SUMS`，加载 `images/avops-postgres-17-linux-amd64.tar`。无需Compose或公网拉取。
2. 执行 `bin/pgprovision -out /私有配置目录`，按实际回环PG地址/端口调整owner.env/database.env。以999:999拥有的新目录持久PG数据，端口只发布127.0.0.1。PG运行参数见source/monitor/deploy/start-postgres.sh，已有数据库不得重新初始化。
3. 在新PG执行生成的init.sql，创建受限runtime和默认读写权限；从私有owner.env加载环境，用owner依次执行 `bin/dbmigrate -compact-operations`、`bin/dbmigrate -remove-key-scopes`，先完成DDL再启动中心。不授予runtime ALTER权限。
4. 准备center.env、database.env、center.yaml、server.pem和server-key.pem，登记实际Agent和证书身份、HTTPS/WSS监听；批准规则默认空。执行 `bash deploy/docker-install.sh center /私有配置目录 /持久中心目录 avops-center`。
5. 每台主机准备agent.env和restart-rules.yaml（默认空数组），配置主动WSS地址、Agent凭证和证书域名。执行 `bash deploy/docker-install.sh agent /私有配置目录 /持久Agent目录 avops-agent`。Agent使用本机Docker Socket和host网络，独立账本目录；不能双实例共用账本。可选配置本机cAdvisor和实际Docker数据盘路径。
6. 在Agent主机先加载应用镜像，执行 `bash deploy/start-cadvisor.sh`，仅发布127.0.0.1:18089；agent.env配置 `AVOPS_CADVISOR_URL=http://127.0.0.1:18089`。挂载路径应对应实际Docker数据目录，已有cAdvisor监听该端口时不得重复启动。
7. 如需配套接收端，准备独立receiver.env、server.pem、server-key.pem，执行 `bash deploy/docker-install.sh receiver /私有接收配置目录 /持久接收目录 avops-notification-receiver`。receiver.env设置独立诊断账号、签名密钥、监听地址及来源白名单，变量详见source/notification_receiver/README.md。中心按接收地址配置事件订阅和签名密钥；不将客户平台的接收地址强制指向演示接收端。
8. 初始化账号登录，角色为管理者/使用者；同账号仅保留一处登录。从未纳管发现清单选择容器，设置分组名并保存基准/YAML。新节点默认禁重启。重启Token自动绑定当前账号，设置有效期后签发；REST使用Authorization: Bearer。启动参数随Agent主动上报，加密保存最后有效记录，离线/停止/删除仍可查看历史；资源趋势记录自安装起积累，保留7天。

安装脚本遇同名容器拒绝覆盖，镜像从包内加载、使用pull=never；Docker限制CPU/内存/PID、只读根文件系统及轮转日志。HTTPS域名/DNS和客户来源限制按现场配置，不跳过证书验证。Socket读取及受控restart权限需在交付环境确认。

## 隔离干净安装验收

Linux Docker主机上，端口25433/29444/28085空闲、已有有效TLS证书时：

```bash
python3 deploy/clean-install-check.py \
  --bundle /绝对路径/monitor-1.0.4-linux-amd64 \
  --work /新的私有测试目录 \
  --cert /证书.pem --key /私钥.pem --tls-name 证书域名
```

脚本创建全新PG目录、凭证、容器和端口，验证owner迁移/受限runtime、HTTPS登录、Agent WSS发现、台账、主动参数加密存储/离线保留、真实cAdvisor资源历史、Token自动归属、Bearer REST演练重启/幂等/审计、接收端HTTPS/HMAC/去重及中心重启恢复。额外使用28089和28443端口。它仅纳管自己新建的canary，结束移除测试容器，保留私有数据和proof.json。需要root、Python3标准库、Docker及有效证书，不需要Go/Node/Compose或网络下载。

## 备份与升级回退

升级前查询活动/未知操作并完成核实。中心做pg_dump一致备份，密钥/配置/证书独立保护；Agent停服备份账本。保留旧镜像和数据，安装工具不能覆盖旧实例；明确停旧写者后启动新容器，不执行整个栈down或删除volume。

操作schema已精简、Key范围表已移除，旧二进制不能直接回滚到新数据库结构。只有兼容程序可以原库回退；恢复旧DB备份必须核对备份后新增操作和未知执行，不能丢失幂等证据。启动后核对台账/账号/Key/审计/新快照。

本版增加资源历史表，已有环境必须由owner运行dbmigrate后再启动新中心。旧密钥对不能用于REST重启，请重新签发Token；不要重建账号或清空台账。历史参数仅展示，重启继续核验现场状态与当前容器ID。旧版本包含启动参数清理逻辑，不能作为本版参数缓存的兼容回退程序。

GitHub中的大体积离线包使用Git LFS：克隆后执行 `git lfs pull`；完整包本身不依赖Git LFS运行。普通Git只保存LFS指针，不能把指针文件当成安装包。

不含客户Docker升级包，不自动升级业务Docker。cAdvisor需要Docker25+。RTM、ARM服务端、主备、黑盒与长期压测不属于本次打包验收。
