import Tag from "antd/es/tag";
import Table from "antd/es/table";
import Pagination from "antd/es/pagination";
import Form from "antd/es/form";
import Checkbox from "antd/es/checkbox";
import Radio from "antd/es/radio";
import Select from "antd/es/select";
import Modal from "antd/es/modal";
import List from "antd/es/list";
import { useEffect, useState, type FormEvent } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Input from "antd/es/input";
import Space from "antd/es/space";
import Typography from "antd/es/typography";
import type { API } from "./api";
export type AccountUser = {
  user_id: string;
  username: string;
  display_name: string;
  status: string;
  version: number;
  created_at: string;
  role_ids: string[];
  permissions: string[];
};
type Role = {
  role_id: string;
  name: string;
  description: string;
  builtin: boolean;
  version: number;
  permissions: string[];
};
const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "请求未完成";
export function Register({ onClose }: { onClose: () => void }) {
  const [username, setUsername] = useState(""),
    [password, setPassword] = useState(""),
    [confirm, setConfirm] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [done, setDone] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (password !== confirm) {
      setNotice("两次密码不一致");
      return;
    }
    setBusy(true);
    try {
      const response = await fetch("/api/v1/auth/register", {
        method: "POST",
        credentials: "omit",
        headers: { "Content-Type": "application/json", "X-AVOPS-Request": "1" },
        body: JSON.stringify({
          username,
          password,
        }),
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.message || "注册失败");
      setPassword("");
      setConfirm("");
      setDone(true);
      setNotice("注册申请已提交，待 Agora 人员开通并分配角色后即可登录。");
    } catch (e) {
      setNotice(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card title="注册账号" className="page-card login-panel">
      <Typography.Paragraph>
        注册不会自动获得权限，默认申请客户运维，由 Agora 人员核实开通。
      </Typography.Paragraph>
      {!done && (
        <Form layout="vertical" onSubmitCapture={submit}>
          <Form.Item label="账号" htmlFor="accounts-field-1">
            <Input
              id="accounts-field-1"
              required
              minLength={3}
              maxLength={64}
              pattern="[a-zA-Z0-9][a-zA-Z0-9_.-]{2,63}"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
            />
          </Form.Item>
          <Form.Item label="密码" htmlFor="accounts-field-2">
            <Input.Password
              id="accounts-field-2"
              required
              minLength={6}
              maxLength={72}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
            />
          </Form.Item>
          <Form.Item label="确认密码" htmlFor="accounts-field-3">
            <Input.Password
              id="accounts-field-3"
              required
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" disabled={busy}>
            提交注册申请
          </Button>
        </Form>
      )}
      {notice && (
        <Alert showIcon type="info" message={notice} className="page-alert" />
      )}
      <Button type="link" onClick={onClose}>
        返回登录
      </Button>
    </Card>
  );
}
export function PasswordChange({
  api,
  onClose,
  onChanged,
}: {
  api: API;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [old, setOld] = useState(""),
    [password, setPassword] = useState(""),
    [confirm, setConfirm] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError("两次密码不一致");
      return;
    }
    setBusy(true);
    try {
      await api.post("/api/v1/auth/password", {
        old_password: old,
        new_password: password,
      });
      onChanged();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      title="修改密码"
      footer={null}
      onCancel={() => !busy && onClose()}
      destroyOnHidden
    >
      <Typography.Paragraph>
        修改后所有旧会话失效，需要重新登录。
      </Typography.Paragraph>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="当前密码" htmlFor="accounts-field-4">
          <Input.Password
            id="accounts-field-4"
            required
            value={old}
            onChange={(e) => setOld(e.target.value)}
            autoComplete="current-password"
          />
        </Form.Item>
        <Form.Item label="新密码" htmlFor="accounts-field-5">
          <Input.Password
            id="accounts-field-5"
            required
            minLength={6}
            maxLength={72}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />
        </Form.Item>
        <Form.Item label="确认新密码" htmlFor="accounts-field-6">
          <Input.Password
            id="accounts-field-6"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
        </Form.Item>
        {error && <Alert message={error} type="error" showIcon />}
        <Space className="actions">
          <Button htmlType="button" disabled={busy} onClick={onClose}>
            取消
          </Button>
          <Button type="primary" htmlType="submit" disabled={busy}>
            保存密码
          </Button>
        </Space>
      </Form>
    </Modal>
  );
}
export default function Accounts({
  api,
  canUsers,
  canRoles,
}: {
  api: API;
  canUsers: boolean;
  canRoles: boolean;
}) {
  const [users, setUsers] = useState<AccountUser[]>([]),
    [roles, setRoles] = useState<Role[]>([]),
    [notice, setNotice] = useState(""),
    [filter, setFilter] = useState(""),
    [offset, setOffset] = useState(0),
    [total, setTotal] = useState(0),
    [selected, setSelected] = useState<AccountUser | null>(null),
    [status, setStatus] = useState("ACTIVE"),
    [userRoles, setUserRoles] = useState<string[]>([]),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false);
  async function load() {
    try {
      if (canUsers) {
        const result = await api.get<{ users: AccountUser[]; total: number }>(
          `/api/v1/users?limit=30&offset=${offset}&status=${filter}`,
        );
        setUsers(result.users);
        setTotal(result.total);
      }
      if (canRoles || canUsers) {
        const r = await api.get<{ roles: Role[] }>("/api/v1/roles");
        setRoles(r.roles);
      }
    } catch (e) {
      setNotice(errorText(e));
    }
  }
  useEffect(() => {
    void load();
  }, [api, filter, offset]);
  async function saveUser(e: FormEvent) {
    e.preventDefault();
    if (!selected) return;
    setBusy(true);
    try {
      await api.post(`/api/v1/users/${selected.user_id}`, {
        status,
        role_ids: userRoles,
        expected_version: selected.version,
        reason,
      });
      setSelected(null);
      setReason("");
      setNotice("账号状态与角色已保存，旧会话失效。");
      await load();
    } catch (e) {
      setNotice(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const names = Object.fromEntries(roles.map((r) => [r.role_id, r.name]));
  return (
    <section>
      <Typography.Title level={2}>账号与角色权限</Typography.Title>
      <p>
        固定两类角色：Agora
        人员管理台账、操作、审计、密钥和账号授权；客户运维查看服务状态与资源并配置通知。重启仍须独立
        Key 和节点白名单。
      </p>
      {notice && (
        <Alert showIcon type="info" message={notice} className="page-alert" />
      )}
      {canUsers && (
        <>
          <Form layout="vertical" component="div" className="filters">
            <Form.Item label="账号状态" htmlFor="accounts-field-7">
              <Select
                id="accounts-field-7"
                style={{ minWidth: 180, width: "100%" }}
                value={filter}
                onChange={(value) => {
                  setFilter(value);
                  setOffset(0);
                }}
              >
                <Select.Option value="">全部</Select.Option>
                <Select.Option value="PENDING">待开通</Select.Option>
                <Select.Option value="ACTIVE">已开通</Select.Option>
                <Select.Option value="DISABLED">已停用</Select.Option>
              </Select>
            </Form.Item>
            <Button onClick={() => void load()}>刷新账号</Button>
          </Form>
          <Table<AccountUser>
            rowKey="user_id"
            dataSource={users}
            scroll={{ x: 760 }}
            pagination={{
              current: offset / 30 + 1,
              pageSize: 30,
              total,
              showSizeChanger: false,
              onChange: (p) => setOffset((p - 1) * 30),
              showTotal: (n) => "共 " + n + " 个账号",
            }}
            columns={[
              {
                title: "账号",
                dataIndex: "username",
                render: (value, u) => (
                  <>
                    <Typography.Text strong>{value}</Typography.Text>
                    <div>
                      <Typography.Text type="secondary">
                        {u.display_name !== value ? u.display_name : ""}
                      </Typography.Text>
                    </div>
                  </>
                ),
              },
              {
                title: "状态",
                dataIndex: "status",
                render: (value) => (
                  <Tag
                    color={
                      value === "ACTIVE"
                        ? "success"
                        : value === "PENDING"
                          ? "processing"
                          : "default"
                    }
                  >
                    {value === "ACTIVE"
                      ? "已开通"
                      : value === "PENDING"
                        ? "待开通"
                        : "已停用"}
                  </Tag>
                ),
              },
              {
                title: "角色",
                key: "roles",
                render: (_, u) =>
                  u.role_ids.map((id) => <Tag key={id}>{names[id] || id}</Tag>),
              },
              {
                title: "注册时间",
                dataIndex: "created_at",
                render: (value) =>
                  new Date(value).toLocaleString("zh-CN", { hour12: false }),
              },
              {
                title: "操作",
                key: "edit",
                render: (_, u) => (
                  <Button
                    type="link"
                    onClick={() => {
                      setSelected(u);
                      setStatus(u.status === "PENDING" ? "ACTIVE" : u.status);
                      setUserRoles(u.role_ids);
                      setReason("");
                    }}
                  >
                    开通 / 编辑
                  </Button>
                ),
              },
            ]}
          />
          {selected && (
            <Card className="page-card" title={`管理 ${selected.username}`}>
              <Form layout="vertical" onSubmitCapture={saveUser}>
                <p>
                  账号只能选择一个角色。当前系统必须保留至少一名 Agora
                  人员；如果 admin 是最后一名 Agora 人员，请先开通另一名 Agora
                  人员，再将 admin 改为客户运维。
                </p>
                <Form.Item label="状态" htmlFor="accounts-field-8">
                  <Select
                    id="accounts-field-8"
                    style={{ minWidth: 180, width: "100%" }}
                    value={status}
                    onChange={(value) => setStatus(value)}
                  >
                    <Select.Option value="ACTIVE">开通</Select.Option>
                    <Select.Option value="DISABLED">停用</Select.Option>
                    <Select.Option value="PENDING">待开通</Select.Option>
                  </Select>
                </Form.Item>
                <Form.Item label="分配角色">
                  <Radio.Group
                    value={userRoles[0]}
                    onChange={(e) => setUserRoles([e.target.value])}
                    options={roles.map((r) => ({
                      value: r.role_id,
                      label: r.name,
                    }))}
                  />
                </Form.Item>
                <Form.Item label="变更依据" htmlFor="accounts-field-9">
                  <Input.TextArea
                    id="accounts-field-9"
                    required
                    minLength={2}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </Form.Item>
                <div className="actions">
                  <Button htmlType="button" onClick={() => setSelected(null)}>
                    取消
                  </Button>
                  <Button htmlType="submit" type="primary" disabled={busy}>
                    保存账号授权
                  </Button>
                </div>
              </Form>
            </Card>
          )}
        </>
      )}
      {canRoles && (
        <Card className="page-card">
          <Typography.Title level={3}>角色权限</Typography.Title>
          <p>角色权限固定，不支持创建自定义角色。每个账号只能分配一类角色。</p>
          <List
            dataSource={roles}
            renderItem={(r) => (
              <List.Item>
                <List.Item.Meta title={r.name} description={r.description} />
              </List.Item>
            )}
          />
        </Card>
      )}
    </section>
  );
}
