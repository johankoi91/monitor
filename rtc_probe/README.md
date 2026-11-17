# RTC Probe

通过原样的 `Agora-Golang-Server-SDK-2.6.6` 探测整体 RTC 服务。探针主动以 WSS 连接中心；每个用例启动独立 Linux 工作进程，创建一个 AgoraService 和发送/接收两个 RtcConnection。

## 配置与判定

在中心“RTC 业务探测”页面先创建目标服务，填写服务名称、AppID、鉴权、AP 客户端入口及 Native 覆盖分组（或既有手动 UDP 端口范围）（内部目标 ID 自动生成）；任务只关联目标、选择一个探针并配置调度、超时和失败/恢复阈值；新任务默认停用，启停只在操作栏控制。默认私有化参数：

- IPList / DomainList：`111.230.192.59`
- VerifyDomainName：`ap.1145326.agora.local`
- Mode：ConnectivityFirst；DisableAut：true
- 发送/接收 UID：10001 / 10002

频道固定由系统按执行 ID 生成，发送/接收 UID 固定为 10001 / 10002，不接受外部配置。多个任务共享目标配置。凭证在中心加密保存，查询只返回“已配置”；Certificate 不下发探针，中心签发本轮短期 Token。仅支持未开启鉴权或 App Certificate，变更 AppID 或鉴权方式时须提交新凭证。目标修改只取消关联任务的旧轮次，历史参数快照保留。旧任务接入配置启动时自动迁移到目标，原双端相同探针的绑定自动迁移为单个 probe_id。配置接口拒绝旧双端字段、UID、频道与启停字段；启停仅使用任务专用接口。

固定使用官方 demo 的 `send_audio_16k_1ch.pcm` 和 `send_video_cif_352x288.yuv`，每轮发送约 5 秒。两端成功加入、发布/送帧正常，接收端同时收到至少 1 秒有效音频及 5 帧有效视频才通过。加入拒绝、发布/送帧失败、超时缺媒体判为业务失败；探针离线、资源不足或进程异常属于监控执行错误。默认连续 3 次业务失败确认异常，2 次成功恢复，监控执行错误转为未知。

启用 UDP edge 范围后，一轮按升序逐个探测完整范围，例如 4001–4008。每个端口使用独立 SDK 进程，在创建连接前调用服务级 `GetAgoraParameter().SetParameters`，设置单元素 `rtc.vos_list`。失败后继续后续端口，全部通过才整轮通过；同一任务未完成时不重入，超时按单端口计算。

## Native UDP / AUT 完整覆盖

目标启用“Native UDP / AUT 自动覆盖”后，关联发现清单中的 AP 分组，并填写 AP 的 `ip_for_client`。Edge 连接地址从容器 `--ip-for-client` 与 `--user-port` 获取；`--ap` 只作分组键，机器 IP 不作连接地址。清单含所有已发现容器，Web 保留发现但不生成探测用例。

每个 AP 客户端入口分别检查 UDP 和 AUT；再遍历所有 Native Edge 直连，并按有向环验证同类及混合模式音视频互通。跨 Edge 用例数按实际 Native 实例数 N 生成，每个实例每轮发送、接收各一次，下一轮反向，后续轮次打乱同类型节点顺序；这不表示所有配对均已验证。3 个 UDP、3 个 AUT 时共 AP 2、直连 6、跨 Edge 6 项。期望实例消失或离线仍保留待验证，确认下线后可在目标中移出覆盖；新容器或配置变更使旧覆盖版本失效。

每个用例一个独立 worker、一个 Service、两个 Connection。公共 `SetLocalAccessPoint` 设置一次；两端分别使用 `conn.GetAgoraParameter().SetParameters`：UDP 设置 `rtc.enable_nasa2=false`，AUT 设置 `true`；直连再设置单元素 `rtc.vos_list`。允许 SDK 辅助访问目标 AP；不要求屏蔽 AP。此版本连续创建多个 Service 返回同一原生句柄，不能用它隔离公共参数。

集成工程的 `native/route_capture.c` 只在 SDK 子进程预加载，记录 IP/端口、流量方向、字节数和时刻；限制前端端点并允许配置 AP、目标 AP IP 上的 SDK 事件上报端口 4301 和 DNS，不采集媒体载荷。事件上报流量不计入媒体路径或 AP 交换证据；已发现的媒体端点仍按媒体角色校验。默认库 `/opt/rtc-probe/libroute_capture.so`，可通过 `RTC_PROBE_ROUTE_CAPTURE` 指定（参见客户端参数）。`routeproof` 将有效媒体窗口的网络元数据与解码回调关联；媒体通过且实际选路 VERIFIED / Guarded 才算成功。AUT 的实际 socket 流量可能也是 UDP，不以 TCP/UDP socket 类型代替 Edge 类型。

