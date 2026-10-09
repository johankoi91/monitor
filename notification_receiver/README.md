# RTC 试点通知接收服务

2026-10-04 已在 `114.132.190.201` 部署并从中心机 `111.230.108.76` 验证。本目录为真实接收服务，不是监控中心或业务 Agent。

## 接口

所有接口要求来源 IP 白名单；通知 POST 校验原始请求体 HMAC 签名，诊断 GET 单独使用 HTTP Basic Auth。生产服务只监听 HTTPS `18443`，凭证与中心管理 API 分开。

| 方法 | 路径 | 行为 |
|---|---|---|
| GET | / | 接收记录页面，分页、自动刷新、完整 JSON/原始文本、复制 |
| POST | /api/v1/notifications | 先验签，再校验 STATUS_CHANGED / WEBHOOK_TEST JSON，可靠写入后返回 200 JSON |
| GET | /api/v1/notifications/recent?limit=50&offset=0 | 倒序分页，limit 为 1–200，offset 非负；返回总数、完整事件及可用的 raw_body |
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

2026-10-08 新增接收记录页面。新受理事件保留原始请求体字符串 raw_body（含空白），历史事件不伪造原始文本，页面明确显示“历史记录重建”。记录仍按 event_id 去重，同一事件的重试不会重复生成页面条目；非法签名/载荷不进入已接收列表。

页面默认 20 条，每页可选 20/50/100 条，分页可查看全部已保存事件。首页每 5 秒自动刷新，翻页时暂停自动刷新，展开项保持；展开可切换格式化 JSON 和原始文本，复制复制原始消息体（旧事件复制重建 JSON）。页面只读，不显示签名密钥，不依赖 CDN。

事件写入 events.jsonl 并 fsync 后才确认。启动时重建去重索引，独占文件锁防止第二个写者；损坏或不完整末尾使启动失败，保留原文件供修复。写失败后服务拒绝继续受理，不声称“恰好一次”。

默认数据容量 64 MiB，可通过 AVOPS_RECEIVER_MAX_BYTES 修改。达到容量时停止接受新事件，重复的已落盘事件仍可确认；首版不自动删除、轮转或过期去重记录。修改保留策略时需同时考虑去重期限。

当前载荷限定 RTC。它接收状态事实，不创建告警工单、不操作 RTC 容器、不代表真实业务已经恢复。

## 部署配置

| 配置 | 用途 |
|---|---|
| AVOPS_RECEIVER_ID / AVOPS_RECEIVER_SECRET | 诊断 GET 专用 Basic Auth 凭证，必填 |
| AVOPS_WEBHOOK_SECRET | 通知 POST 独立签名密钥；旧环境迁移暂用 AVOPS_RECEIVER_SECRET，新增环境应单独配置 |
| AVOPS_RECEIVER_ADDR | 默认 127.0.0.1:18443，实机 0.0.0.0:18443 |
| AVOPS_RECEIVER_ALLOWED_IPS | 实机为 111.230.108.76、127.0.0.1、::1；按连接来源判断，不信任 X-Forwarded-For |
| AVOPS_RECEIVER_VIEW_ALLOWED_IPS | 可选独立页面/记录 GET 来源列表；未配置时沿用原来源规则，不能用此名单调用通知 POST 或诊断健康接口 |
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

中心发送器采用 HTTPS IP 地址 + `tls_server_name=edge.rtcdevelopers.com`：连接 `https://114.132.190.201:18443`，TLS 握手按 `edge.rtcdevelopers.com` 校验证书链和 ServerName，不能设置 InsecureSkipVerify。也可以使用解析到该 IP 的证书域名 URL。

接收路径为 /api/v1/notifications。发送器及接收端均只支持 HTTPS Webhook，不能以 Basic Auth 替代通知签名。

## 开发与验证

本机只读查看代理（验证远端 HTTPS，诊断凭证只由代理从受保护文件读取，不发送到浏览器）：

```bash
go run ./cmd/viewer -credentials /私有目录/receiver.env
# 浏览器打开 http://127.0.0.1:18087/
```

代理仅监听 127.0.0.1:18087，只允许读取页面和记录，检查 Host、Origin 和跨站来源。201 页面仍受 Basic Auth 和来源列表保护；证书域名没有解析到接收机时，可经此代理查看，不能关闭服务器证书校验。

使用 Go 1.21+，无外部模块依赖，Linux/Darwin 文件锁实现；运行需要 TLS 和凭证。

```bash
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o notification-receiver .
```

已验证：认证、来源限制与伪造转发头、载荷及大小限制、并发去重、同 ID 冲突、跨重启恢复、容量及写入失败、不完整尾部和第二个写者拒绝。实机由中心发出明确标为连通性测试的事件，首次落盘、再次重复、接收服务重启后仍去重，健康计数保持为 1。没有据此宣称实际 RTC 状态推送已上线。
