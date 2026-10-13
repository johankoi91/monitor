import Form from "antd/es/form";
import { useEffect, useState, type FormEvent } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Input from "antd/es/input";
import Space from "antd/es/space";
import Typography from "antd/es/typography";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import Popconfirm from "antd/es/popconfirm";
import type { API } from "./api";
const text = (e: unknown) => (e instanceof Error ? e.message : "申请未完成");
type Key = {
  owner_id?: string;
  key_id: string;
  name: string;
  purpose: string;
  enabled: boolean;
  created_at: string;
};
export default function AccessKeys({
  api,
  canManage,
}: {
  api: API;
  canManage: boolean;
}) {
  const [data, setData] = useState<{ keys: Key[] }>({
      keys: [],
    }),
    [name, setName] = useState(""),
    [issued, setIssued] = useState<{ key: Key; secret: string } | null>(null),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [currentAccount, setCurrentAccount] = useState("");
  async function load() {
    if (!canManage) return;
    try {
      setData(await api.get("/api/v1/access-keys"));
      if (api.accountSession) {
        const me = await api.get<{ user: { username: string } }>(
          "/api/v1/auth/me",
        );
        setCurrentAccount(me.user.username);
      }
    } catch (e) {
      setNotice(text(e));
    }
  }
  useEffect(() => {
    void load();
  }, [api, canManage]);
  async function issue(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    try {
      const result = await api.post<{ key: Key; secret: string }>(
        "/api/v1/access-keys",
        {
          name,
          purpose: "RESTART",
        },
      );
      setIssued(result);
      setNotice("密钥已签发，密钥值仅本次显示。");
      await load();
    } catch (e) {
      setNotice(text(e));
    } finally {
      setBusy(false);
    }
  }
  async function revoke(key: Key) {
    try {
      await api.post(`/api/v1/access-keys/${key.key_id}/revoke`, {});
      setNotice("密钥已停用");
      await load();
    } catch (e) {
      setNotice(text(e));
    }
  }
  async function copySecret() {
    if (!issued) return;
    try {
      await navigator.clipboard.writeText(issued.secret);
      setNotice("密钥已复制到剪贴板，请妥善保存。");
    } catch {
      setNotice("浏览器未允许自动复制，请手动复制下方密钥。");
    }
  }
  async function rotate(key: Key) {
    if (busy) return;
    setBusy(true);
    try {
      const result = await api.post<{ key: Key; secret: string }>(
        "/api/v1/access-keys",
        {
          name: key.name,
          purpose: "RESTART",
        },
      );
      setIssued(result);
      try {
        await navigator.clipboard.writeText(result.secret);
        setNotice("新密钥已签发并复制到剪贴板，正在停用旧 Key。");
      } catch {
        setNotice("新密钥已签发，浏览器未允许自动复制，请手动复制。");
      }
      await api.post(`/api/v1/access-keys/${key.key_id}/revoke`, {});
      setNotice("新 Key 已签发，旧 Key 已停用。请保存本次显示的密钥。");
      await load();
    } catch (e) {
      setNotice(text(e));
    } finally {
      setBusy(false);
    }
  }
  if (!canManage)
    return (
      <section>
        <Typography.Title level={3}>接入密钥</Typography.Title>
        <p>
          这里只直接签发和管理重启安全 Key。仅 Agora
          人员可操作，页面账号密码不通过 Key 登录。
        </p>
      </section>
    );
  return (
    <section>
      <Typography.Title level={2}>重启安全 Key 签发与管理</Typography.Title>
      <p>重启安全 Key 仍须同时满足中心/Agent 的节点白名单和现场状态规则。</p>
      {notice && <Alert message={notice} type="info" showIcon />}
      <Card className="page-card">
        <Form layout="vertical" onSubmitCapture={issue}>
          <Form.Item label="所属账号">
            <Input value={currentAccount || "当前登录账号"} readOnly />
          </Form.Item>
          <Form.Item label="密钥名称" htmlFor="accesskeys-field-2">
            <Input
              id="accesskeys-field-2"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" disabled={busy}>
            签发密钥
          </Button>
        </Form>
      </Card>
      {issued && (
        <Card title="本次签发结果" className="page-card">
          <Typography.Paragraph>
            密钥值仅显示一次，请妥善保存并独立交付给接入方。
          </Typography.Paragraph>
          <pre>
            {JSON.stringify(
              {
                key_id: issued.key.key_id,
                secret: issued.secret,
                purpose: issued.key.purpose,
              },
              null,
              2,
            )}
          </pre>
          <Space>
            <Button onClick={() => void copySecret()}>复制密钥</Button>
            <Button onClick={() => setIssued(null)}>已保存，隐藏密钥</Button>
          </Space>
        </Card>
      )}
      <Card title="已签发密钥" className="page-card">
        <Table<Key>
          rowKey="key_id"
          dataSource={data.keys}
          scroll={{ x: 850 }}
          pagination={{ pageSize: 10, showSizeChanger: false }}
          columns={[
            {
              title: "签发时间",
              dataIndex: "created_at",
              width: 180,
              render: (value: string) =>
                new Date(value).toLocaleString("zh-CN", { hour12: false }),
              sorter: (a, b) =>
                Date.parse(a.created_at) - Date.parse(b.created_at),
              defaultSortOrder: "descend",
            },
            {
              title: "密钥",
              key: "identity",
              render: (_, k) => (
                <>
                  <Typography.Text strong>{k.name}</Typography.Text>
                  <div>
                    <Typography.Text type="secondary">
                      {k.key_id}
                    </Typography.Text>
                  </div>
                </>
              ),
            },
            {
              title: "状态",
              key: "status",
              width: 100,
              render: (_, k) => (
                <Tag color={k.enabled ? "success" : "default"}>
                  {k.enabled ? "有效" : "已停用"}
                </Tag>
              ),
            },
            {
              title: "操作",
              key: "actions",
              width: 270,
              render: (_, k) => (
                <Space>
                  <Popconfirm
                    title="轮换此 Key？"
                    description="生成新 Key 并停用旧 Key，原接入方需更新凭证。"
                    onConfirm={() => rotate(k)}
                  >
                    <Button type="link" disabled={!k.enabled || busy}>
                      轮换并复制新密钥
                    </Button>
                  </Popconfirm>
                  <Popconfirm
                    title="确认停用？"
                    description="此 Key 的后续请求将被拒绝。"
                    onConfirm={() => revoke(k)}
                  >
                    <Button type="link" danger disabled={!k.enabled || busy}>
                      停用
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </section>
  );
}