三类完成后分别起算下一轮间隔，默认 AP 180 秒、直连 900 秒、跨 Edge 1800 秒；页面分别设置。同一任务可并行派发不同用例，并发上限取期望 Native 实例数、探针资源容量和中心总容量的最小值。按需启动进程，不预建空闲 SDK。AP 优先领取空闲槽位；每个完成的工作槽位独立冷却 5 秒；启用和动态发现均检查 worker 占用预算。三类分别累计失败/恢复阈值并聚合整体状态，已确认且新鲜的业务失败优先于其他未知；过期结果不维持健康结论。详情分别查看覆盖、媒体计数、实际路径及子用例调用/日志。

## 目录

| 目录 | 用途 |
|---|---|
| `cmd/rtc-probe` | 探针进程、原生 worker 和本地自检入口 |
| `agoranative` | 真实 SDK 调用、音视频收发、回调及原生验收测试 |
| `probe` | WSS 调度、进程隔离、结果补报、日志上传与本地保留 |
| `hub` | 中心集成的目标/任务管理、健康判定、执行历史 |
| `protocol` | 控制消息、目标接入参数、证据及保留策略 |
| `native` / `routeproof` | SDK 子进程端点约束、网络元数据与路径证据判定 |
| `telemetry` | API/回调明细、脱敏和实时汇总 |
| `durable` | 有界读取、原子文件保存 |
| `testkit` | 自动化测试使用的模拟 worker，不进入生产依赖 |
| `deploy` | 依赖下载、原生打包、升级及离线导出 |
| `build/demo-media` | 构建所需的固定 PCM/YUV 素材，可重新下载 |

## 构建

需要原始 Go SDK 目录位于同级 `../Agora-Golang-Server-SDK-2.6.6`。集成工程不修改 SDK 源码。Linux amd64 原生构建需要 Go 1.24、CGO/C 编译器、pkg-config，以及 FFmpeg 的 libavformat/libavcodec/libavutil/libswresample 开发库；已有构建环境使用 FFmpeg 6.1.3。

在仓库根目录执行，下载脚本校验原生 SDK 和两份固定素材的 SHA-256：

```sh
python3 rtc_probe/deploy/fetch_dependencies.py
docker build -f rtc_probe/Dockerfile -t rtc-probe:sdk-local .
docker run --rm --network none --read-only --tmpfs /tmp:size=32m,mode=1777 --memory 512m --pids-limit 256 rtc-probe:sdk-local self-check
```

也可在配齐依赖的 Linux 构建机直接编译，再使用 `deploy/package_native.py` 收集实际动态库并生成独立运行镜像上下文：

```sh
cd rtc_probe
CGO_ENABLED=1 go build -tags agora -trimpath -o /tmp/rtc-probe ./cmd/rtc-probe
python3 deploy/package_native.py --binary /tmp/rtc-probe --sdk ../Agora-Golang-Server-SDK-2.6.6/agora_sdk --media build/demo-media --output /tmp/rtc-probe-image
docker build -t rtc-probe:sdk-local /tmp/rtc-probe-image
```

`package_native.py` 适用于现有构建机的 `/etc/pki/tls/certs/ca-bundle.crt` 布局。`self-check` 验证素材并创建真实 SDK 对象，不加入频道；完整验收需从中心发起真实音视频任务。未使用 `-tags agora` 的客户端拒绝运行。

## 运行

在中心登记探针，将一次性返回的凭证保存到权限 0600 的 `/etc/rtc-probe.env`：

```text
RTC_PROBE_ID=中心登记的探针标识
RTC_PROBE_SECRET=中心返回的机器凭证
RTC_PROBE_CENTER_URL=wss://中心域名:19443/api/v1/probe/connect
RTC_PROBE_DATA_DIR=/data
RTC_PROBE_CAPACITY=auto
```

中心域名须可解析、TLS 证书须可信；私有 CA 按现场部署方案安装。`auto` 根据 CPU/cgroup 配额及内存额度计算资源容量；内存预算保留父进程 256 MiB，并按每个 worker 256 MiB 计算，上限 64。中心再根据实际 Native 清单决定本任务并发数，实例数未写死。缺失实例继续保留为待验证，确认移出覆盖后才缩减期望清单。也可填写整数作为人工资源上限。以下资源示例可容纳当前 8 个 Native 实例；资源上限不足时排队，不跳过覆盖项。运行真实音视频探针：

```sh
install -d -m 0700 -o 65532 -g 65532 /var/lib/rtc-probe/native
docker run -d --name rtc-probe --pull never --network host --restart unless-stopped --read-only --tmpfs /tmp:size=32m,mode=1777 --cap-drop ALL --security-opt no-new-privileges --memory 4g --cpus 8 --pids-limit 2048 --env-file /etc/rtc-probe.env -v /var/lib/rtc-probe/native:/data rtc-probe:sdk-local
```

## 日志与保留

本地按角色记录 `sdk-api.jsonl`，保存方法入参、返回值、回调参数、关联调用及耗时；高频明细留本地，WSS 上报首帧、错误和秒级汇总。原始 PCM/YUV 不落盘，Token 等秘密脱敏。SDK 二进制日志按原文件边界保存；进程退出码、信号、超时和清理结果纳入诊断。

