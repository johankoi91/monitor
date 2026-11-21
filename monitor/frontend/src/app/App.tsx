import { DownOutlined } from "@ant-design/icons";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Dropdown from "antd/es/dropdown";
import Layout from "antd/es/layout";
import Typography from "antd/es/typography";
import { useEffect, useMemo, useState } from "react";
import {
  PasswordChange,
  Register,
  type AccountUser,
} from "../features/accounts/Accounts";
import { client } from "../shared/api";

import Login from "../features/auth/Login";
import { errorMessage as message } from "../shared/format";
import Workspace from "./Workspace";
const authorizationStorageKey = "avops-monitor-authorization";
export default function App() {
  const [authorization, setAuthorization] = useState(""),
    [loginError, setLoginError] = useState(""),
    [canManageKeys, setCanManageKeys] = useState(false),
    [registering, setRegistering] = useState(false),
    [user, setUser] = useState<AccountUser | null>(null),
    [permissions, setPermissions] = useState<string[]>([]),
    [changingPassword, setChangingPassword] = useState(false),
    [restoring, setRestoring] = useState(true);
  useEffect(() => {
    const replaced = () => setLoginError("账号已在其他位置登录，请重新登录");
    window.addEventListener("session-replaced", replaced);
    return () => window.removeEventListener("session-replaced", replaced);
  }, []);
  const api = useMemo(
    () =>
      client(authorization, () => {
        localStorage.removeItem(authorizationStorageKey);
        setAuthorization("");
        setLoginError("凭证无效，请重新登录");
      }),
    [authorization],
  );
  useEffect(() => {
    const savedAuthorization = localStorage.getItem(authorizationStorageKey);
    if (!savedAuthorization) {
      setRestoring(false);
      return;
    }
    void (async () => {
      try {
        const version = await client(savedAuthorization, () => {}).get<{
          credential_purpose?: string;
          can_manage_keys?: boolean;
          user?: AccountUser | null;
          permissions?: string[];
        }>("/version");
        if (version.credential_purpose === "RESTART")
          throw new Error("重启 Token 仅用于操作接口，请使用账号登录");
        setCanManageKeys(!!version.can_manage_keys);
        setUser(version.user || null);
        setPermissions(version.permissions || []);
        setAuthorization(savedAuthorization);
      } catch (e) {
        localStorage.removeItem(authorizationStorageKey);
        setLoginError(message(e));
      } finally {
        setRestoring(false);
      }
    })();
  }, []);
  async function login(id: string, secret: string) {
    const response = await fetch("/api/v1/auth/login", {
      method: "POST",
      credentials: "omit",
      headers: { "Content-Type": "application/json", "X-AVOPS-Request": "1" },
      body: JSON.stringify({ username: id, password: secret }),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.message || "账号登录失败");
    const token = `Bearer ${result.token}`;
    const version = await client(token, () => {}).get<{
      credential_purpose?: string;
      can_manage_keys?: boolean;
      user?: AccountUser | null;
      permissions?: string[];
    }>("/version");
    if (version.credential_purpose === "RESTART")
      throw new Error("重启 Token 仅用于操作接口，请使用账号登录");
    setCanManageKeys(!!version.can_manage_keys);
    setUser(version.user || null);
    setPermissions(version.permissions || []);
    setLoginError("");
    localStorage.setItem(authorizationStorageKey, token);
    setAuthorization(token);
  }
  return (
    <>
      <Layout.Header className="app-header">
        <div className="brand-group">
          <span className="brand-mark">AV</span>
          <div>
            <Typography.Title level={1}>V1.0 监控</Typography.Title>
            <small>集中状态 · 容器基线 · 受控操作</small>
          </div>
        </div>
        {authorization && (
          <div className="session-actions">
            {user && (
              <Dropdown
                trigger={["click"]}
                menu={{
                  items: [
                    { key: "password", label: "修改密码" },
                    { type: "divider" },
                    { key: "logout", label: "退出登录", danger: true },
                  ],
                  onClick: ({ key }) => {
                    if (key === "password") setChangingPassword(true);
                    if (key === "logout") {
                      void (async () => {
                        if (api.accountSession)
                          try {
                            await api.post("/api/v1/auth/logout", {});
                          } catch {}
                        localStorage.removeItem(authorizationStorageKey);
                        setAuthorization("");
                        setCanManageKeys(false);
                        setUser(null);
                        setPermissions([]);
                      })();
                    }
                  },
                }}
              >
                <Button type="text" className="profile-trigger">
                  {user.display_name} ·{" "}
                  {user.role_ids.includes("agora") ? "管理者" : "使用者"}{" "}
                  <DownOutlined />
                </Button>
              </Dropdown>
            )}
          </div>
        )}
      </Layout.Header>
      <Layout.Content className="app-content">
        {restoring ? (
          <Card className="page-card login-panel">登录状态恢复中…</Card>
        ) : authorization ? (
          <Workspace
            api={api}
            canManageKeys={canManageKeys}
            permissions={permissions}
          />
        ) : registering ? (
          <Register onClose={() => setRegistering(false)} />
        ) : (
          <>
            <Login onLogin={login} error={loginError} />
            <div className="login-register">
              <Button type="link" onClick={() => setRegistering(true)}>
                注册账号
              </Button>
            </div>
          </>
        )}
      </Layout.Content>
      {changingPassword && (
        <PasswordChange
          api={api}
          onClose={() => setChangingPassword(false)}
          onChanged={() => {
            setChangingPassword(false);
            localStorage.removeItem(authorizationStorageKey);
            setAuthorization("");
            setUser(null);
            setPermissions([]);
            setLoginError("密码已修改，请重新登录");
          }}
        />
      )}
    </>
  );
}
