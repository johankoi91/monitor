# RTC 试点通知接收服务

2026-10-04 已在 `114.132.190.201` 部署并从中心机 `111.230.108.76` 验证。本目录为真实接收服务，不是监控中心或业务 Agent。

## 接口

所有接口同时要求来源 IP 白名单和 HTTP Basic Auth。生产服务只监听 HTTPS `18443`，凭证与中心管理 API 分开。

| 方法 | 路径 | 行为 |
|---|---|---|
| POST | /api/v1/notifications | 校验 RTC STATUS_CHANGED JSON，可靠写入后返回 200 |
| GET | /api/v1/notifications/recent?limit=50 | 最新接收记录，limit 为 1–200 |
| GET | /health/live | 进程响应及记录数量 |
| GET | /health/ready | 存储状态，存储写失败后返回 503 |

载荷与中心 OpenAPI 的 NotificationPayload 对齐：event_id、event_type、occurred_at、product=RTC、cluster、service_code、可选 node_id、previous_status、current_status、reason_code、reason、collected_at（可空）、stale。

- 首次受理：`{"code":"OK","duplicate":false,"persisted":true,"event_id":"..."}`。
- 相同事件重试：仍为 200，duplicate=true，不重复存储。
- 相同 ID 不同载荷：409。
- 缺失/错误认证：401；非批准来源：403。
- JSON 不合法：400；非 JSON：415；请求大于 256 KiB：413。
- 容量到限：507；写入/同步失败：503，不确认成功。

## 持久化与边界

事件写入 events.jsonl 并 fsync 后才确认。启动时重建去重索引，独占文件锁防止第二个写者；损坏或不完整末尾使启动失败，保留原文件供修复。写失败后服务拒绝继续受理，不声称“恰好一次”。

默认数据容量 64 MiB，可通过 AVOPS_RECEIVER_MAX_BYTES 修改。达到容量时停止接受新事件，重复的已落盘事件仍可确认；首版不自动删除、轮转或过期去重记录。修改保留策略时需同时考虑去重期限。

当前载荷限定 RTC。它接收状态事实，不创建告警工单、不操作 RTC 容器、不代表真实业务已经恢复。

## 部署配置

| 配置 | 用途 |
|---|---|
| AVOPS_RECEIVER_ID / AVOPS_RECEIVER_SECRET | 独立 Basic Auth 凭证，必填 |
| AVOPS_RECEIVER_ADDR | 默认 127.0.0.1:18443，实机 0.0.0.0:18443 |
| AVOPS_RECEIVER_ALLOWED_IPS | 实机为 111.230.108.76、127.0.0.1、::1；按连接来源判断，不信任 X-Forwarded-For |
| AVOPS_RECEIVER_DATA_DIR | 实机 /var/lib/avops-notification-receiver |
| AVOPS_RECEIVER_MAX_BYTES | 最大事件文件大小，默认 67108864 |
| AVOPS_RECEIVER_TLS_CERT / AVOPS_RECEIVER_TLS_KEY | PEM 证书与私钥，必填；最低 TLS 1.2 |

systemd 单元：[avops-notification-receiver.service](avops-notification-receiver.service)。安装脚本：[install.sh](install.sh)。以专用 avops-notify 用户运行，Restart=on-failure、MemoryMax=256M、CPUQuota=50%，不替换或重启现有 RTM 业务。

实机路径：

- 程序：/opt/avops-notification-receiver/notification-receiver。
- 配置：/etc/avops-notification-receiver/receiver.env，root 0600。
- 证书：/etc/avops-notification-receiver/server.pem。
- 私钥：/etc/avops-notification-receiver/server-key.pem，root:avops-notify 0640。
- 数据：/var/lib/avops-notification-receiver/events.jsonl，avops-notify 0600。

生成的凭证保存在本机 /Users/hanxiaoqing/.cache/avops-rtc-pilot/receiver/receiver.env；中心机测试凭证在 /root/.config/avops-pilot-notification/receiver-curl.conf。文件权限 0600，不在本文或仓库公布密钥。

## 证书域名与目标地址

提供的证书覆盖 edge.rtcdevelopers.com 及 *.edge.rtcdevelopers.com，不覆盖 IP。当前没有配置本次接收端的 DNS 解析，测试通过 curl --resolve 指定接收 IP，同时保留完整证书校验：

```bash
# 在中心机执行，凭证从受保护的配置文件读取。
curl --noproxy '*' \
  --resolve edge.rtcdevelopers.com:18443:114.132.190.201 \
  --config /root/.config/avops-pilot-notification/receiver-curl.conf \
  https://edge.rtcdevelopers.com:18443/health/ready
```

中心发送器接入时，需采用以下一种方式：使用已解析到接收 IP 的证书域名 URL；或连接 https://114.132.190.201:18443，同时配置 tls_server_name=edge.rtcdevelopers.com。后者必须校验证书链和 ServerName，不能设置 InsecureSkipVerify。当前中心发送器尚未实现该配置，不能把 IP URL 直接拿给通用客户端使用。

接收路径为 /api/v1/notifications。HTTP 目标支持是中心发送器的要求；本接收服务的实机部署使用 HTTPS。

## 开发与验证

使用 Go 1.21+，无外部模块依赖，Linux/Darwin 文件锁实现；运行需要 TLS 和凭证。

```bash
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o notification-receiver .
```

已验证：认证、来源限制与伪造转发头、载荷及大小限制、并发去重、同 ID 冲突、跨重启恢复、容量及写入失败、不完整尾部和第二个写者拒绝。实机由中心发出明确标为连通性测试的事件，首次落盘、再次重复、接收服务重启后仍去重，健康计数保持为 1。没有据此宣称实际 RTC 状态推送已上线。