同一用例的收发连接共享一个 SDK Service，中心只回捞一个 Service 诊断包。包内 `sdk-api.jsonl` / `events.jsonl` 合并收发记录并标明 `endpoint_role`，`result.json` 包含两端结果；共享的 `sdk/agora*.log`、进程输出和路径元数据各保存一份，二进制 SDK 文件保持原文件边界。日志请求 `scope=service`、`endpoint_role=pair`，manifest 同时校验收发 UID 与频道。新任务按探针 `shared_service_logs_v1` 能力生成该格式，旧格式诊断仍可读取。ZIP 上限 8 MiB，按 256 KiB 分块校验、断点续传。默认中心执行明细 7 天，历史/诊断各 2048 MiB；探针成功日志 60 分钟、失败 1440 分钟、预算 1024 MiB。策略可在中心页面配置；预算优先回收已确认日志，未确认结果/事件受保护，空间不足时暂停新任务。

## 升级与离线包

保留 `deploy/fetch_dependencies.py`、`package_native.py`、`upgrade.py`、`export_offline.py` 四个脚本。升级器面向现有 Docker 中心/探针，沿用原环境、网络、数据卷及资源限制；原容器更名保留。

导出本机已有镜像；发布标签须与 `avops-center:<标签>` 或 `rtc-probe:sdk-<标签>` 一致：

```sh
python3 rtc_probe/deploy/export_offline.py probe /path/to/probe-offline --release your-release
python3 rtc_probe/deploy/export_offline.py center /path/to/center-offline --release your-release
```

包内含唯一 `README.md`、升级器、镜像、manifest 和 SHA256SUMS；中心包另含 `rtc_history.sql`。包中不包含部署配置、凭证或运行数据。在包目录校验、加载并升级：

```sh
sha256sum -c SHA256SUMS
docker load -i image.tar.gz
# root 执行；标签取 manifest.json。中心模式改为 center 及对应目录。
python3 upgrade.py probe /opt/rtc-probe/your-release --release your-release --prebuilt
```

中心新增 RTC 表由数据库 owner 执行 `rtc_history.sql`，运行账号只需既有 DML 授权。迁移到目标配置前，先停用 RTC 任务、确认没有执行中轮次并备份加密控制文档。现有 PostgreSQL 容器布局的备份示例：

```sh
umask 077
docker exec avops-postgres psql -X -At -v ON_ERROR_STOP=1 -U avops_owner -d avops_monitor -c "SELECT format('UPDATE avops.documents SET payload=%L::jsonb, encrypted_payload=%L::bytea, revision=%L, updated_at=%L::timestamptz WHERE name=%L;', payload::text, encrypted_payload::text, revision, updated_at::text, name) FROM avops.documents WHERE name='rtc-probe/state-v1'" > rtc-state-restore.sql
test -s rtc-state-restore.sql
```

回退时停止新容器，恢复所需的 RTC 控制文档，再将 `<容器名>-before-<标签>` 更名回原容器并启动。降级到旧任务配置版本必须恢复升级前的控制文档，不能只换回容器；升级器自动回退旧容器后若 API 无法就绪，同样需要先停中心再恢复该文档。上述备份用 `docker exec -i avops-postgres psql -X -v ON_ERROR_STOP=1 -U avops_owner -d avops_monitor --single-transaction < rtc-state-restore.sql` 恢复，只覆盖 RTC 控制状态，保留执行历史和 V1 台账。确认就绪后恢复原任务启用状态。私有备份放包外。

## 测试

```sh
cd rtc_probe
go test -race ./...
cd ../monitor
go test ./internal/center ./internal/postgres ./internal/notify
```

真实 Linux SDK 测试使用 `-tags agora`。`RTC_NATIVE_LIVE_QA=1` 与 `RTC_NATIVE_QA_APP_ID` 显式启用真实频道验收；常规自动化使用 `testkit` 验证调度、证据、去重、取消和日志，不产生真实 RTC 成功结论。

完整设计及验收记录见 `../1.1/RTC_Native_UDP_AUT_完整探测设计.md`。真实全计划验收另由 `RTC_NATIVE_COVERAGE_QA=1` 显式启用，测试 `TestNativeLiveFullCoverage`；不能用模拟 worker 的结果宣称真实服务正常。


并发版本的控制消息优先于遥测，SDK 事件突发合并进行持久化提交；只有提交成功后才发布就绪/结果，逐条 API / 回调及事件文件仍完整保存。中心需为并发数据提供足够 CPU；现场已调整为 2 vCPU / 512 MiB。开始指令过迟、剩余时间不足以发送固定素材时归为控制执行错误。升级器可选 `--probe-capacity auto --probe-cpus 8 --probe-memory-mib 4096 --probe-pids-limit 2048`；这些是资源额度，非固定 Edge 数。


2026-11-18 当前生产验收：18 项真实音视频通过，按 Service 回捞 18 个合并日志包；逐包验证包含收发 API/回调、两端结果及一份共享 SDK 日志，每包仅一次 Initialize。探针版本 0.7.0。详细历史与验收记录见上述设计文档。
