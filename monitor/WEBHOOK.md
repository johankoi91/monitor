# 通知 Webhook 接入

按[声网接收 Webhook 官方文档](https://doc.shengwang.cn/doc/rtc/restful/webhook/receive_webhook)实现传输、签名、响应和重试规则。载荷保留本系统的运维状态 JSON，不是声网云 RTC 频道事件 API。

## 发送与接收

只允许 HTTPS，验证证书链及域名，不跟随重定向；IP URL 可配置 TLS ServerName。客户端复用连接，空闲保持 60 秒、每目标空闲连接池 100；接收方建议 keep-alive 超时至少 10 秒、允许至少 100 次请求。

中心对实际发送的原始 JSON 字节生成两个请求头：

| 请求头 | 算法 |
|---|---|
| Agora-Signature | HMAC-SHA1，小写十六进制 |
| Agora-Signature-V2 | HMAC-SHA256，小写十六进制 |

接收端必须先读取原始请求体，再验签，之后才能解析 JSON。不能对解析、缩进或重排后的 JSON 验签。优先 V2；V2 已提供但错误时直接拒绝，不降级 V1。比较签名采用常量时间比较；通知不使用 HTTP Basic Auth。

Python 接收端验签示例（secret 从受保护配置读取）：

```python
import hashlib
import hmac

def verify(raw_body: bytes, headers, secret: str) -> bool:
    v2 = headers.get('Agora-Signature-V2')
    received = v2 if v2 is not None else headers.get('Agora-Signature', '')
    digest = hashlib.sha256 if v2 is not None else hashlib.sha1
    try:
        signature = bytes.fromhex(received)
    except ValueError:
        return False
    expected = hmac.new(secret.encode(), raw_body, digest).digest()
    return hmac.compare_digest(expected, signature)
```

接收端须在 10 秒内返回 HTTP 200 和有效 JSON，例如 `{"code":"OK"}`；202、204、空响应和非 JSON 都失败。本系统将响应体上限设为 4 KiB，接收方应保持确认响应简短。发送器失败最多重试 3 次，共 4 次尝试，等待间隔为 0、1、5 秒；官方规定递增间隔，这些具体数值属于本系统实现。投递不阻塞采集和查询。

## 健康检查、重复与乱序

仅发送节点状态通知，不再生成服务聚合通知。历史聚合消息保留；WEBHOOK_TEST 仍为明确的传输检查，不是服务聚合或节点健康变化。

启用或更换地址、TLS 身份、签名密钥前，发送签名健康检查；不通过则保留原配置。“测试已保存 Webhook”可以单独验证已保存的目标，返回状态码和事件 ID，并持久记录结果。测试类型为 WEBHOOK_TEST、原因 MONITOR_WEBHOOK_TEST，不改变业务健康状态，也不使用官方频道测试的 channelName/uid。

状态通知为 STATUS_CHANGED，包含 event_id、occurred_at、product、cluster、service_code、node_id、前后状态、原因、collected_at 和 stale。重试保持相同 ID 和载荷。接收方按 event_id 去重，乱序时结合事件时间及轮询接口核实最新状态；不能把通知接收成功视作业务恢复。

试点接收端在可靠写盘/fsync 后返回 200 JSON；重复相同载荷仍返回 200，相同 ID 不同载荷返回 409。它的诊断 GET 保留独立 Basic Auth 与来源限制，不能用该 Basic 凭证代替通知 POST 签名。

## 配置与迁移

节点通知的 node_id 使用 `主机IP-容器名`，例如 `111.230.192.59-agora_web_media_edge_1`，接收方以 `cluster + node_id` 定位。台账/重启接口的内部 ID 保持原绑定，不把通知标识直接传入重启 API。旧通知历史与已排队事件不改写，以保持事件 ID、原始消息体与重试一致。

页面配置 HTTPS URL、TLS 域名、启停和签名密钥，支持生成/复制新密钥。将同一密钥配置到接收端后再启用。中心 PostgreSQL AES-GCM 加密存储签名密钥，配置查询不返回密钥；更换目标要求重新填写，旧目标未完成投递停止重试。

旧试点从 Basic 通知迁移时，暂将已有独立接收端 Secret 用作共享签名密钥，移除发送端 ID 和 Authorization。接收端通过 AVOPS_WEBHOOK_SECRET 配置独立密钥，未设置时兼容读取旧 AVOPS_RECEIVER_SECRET。切换须先停止中心发送、升级接收端、再升级中心，保持账号/台账/操作数据。新增部署应将诊断密码与签名密钥分开配置。

Agent 和机器重启仍采用原有 Basic 协议及独立权限限制，账号登录仍采用会话和 RBAC；本次只改变通知鉴权协议。
