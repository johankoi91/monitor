# RTC Probe

通过原样的 `Agora-Golang-Server-SDK-2.6.6` 探测整体 RTC 服务。探针主动以 WSS 连接中心；每轮启动独立 Linux 工作进程，创建一个 AgoraService 和发送/接收两个 RtcConnection。

## 配置与判定

在中心“RTC 业务探测”页面先创建目标服务，填写 AppID、鉴权、频道/UID、私有化入口和可选 UDP edge 端口范围；任务只关联目标、选择探针并配置调度、超时和失败/恢复阈值。默认私有化参数：

- IPList / DomainList：`111.230.192.59`
- VerifyDomainName：`ap.1145326.agora.local`
- Mode：ConnectivityFirst；DisableAut：true
- 发送/接收 UID：10001 / 10002

多个任务共享目标配置。凭证在中心加密保存，查询只返回“已配置”；Certificate 不下发探针，中心签发本轮短期 Token。变更 AppID、鉴权、固定频道或预签发 Token 的 UID 时须提交新凭证。目标修改只取消关联任务的旧轮次，历史参数快照保留。旧任务接入配置启动时自动迁移到目标。

固定使用官方 demo 的 `send_audio_16k_1ch.pcm` 和 `send_video_cif_352x288.yuv`，每轮发送约 5 秒。两端成功加入、发布/送帧正常，接收端同时收到至少 1 秒有效音频及 5 帧有效视频才通过。加入拒绝、发布/送帧失败、超时缺媒体判为业务失败；探针离线、资源不足或进程异常属于监控执行错误。默认连续 3 次业务失败确认异常，2 次成功恢复，监控执行错误转为未知。

启用 UDP edge 范围后，一轮按升序逐个探测完整范围，例如 4001–4008。每个端口使用独立 SDK 进程，在创建连接前调用服务级 `GetAgoraParameter().SetParameters`，设置单元素 `rtc.vos_list`。失败后继续后续端口，全部通过才整轮通过；同一任务未完成时不重入，超时按单端口计算。

## 目录

| 目录 | 用途 |
|---|---|
| `cmd/rtc-probe` | 探针进程、原生 worker 和本地自检入口 |
| `agoranative` | 真实 SDK 调用、音视频收发、回调及原生验收测试 |
| `probe` | WSS 调度、进程隔离、结果补报、日志上传与本地保留 |
| `hub` | 中心集成的目标/任务管理、健康判定、历史与审计 |
| `protocol` | 控制消息、目标接入参数、证据及保留策略 |
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
RTC_PROBE_CAPACITY=1
```

中心域名须可解析、TLS 证书须可信；私有 CA 按现场部署方案安装。运行真实音视频探针：

```sh
install -d -m 0700 -o 65532 -g 65532 /var/lib/rtc-probe/native
docker run -d --name rtc-probe --pull never --network host --restart unless-stopped --read-only --tmpfs /tmp:size=32m,mode=1777 --cap-drop ALL --security-opt no-new-privileges --memory 512m --cpus 1 --pids-limit 256 --env-file /etc/rtc-probe.env -v /var/lib/rtc-probe/native:/data rtc-probe:sdk-local
```

## 日志与保留

每个角色的 `sdk-api.jsonl` 保存方法入参、返回值、回调参数、关联调用及耗时；高频明细留本地，WSS 上报首帧、错误和秒级汇总。原始 PCM/YUV 不落盘，Token 等秘密脱敏。SDK 二进制日志按原文件边界保存；进程退出码、信号、超时和清理结果纳入诊断。

中心自动回捞两端日志，ZIP 上限 8 MiB，按 256 KiB 分块校验、断点续传。默认中心执行明细 7 天、审计 30 天，历史/诊断各 2048 MiB；探针成功日志 60 分钟、失败 1440 分钟、预算 1024 MiB。策略可在中心页面配置；预算优先回收已确认日志，未确认结果/事件受保护，空间不足时暂停新任务。

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

回退时停止新容器，恢复所需的 RTC 控制文档，再将 `<容器名>-before-<标签>` 更名回原容器并启动。降级到旧任务配置版本必须恢复升级前的控制文档，不能只换回容器；升级器自动回退旧容器后若 API 无法就绪，同样需要先停中心再恢复该文档。上述备份用 `docker exec -i avops-postgres psql -X -v ON_ERROR_STOP=1 -U avops_owner -d avops_monitor --single-transaction < rtc-state-restore.sql` 恢复，只覆盖 RTC 控制状态，保留执行历史、审计和 V1 台账。确认就绪后恢复原任务启用状态。私有备份放包外。

## 测试

```sh
cd rtc_probe
go test -race ./...
cd ../monitor
go test ./internal/center ./internal/postgres ./internal/notify
```

真实 Linux SDK 测试使用 `-tags agora`。`RTC_NATIVE_LIVE_QA=1` 与 `RTC_NATIVE_QA_APP_ID` 显式启用真实频道验收；常规自动化使用 `testkit` 验证调度、证据、去重、取消和日志，不产生真实 RTC 成功结论。
