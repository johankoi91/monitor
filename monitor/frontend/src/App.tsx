import Table from "antd/es/table";
import AutoComplete from "antd/es/auto-complete";
import Pagination from "antd/es/pagination";
import Form from "antd/es/form";
import Checkbox from "antd/es/checkbox";
import Select from "antd/es/select";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import AntEmpty from "antd/es/empty";
import Input from "antd/es/input";
import AntModal from "antd/es/modal";
import Tag from "antd/es/tag";
import Card from "antd/es/card";
import Col from "antd/es/col";
import List from "antd/es/list";
import Row from "antd/es/row";
import Statistic from "antd/es/statistic";
import Typography from "antd/es/typography";
import Dropdown from "antd/es/dropdown";
import Breadcrumb from "antd/es/breadcrumb";
import Skeleton from "antd/es/skeleton";
import WorkspaceNavigation from "./WorkspaceNavigation";
import Subpages from "./Subpages";
import Overview from "./Overview";
import { useWorkspaceNavigation, type RTCPane } from "./navigation";
import Layout from "antd/es/layout";
import { DownOutlined } from "@ant-design/icons";
import { client, type API } from "./api";
import { checksForPorts } from "./checks";
import { resolveGroup } from "./groups";
import NotificationSettingsForm from "./NotificationSettings";
import StorageStatus from "./StorageStatus";
import RTCProbes from "./RTCProbes";
import AccessKeys from "./AccessKeys";
import Resources from "./Resources";
import Operations, { OperationDetails } from "./Operations";
import StartupParameters from "./StartupParameters";
import Accounts, {
  Register,
  PasswordChange,
  type AccountUser,
} from "./Accounts";
import type {
  Addition,
  Agent,
  Baseline,
  Container,
  NodeStatus,
  Node,
  Notifications,
  Operation,
  ServiceStatus,
} from "./types";

const stamp = (value?: string | null) =>
  value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "暂无";
const key = (c: Container) => `${c.agent_id}/${c.container_id}`;
const authorizationStorageKey = "avops-monitor-authorization";
const message = (e: unknown) => (e instanceof Error ? e.message : "请求未完成");
const operationLabels: Record<string, string> = {
  ACCEPTED: "已受理",
  EXECUTE_INTENT: "执行前核验通过",
  WAIT_RESULT: "正在重启",
  VERIFY: "正在复查",
  UNCERTAIN: "结果未知，需现场核实",
  SUCCESS: "成功",
  FAILED: "失败",
  REJECTED: "已拒绝",
  TIMEOUT: "超时",
  MANUALLY_CLOSED: "已现场核实关闭",
  DELIVERED: "已送达",
  PENDING: "待发",
  INFLIGHT: "投递中",
  DROPPED: "容量丢弃",
};
function Badge({ value, label }: { value: string; label?: string }) {
  const colors: Record<string, string> = {
    HEALTHY: "success",
    DELIVERED: "success",
    FAILED: "error",
    PENDING: "processing",
    INFLIGHT: "processing",
    RUNNING: "success",
    running: "success",
    UNHEALTHY: "error",
    DEAD: "error",
    DEGRADED: "warning",
    UNKNOWN: "default",
    CREATED: "processing",
    created: "processing",
  };
  return (
    <Tag color={colors[value] || "default"}>
      {label || operationLabels[value] || value}
    </Tag>
  );
}
function Empty({ children }: { children: ReactNode }) {
  return (
    <AntEmpty image={AntEmpty.PRESENTED_IMAGE_SIMPLE} description={children} />
  );
}
function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <AntModal
      open
      title={title}
      footer={null}
      onCancel={onClose}
      destroyOnClose
    >
      {children}
    </AntModal>
  );
}
function Login({
  onLogin,
  error,
}: {
  onLogin: (id: string, secret: string) => Promise<void>;
  error: string;
}) {
  const [id, setID] = useState(""),
    [secret, setSecret] = useState(""),
    [busy, setBusy] = useState(false),
    [localError, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (!id.trim() || id.includes(":") || !secret) {
      setError("请填写有效的账号和密码");
      return;
    }
    setBusy(true);
    setError("");
    const value = secret;
    setSecret("");
    try {
      await onLogin(id.trim(), value);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card className="page-card login-panel">
      <Typography.Title level={2}>登录监控系统</Typography.Title>
      <p>使用已开通账号登录，权限由所分配角色决定。</p>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="账号" htmlFor="app-field-1">
          <Input
            id="app-field-1"
            autoComplete="username"
            value={id}
            onChange={(e) => setID(e.target.value)}
            required
          />
        </Form.Item>
        <Form.Item label="密码" htmlFor="app-field-2">
          <Input.Password
            id="app-field-2"
            autoComplete="current-password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            required
          />
        </Form.Item>
        {(localError || error) && (
          <Alert message={localError || error} type="error" showIcon />
        )}
        <Button type="primary" htmlType="submit" disabled={busy}>
          {busy ? "登录中…" : "登录"}
        </Button>
      </Form>
    </Card>
  );
}
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
            <Typography.Title level={1}>音视频运维监控</Typography.Title>
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
      <Layout.Footer className="app-footer">
        AGENT / CONTAINER / READINESS · 容器与浅层就绪状态
      </Layout.Footer>
    </>
  );
}
function Workspace({
  api,
  canManageKeys,
  permissions,
}: {
  api: API;
  canManageKeys: boolean;
  permissions: string[];
}) {
  const can = (p: string) => permissions.includes(p);
  const { route, sections, current, section, navigate } =
    useWorkspaceNavigation(permissions, canManageKeys);
  const tab = route.view;
  const setTab = (view: typeof tab) => {
    const item = sections
      .flatMap((section) => section.items)
      .find((item) => item.route.view === view);
    if (view === "overview") navigate({ view, page: "" });
    else if (item) navigate(item.route);
  };
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState<Record<string, boolean>>({});
  const [updatedAt, setUpdatedAt] = useState<Record<string, string>>({});
  const [loadErrors, setLoadErrors] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState(""),
    [noticeType, setNoticeType] = useState<"info" | "error">("info"),
    [baseline, setBaseline] = useState<Baseline | null>(null),
    [agents, setAgents] = useState<Agent[]>([]),
    [status, setStatus] = useState<ServiceStatus[]>([]),
    [statusGroup, setStatusGroup] = useState(""),
    [notificationCategory, setNotificationCategory] = useState(""),
    [statusHost, setStatusHost] = useState(""),
    [serviceSearch, setServiceSearch] = useState(""),
    [notificationResult, setNotificationResult] = useState(""),
    [operations, setOperations] = useState<Operation[]>([]),
    [notifications, setNotifications] = useState<Notifications>({
      enabled: false,
    });
  const [filters, setFilters] = useState({
      agent_id: "",
      runtime_status: "running",
    }),
    [query, setQuery] = useState(filters),
    [page, setPage] = useState(1),
    [total, setTotal] = useState(0),
    [containers, setContainers] = useState<Container[]>([]);
  const [chosen, setChosen] = useState<Map<string, Container>>(new Map()),
    [additions, setAdditions] = useState<Map<string, Addition>>(new Map()),
    [removals, setRemovals] = useState<Set<string>>(new Set()),
    [expandedStatusDetails, setExpandedStatusDetails] = useState<Set<string>>(
      new Set(
        new URLSearchParams(window.location.search).get("node")
          ? [new URLSearchParams(window.location.search).get("node")!]
          : [],
      ),
    ),
    [saving, setSaving] = useState(false);
  const [groupName, setGroupName] = useState("");
  const inventoryPane = route.page;
  const [tcpPort, setTCPPort] = useState("");
  const [detail, setDetail] = useState<unknown>(null),
    [portsNode, setPortsNode] = useState<Node | null>(null),
    [restart, setRestart] = useState<NodeStatus | null>(null),
    [operation, setOperation] = useState<Operation | null>(null),
    [resolve, setResolve] = useState<Operation | null>(null);
  useEffect(() => {
    const restore = () => {
      const node = new URLSearchParams(window.location.search).get("node");
      setExpandedStatusDetails(new Set(node ? [node] : []));
    };
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);
  function openNode(id: string | null) {
    const url = new URL(window.location.href);
    if (id) url.searchParams.set("node", id);
    else url.searchParams.delete("node");
    window.history.pushState(
      null,
      "",
      `${url.pathname}${url.search}${url.hash}`,
    );
    setExpandedStatusDetails(new Set(id ? [id] : []));
  }
  const draft = useRef(false),
    requestGeneration = useRef(0),
    operationsInFlight = useRef(false),
    busy = useRef(false);
  draft.current = chosen.size + additions.size + removals.size > 0;
  busy.current = saving;
  const showNotice = (value: string, type: "info" | "error" = "info") => {
    setNoticeType(type);
    setNotice(value);
  };
  const loadOperations = useCallback(async () => {
    if (operationsInFlight.current) return;
    const path = permissions.includes("operations.read")
      ? "/api/v1/operations?limit=50"
      : permissions.includes("restart.result")
        ? "/api/v1/operations/mine"
        : "";
    if (!path) return;
    operationsInFlight.current = true;
    try {
      const result = await api.get<{ operations: Operation[] }>(path);
      setOperations(result.operations);
      setOperation((old) =>
        old
          ? result.operations.find(
              (item) => item.operation_id === old.operation_id,
            ) || old
          : null,
      );
    } catch (e) {
      setNoticeType("error");
      setNotice(message(e));
    } finally {
      operationsInFlight.current = false;
    }
  }, [api, permissions]);
  const reload = useCallback(async () => {
    if (!permissions.includes("monitor.read")) return;
    const generation = ++requestGeneration.current;
    setLoading(true);
    try {
      const params = new URLSearchParams({
        ...query,
        selectable_only: "true",
        page: String(page),
        page_size: "50",
      });
      const [b, a, s, c, , n] = await Promise.all([
        tab === "inventory" && permissions.includes("baseline.read")
          ? api.get<Baseline>("/api/v1/baseline")
          : Promise.resolve(null),
        tab === "inventory"
          ? api.get<{ agents: Agent[] }>("/api/v1/agents")
          : Promise.resolve(null),
        ["overview", "status", "inventory", "operations"].includes(tab)
          ? api.get<{ services: ServiceStatus[] }>(
              tab === "status"
                ? "/api/v1/services/status?include_startup=true"
                : "/api/v1/services/status",
            )
          : Promise.resolve(null),
        tab === "inventory" && permissions.includes("inventory.read")
          ? api.get<{ containers: Container[]; total: number }>(
              `/api/v1/containers?${params}`,
            )
          : Promise.resolve(null),
        tab === "operations" ? loadOperations() : Promise.resolve(),
        ["notifications", "overview"].includes(tab)
          ? api.get<Notifications>("/api/v1/notifications/status")
          : Promise.resolve(null),
      ]);
      if (generation !== requestGeneration.current) return;
      if (c) {
        const lastPage = Math.max(1, Math.ceil(c.total / 50));
        if (page > lastPage) setPage(lastPage);
        setContainers(c.containers);
        setTotal(c.total);
      }
      if (b)
        setBaseline((old) => {
          if (!old || !draft.current) return b;
          if (old.revision !== b.revision)
            showNotice(
              "基准已被其他请求更新。请清空变更并刷新后重新确认。",
              "error",
            );
          return old;
        });
      if (a) setAgents(a.agents);
      if (s) setStatus(s.services);
      if (n) setNotifications(n);
      setLoaded((old) => ({ ...old, [tab]: true }));
      setUpdatedAt((old) => ({ ...old, [tab]: new Date().toISOString() }));
      setLoadErrors((old) => ({ ...old, [tab]: "" }));
    } catch (e) {
      if (generation === requestGeneration.current)
        setLoadErrors((old) => ({ ...old, [tab]: message(e) }));
    } finally {
      if (generation === requestGeneration.current) setLoading(false);
    }
  }, [api, query, page, permissions, tab, loadOperations]);
  useEffect(() => {
    if (
      ![
        "overview",
        "status",
        "inventory",
        "operations",
        "notifications",
      ].includes(tab)
    )
      return;
    void reload();
    const timer = setInterval(() => {
      if (!document.hidden && !busy.current) void reload();
    }, 15000);
    return () => {
      requestGeneration.current++;
      clearInterval(timer);
    };
  }, [reload, tab]);
  useEffect(() => {
    if (tab !== "operations" && !operation?.node_locked) return;
    if (!operations.some((o) => o.node_locked) && !operation?.node_locked)
      return;
    const timer = setInterval(() => {
      if (!document.hidden) void loadOperations();
    }, 2000);
    return () => clearInterval(timer);
  }, [
    loadOperations,
    tab,
    operations.some((o) => o.node_locked),
    operation?.node_locked,
  ]);
  useEffect(() => {
    setNotice("");
  }, [tab, route.page]);
  useEffect(() => {
    if (!notice || noticeType === "error") return;
    const timer = window.setTimeout(() => setNotice(""), 6000);
    return () => window.clearTimeout(timer);
  }, [notice, noticeType]);
  const members =
    baseline?.definition?.clusters.flatMap((c) =>
      c.services.flatMap((s) =>
        s.nodes.map((n) => ({ ...n, cluster: c, service: s })),
      ),
    ) || [];
  const observedStatus = loadErrors[tab]
    ? status.map((service) => ({
        ...service,
        status: "UNKNOWN",
        nodes: service.nodes.map((node) => ({
          ...node,
          stale: true,
          status: "UNKNOWN",
          resources: node.resources
            ? { ...node.resources, stale: true }
            : node.resources,
        })),
      }))
    : status;
  const nodeStatuses = new Map(
    observedStatus.flatMap((s) => s.nodes.map((n) => [n.node_id, n] as const)),
  );
  const memberGroups = new Map(
    members.map((n) => [n.id, n.cluster.name || n.cluster.code]),
  );
  const statusGroups = [
    ...observedStatus
      .reduce((groups, service) => {
        const group = groups.get(service.cluster_code);
        if (group) group.nodes.push(...service.nodes);
        else
          groups.set(service.cluster_code, {
            ...service,
            nodes: [...service.nodes],
          });
        return groups;
      }, new Map<string, ServiceStatus>())
      .values(),
  ].filter((group) => group.nodes.length > 0);
  const statusHosts = [
    ...new Set(
      observedStatus.flatMap((s) => s.nodes.map((n) => n.host_address)),
    ),
  ].sort();
  const visibleStatus = statusGroups
    .filter((s) => !statusGroup || s.cluster_code === statusGroup)
    .map((s) => {
      const nodes = s.nodes.filter(
        (n) => !statusHost || n.host_address === statusHost,
      );
      const healthy = nodes.filter((n) => n.status === "HEALTHY").length;
      const unhealthy = nodes.filter((n) => n.status === "UNHEALTHY").length;
      const visibleHealth =
        nodes.length && healthy === nodes.length
          ? "HEALTHY"
          : healthy > 0
            ? "DEGRADED"
            : unhealthy > 0
              ? "UNHEALTHY"
              : "UNKNOWN";
      const hosts = [
        ...nodes
          .reduce((map, node) => {
            const list = map.get(node.host_address) || [];
            list.push(node);
            map.set(node.host_address, list);
            return map;
          }, new Map<string, NodeStatus[]>())
          .entries(),
      ].map(([address, nodes]) => ({ address, nodes }));
      return {
        ...s,
        hosts,
        nodes,
        status: visibleHealth,
        healthy_node_count: healthy,
        unhealthy_node_count: unhealthy,
        unknown_node_count: nodes.filter((n) => n.status === "UNKNOWN").length,
      };
    })
    .filter((s) => s.nodes.length > 0);
  const eligible = (c: Container) => c.agent_online && !c.stale && !c.managed;
  function select(c: Container, checked: boolean) {
    setChosen((previous) => {
      const next = new Map(previous);
      if (checked) next.set(key(c), c);
      else next.delete(key(c));
      return next;
    });
  }
  const groups = [
    ...(baseline?.definition?.clusters || []),
    ...Array.from(additions.values(), (a) => ({
      code: a.cluster_code,
      name: a.cluster_name || a.cluster_code,
    })),
  ];
  async function stage() {
    let checks;
    let group;
    try {
      checks = checksForPorts(tcpPort);
      group = await resolveGroup(groupName, groups);
    } catch (e) {
      showNotice(message(e), "error");
      return;
    }
    setAdditions((previous) => {
      const next = new Map(previous);
      chosen.forEach((c, k) =>
        next.set(k, {
          agent_id: c.agent_id,
          container_id: c.container_id,
          container_name: c.container_name,
          snapshot_id: c.snapshot_id,
          cluster_code: group.code,
          cluster_name: group.name,
          host_address: c.host_address,
          checks,
        }),
      );
      return next;
    });
    setChosen(new Map());
    setGroupName(group.name);
    setNotice("");
  }
  async function save() {
    if (saving || !baseline) return;
    if (chosen.size) {
      showNotice("请先将已选容器加入待保存变更", "error");
      return;
    }
    setSaving(true);
    try {
      const result = await api.post<Baseline>("/api/v1/baseline/selection", {
        expected_revision: baseline.revision,
        additions: Array.from(
          additions.values(),
          ({ container_name, host_address, ...item }) => item,
        ),
        removals: [...removals],
      });
      draft.current = false;
      setBaseline(result);
      setAdditions(new Map());
      setRemovals(new Set());
      setNotice("基准已保存并生效，可下载 YAML。");
      await reload();
    } catch (e) {
      showNotice(
        `${message(e)}。变更已保留；若请求超时，请先核对当前基准。`,
        "error",
      );
    } finally {
      setSaving(false);
    }
  }
  function clear() {
    setChosen(new Map());
    setAdditions(new Map());
    setRemovals(new Set());
    draft.current = false;
    setGroupName("");
    setTCPPort("");
    setNotice("变更已清空");
    void reload();
  }
  function remove(id: string, agent: string, name: string) {
    setRemovals((previous) => {
      const next = new Set(previous);
      if (next.has(id)) {
        next.delete(id);
        setAdditions(
          (items) =>
            new Map(
              [...items].filter(
                ([, a]) => a.agent_id !== agent || a.container_name !== name,
              ),
            ),
        );
        setChosen(
          (items) => new Map([...items].filter(([, c]) => c.node_id !== id)),
        );
      } else next.add(id);
      return next;
    });
  }
  async function showDetail(c: Container) {
    try {
      const value = await api.get<any>(
        `/api/v1/containers/${encodeURIComponent(c.agent_id)}/${encodeURIComponent(c.container_id)}`,
      );
      const strip = (item: any): any => {
        if (Array.isArray(item)) return item.map(strip);
        if (!item || typeof item !== "object") return item;
        return Object.fromEntries(
          Object.entries(item)
            .filter(
              ([name]) => name !== "resources" && name !== "resource_metrics",
            )
            .map(([name, child]) => [name, strip(child)]),
        );
      };
      setDetail(strip(value));
    } catch (e) {
      showNotice(message(e), "error");
    }
  }
  async function showOperation(id: string) {
    try {
      const response = await api.get<{ operation: Operation }>(
        `/api/v1/operations/${id}`,
      );
      setOperation(response.operation);
    } catch (e) {
      showNotice(message(e), "error");
    }
  }
  return (
    <>
      <div className="workspace-shell">
        <WorkspaceNavigation
          sections={sections}
          route={route}
          activeSection={section?.id}
          canOverview={can("monitor.read")}
          onNavigate={navigate}
        />
        <main className="workspace-main" id="workspace-main">
          <Breadcrumb
            items={[
              {
                title: can("monitor.read") ? (
                  <Button type="link" onClick={() => setTab("overview")}>
                    工作台
                  </Button>
                ) : (
                  "工作台"
                ),
              },
              ...(section ? [{ title: section.label }] : []),
              { title: current?.label || "运行总览" },
            ]}
          />
          <div className="workspace-page-heading">
            <div>
              <Typography.Title
                level={2}
                id="workspace-page-title"
                tabIndex={-1}
              >
                {current?.label || "运行总览"}
              </Typography.Title>
              <Typography.Paragraph type="secondary">
                {current?.description ||
                  "分开判断容器状态、RTC 业务证据与通知投递结果。"}
              </Typography.Paragraph>
            </div>
            {[
              "overview",
              "status",
              "inventory",
              "notifications",
              "operations",
            ].includes(tab) && (
              <div className="workspace-refresh">
                <Typography.Text type="secondary">
                  {updatedAt[tab]
                    ? `读取于 ${stamp(updatedAt[tab])}`
                    : "等待首次读取"}
                </Typography.Text>
                <Button loading={loading} onClick={() => void reload()}>
                  刷新
                </Button>
              </div>
            )}
          </div>
          {[
            "overview",
            "status",
            "inventory",
            "notifications",
            "operations",
          ].includes(tab) &&
          !loaded[tab] &&
          loading ? (
            <Skeleton active paragraph={{ rows: 5 }} />
          ) : null}
          {loadErrors[tab] && (
            <Alert
              className="page-alert"
              type="error"
              showIcon
              message={loadErrors[tab]}
              description={
                loaded[tab]
                  ? "当前状态待确认，下方保留最后一次成功读取的快照。恢复读取后再判断当前健康状态。"
                  : "尚未读到有效数据，请重试。"
              }
              action={<Button onClick={() => void reload()}>重试</Button>}
            />
          )}
          <div
            className="workspace-page"
            hidden={
              [
                "overview",
                "status",
                "inventory",
                "notifications",
                "operations",
              ].includes(tab) && !loaded[tab]
            }
          >
            {tab === "overview" && can("monitor.read") && (
              <Overview
                api={api}
                services={observedStatus}
                notifications={notifications}
                onNavigate={navigate}
              />
            )}
            {notice && (noticeType === "info" || tab !== "inventory") && (
              <Alert
                message={notice}
                type={noticeType}
                showIcon
                className="page-alert"
              />
            )}
            {tab === "status" && (
              <section>
                <Form layout="vertical" className="filters status-filters">
                  <Form.Item
                    label="搜索容器、主机或分组"
                    htmlFor="service-search"
                  >
                    <Input
                      id="service-search"
                      allowClear
                      value={serviceSearch}
                      onChange={(e) => setServiceSearch(e.target.value)}
                    />
                  </Form.Item>

                  <Form.Item label="主机 IP">
                    <Select
                      style={{ minWidth: 200 }}
                      value={statusHost}
                      onChange={setStatusHost}
                      options={[
                        { value: "", label: "全部主机" },
                        ...statusHosts.map((value) => ({
                          value,
                          label: value,
                        })),
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="分组名" htmlFor="app-field-status-group">
                    <Select
                      id="app-field-status-group"
                      style={{ minWidth: 240, width: "100%" }}
                      value={statusGroup}
                      onChange={setStatusGroup}
                    >
                      <Select.Option value="">全部分组</Select.Option>
                      {statusGroups.map((group) => (
                        <Select.Option
                          key={group.cluster_code}
                          value={group.cluster_code}
                        >
                          {group.group_name || group.cluster_code}
                        </Select.Option>
                      ))}
                    </Select>
                  </Form.Item>
                </Form>
                {!status.length && (
                  <Empty>
                    尚未保存基准。进入“容器与基准台账”选择要监控的容器。
                  </Empty>
                )}
                {status.length > 0 && !visibleStatus.length && (
                  <Empty>没有匹配的分组状态。</Empty>
                )}
                <Card className="page-card">
                  <Table<NodeStatus & { groupLabel: string }>
                    size="small"
                    rowKey="node_id"
                    scroll={{ x: 760 }}
                    pagination={{
                      defaultPageSize: 20,
                      showSizeChanger: true,
                      showTotal: (total) => `共 ${total} 个容器`,
                    }}
                    dataSource={visibleStatus
                      .flatMap((s) =>
                        s.nodes.map((n) => ({
                          ...n,
                          groupLabel: s.group_name || s.cluster_code,
                        })),
                      )
                      .filter((n) =>
                        `${n.container_name} ${n.host_address} ${n.groupLabel}`
                          .toLowerCase()
                          .includes(serviceSearch.toLowerCase()),
                      )}
                    columns={[
                      {
                        title: "容器 / 分组",
                        render: (_, n) => (
                          <>
                            <span className="workspace-service-name">
                              {n.container_name}
                            </span>
                            <span className="workspace-service-note">
                              {n.groupLabel}
                            </span>
                          </>
                        ),
                      },
                      {
                        title: "主机 IP",
                        dataIndex: "host_address",
                        width: 160,
                      },
                      {
                        title: "检查状态",
                        width: 180,
                        render: (_, n) => (
                          <>
                            <Badge
                              value={n.stale ? "UNKNOWN" : n.status}
                              label={
                                n.stale
                                  ? "数据过期 · 待确认"
                                  : {
                                      HEALTHY: "正常",
                                      UNHEALTHY: "异常",
                                      DEGRADED: "部分异常",
                                      UNKNOWN: "待确认",
                                    }[n.status] || n.status
                              }
                            />
                            <span className="workspace-service-note">
                              {n.reason}
                            </span>
                          </>
                        ),
                      },
                      {
                        title: "资源 / 采样",
                        width: 180,
                        render: (_, n) => (
                          <>
                            <span>
                              {n.resources &&
                              !n.resources.stale &&
                              n.resources.status !== "UNAVAILABLE"
                                ? `${n.resources.cpu_usage_cores == null ? "—" : n.resources.cpu_usage_cores.toFixed(4)} 核 · ${n.resources.memory_working_set_bytes == null ? "—" : (n.resources.memory_working_set_bytes / 1048576).toFixed(1)} MiB`
                                : "等待有效资源采样"}
                            </span>
                            <span className="workspace-service-note">
                              {stamp(n.collected_at)}
                            </span>
                          </>
                        ),
                      },
                      {
                        title: "操作",
                        width: 100,
                        render: (_, n) => (
                          <Button
                            type="link"
                            onClick={() => openNode(n.node_id)}
                          >
                            查看详情
                          </Button>
                        ),
                      },
                    ]}
                  />
                </Card>
                {observedStatus
                  .flatMap((s) => s.nodes)
                  .filter((n) => expandedStatusDetails.has(n.node_id))
                  .map((n) => (
                    <Card
                      key={n.node_id}
                      className="workspace-diagnostic"
                      title={n.container_name}
                      extra={
                        <Button onClick={() => openNode(null)}>
                          返回容器列表
                        </Button>
                      }
                    >
                      <div className="section-title">
                        <div>
                          <Typography.Text>{n.host_address}</Typography.Text>
                          <span className="workspace-service-note">
                            {n.reason}
                          </span>
                        </div>
                        <div>
                          <Badge
                            value={n.stale ? "UNKNOWN" : n.status}
                            label={
                              n.stale
                                ? "待确认"
                                : {
                                    HEALTHY: "正常",
                                    UNHEALTHY: "异常",
                                    DEGRADED: "部分异常",
                                    UNKNOWN: "待确认",
                                  }[n.status] || n.status
                            }
                          />
                          {can("restart.execute") && (
                            <Button
                              danger
                              disabled={!n.restart_enabled}
                              onClick={() => setRestart(n)}
                            >
                              重启
                            </Button>
                          )}
                        </div>
                      </div>
                      <Resources
                        api={api}
                        nodeID={n.node_id}
                        value={n.resources}
                        lifecycle={n.lifecycle}
                        runtime={n.runtime_status}
                        updatedAt={n.collected_at}
                        dataStale={n.stale}
                        startup={<StartupParameters data={n.startup_record} />}
                        detailsOpen
                      />
                    </Card>
                  ))}
              </section>
            )}
            {tab === "rtc" && can("monitor.read") && (
              <RTCProbes
                api={api}
                canManage={can("rtc.manage")}
                canDiagnose={can("rtc.diagnostics")}
                pane={route.page as RTCPane}
              />
            )}
            {tab === "inventory" && can("inventory.read") && (
              <section className="inventory-screen">
                <div className="inventory-layout">
                  <Card className="inventory-main">
                    <Subpages
                      activeKey={inventoryPane}
                      items={[
                        {
                          key: "discovery",
                          label: "容器发现",
                          children: (
                            <>
                              {" "}
                              <Form
                                layout="vertical"
                                className="filters inventory-filters"
                                onSubmitCapture={(e) => {
                                  e.preventDefault();
                                  setPage(1);
                                  setQuery({ ...filters });
                                }}
                              >
                                <Form.Item
                                  label="主机 IP"
                                  htmlFor="app-field-3"
                                >
                                  <Select
                                    id="app-field-3"
                                    style={{ minWidth: 180, width: "100%" }}
                                    value={filters.agent_id}
                                    onChange={(value) =>
                                      setFilters({
                                        ...filters,
                                        agent_id: value,
                                      })
                                    }
                                  >
                                    <Select.Option value="">
                                      全部机器
                                    </Select.Option>
                                    {agents.map((a) => (
                                      <Select.Option
                                        key={a.agent_id}
                                        value={a.agent_id}
                                      >
                                        {a.host_address}
                                      </Select.Option>
                                    ))}
                                  </Select>
                                </Form.Item>
                                <Form.Item label="运行态" htmlFor="app-field-4">
                                  <Select
                                    id="app-field-4"
                                    style={{ minWidth: 180, width: "100%" }}
                                    value={filters.runtime_status}
                                    onChange={(value) =>
                                      setFilters({
                                        ...filters,
                                        runtime_status: value,
                                      })
                                    }
                                  >
                                    {[
                                      ["", "全部状态"],
                                      ["running", "运行中"],
                                      ["created", "已创建"],
                                      ["exited", "已停止"],
                                      ["paused", "已暂停"],
                                      ["restarting", "重启中"],
                                    ].map(([v, label]) => (
                                      <Select.Option key={v} value={v}>
                                        {label}
                                      </Select.Option>
                                    ))}
                                  </Select>
                                </Form.Item>
                                <Button htmlType="submit">筛选</Button>
                              </Form>
                              <Table<Container>
                                rowKey={key}
                                dataSource={containers}
                                locale={{
                                  emptyText: "暂无符合筛选条件的未纳管容器",
                                }}
                                pagination={false}
                                size="small"
                                scroll={{
                                  x: 840,
                                  y: "max(220px, calc(100vh - 410px))",
                                }}
                                rowSelection={{
                                  selectedRowKeys: [...chosen.keys()],
                                  preserveSelectedRowKeys: true,
                                  getCheckboxProps: (c) => ({
                                    disabled:
                                      saving ||
                                      (!eligible(c) && !chosen.has(key(c))),
                                  }),
                                  onSelect: (c, checked) => select(c, checked),
                                  onSelectAll: (checked, _selected, changed) =>
                                    changed.forEach((c) => select(c, checked)),
                                }}
                                columns={[
                                  {
                                    title: "主机 IP",
                                    dataIndex: "host_address",
                                    width: 160,
                                  },
                                  {
                                    title: "容器 / 镜像",
                                    key: "container",
                                    width: 260,
                                    ellipsis: true,
                                    render: (_, c) => (
                                      <>
                                        <Typography.Text strong>
                                          {c.container_name}
                                        </Typography.Text>
                                        <div>
                                          <Typography.Text type="secondary">
                                            {c.image}
                                          </Typography.Text>
                                        </div>
                                      </>
                                    ),
                                  },
                                  {
                                    title: "运行态",
                                    key: "state",
                                    width: 120,
                                    render: (_, c) => (
                                      <>
                                        <Badge
                                          value={c.runtime_status}
                                          label={
                                            (
                                              {
                                                running: "运行中",
                                                created: "已创建",
                                                exited: "已停止",
                                                paused: "已暂停",
                                                restarting: "重启中",
                                                removing: "未运行",
                                                dead: "未运行",
                                              } as Record<string, string>
                                            )[c.runtime_status] ||
                                            c.runtime_status
                                          }
                                        />
                                        {c.stale && <Tag>离线 / 过期</Tag>}
                                      </>
                                    ),
                                  },
                                  {
                                    title: "采集时间",
                                    dataIndex: "collected_at",
                                    width: 180,
                                    render: stamp,
                                  },
                                  {
                                    title: "操作",
                                    key: "actions",
                                    width: 90,
                                    render: (_, c) => (
                                      <Button
                                        type="link"
                                        onClick={() => void showDetail(c)}
                                      >
                                        详情
                                      </Button>
                                    ),
                                  },
                                ]}
                              />
                              <div className="table-footer">
                                <Typography.Text type="secondary">
                                  已勾选 {chosen.size} 个 · 跨筛选和分页保留
                                </Typography.Text>
                                <Pagination
                                  current={page}
                                  total={total}
                                  pageSize={50}
                                  showSizeChanger={false}
                                  disabled={saving}
                                  onChange={setPage}
                                  showTotal={(n) => "共 " + n + " 个容器"}
                                />
                              </div>
                            </>
                          ),
                        },
                        {
                          key: "baseline",
                          label: `当前基准（${members.length}）`,
                          children: (
                            <div className="inventory-baseline">
                              <div className="section-title">
                                <Typography.Text strong>
                                  已纳管 {members.length} 个容器
                                </Typography.Text>
                                {baseline?.yaml_url && (
                                  <Button
                                    onClick={() =>
                                      void api
                                        .download(baseline.yaml_url!)
                                        .catch((e) =>
                                          showNotice(message(e), "error"),
                                        )
                                    }
                                  >
                                    下载 YAML
                                  </Button>
                                )}
                              </div>
                              <p>
                                {baseline?.active
                                  ? `版本 ${baseline.revision} · 保存于 ${stamp(baseline.saved_at)}`
                                  : "尚未建立基准"}
                              </p>
                              <Table<Node>
                                rowKey="id"
                                dataSource={members}
                                pagination={false}
                                size="small"
                                scroll={{
                                  x: 760,
                                  y: "max(220px, calc(100vh - 410px))",
                                }}
                                columns={[
                                  {
                                    title: "主机 IP",
                                    dataIndex: "host_address",
                                    width: 160,
                                  },
                                  {
                                    title: "容器",
                                    dataIndex: "container_name",
                                    render: (value) => (
                                      <Typography.Text strong>
                                        {value}
                                      </Typography.Text>
                                    ),
                                  },
                                  {
                                    title: "分组名",
                                    key: "group",
                                    width: 140,
                                    render: (_, n) => memberGroups.get(n.id),
                                  },
                                  {
                                    title: "状态",
                                    key: "state",
                                    width: 100,
                                    render: (_, n) => {
                                      const value =
                                        nodeStatuses.get(n.id)?.status ||
                                        "UNKNOWN";
                                      return (
                                        <Badge
                                          value={value}
                                          label={
                                            value === "HEALTHY"
                                              ? "正常"
                                              : value === "UNHEALTHY"
                                                ? "异常"
                                                : value === "DEGRADED"
                                                  ? "部分异常"
                                                  : "未知"
                                          }
                                        />
                                      );
                                    },
                                  },
                                  {
                                    title: "观测说明",
                                    key: "reason",
                                    render: (_, n) => (
                                      <Typography.Text type="secondary">
                                        {nodeStatuses.get(n.id)?.reason ||
                                          "等待有效快照"}
                                      </Typography.Text>
                                    ),
                                  },
                                  {
                                    title: "操作",
                                    key: "settings",
                                    width: 90,
                                    render: (_, n) => (
                                      <Button
                                        type="link"
                                        disabled={
                                          !can("baseline.write") ||
                                          saving ||
                                          draft.current ||
                                          !!nodeStatuses.get(n.id)
                                            ?.active_operation_id
                                        }
                                        onClick={() => setPortsNode(n)}
                                      >
                                        设置
                                      </Button>
                                    ),
                                  },
                                ]}
                              />
                            </div>
                          ),
                        },
                      ]}
                    />
                  </Card>
                  <Card
                    className={`inventory-workbench${chosen.size + additions.size + removals.size ? "" : " inventory-idle"}`}
                    title="纳管设置"
                    extra={
                      <Tag color={chosen.size ? "blue" : "default"}>
                        已选 {chosen.size} 个
                      </Tag>
                    }
                  >
                    <Form
                      layout="vertical"
                      component="div"
                      className="inventory-assignment"
                    >
                      <Form.Item label="分组名" required htmlFor="app-field-5">
                        <AutoComplete
                          id="app-field-5"
                          style={{ width: "100%" }}
                          value={groupName}
                          onChange={setGroupName}
                          options={[...new Set(groups.map((g) => g.name))].map(
                            (value) => ({ value }),
                          )}
                          placeholder="选择或输入分组名，支持中文"
                          disabled={saving}
                        />
                      </Form.Item>
                      <Form.Item label="TCP 端口（可选）" htmlFor="app-field-6">
                        <Input
                          id="app-field-6"
                          value={tcpPort}
                          placeholder="例如 443, 8002, 8003"
                          disabled={saving}
                          onChange={(e) => setTCPPort(e.target.value)}
                        />
                      </Form.Item>
                      <div className="inventory-chosen">
                        <Typography.Text type="secondary">
                          已选 {chosen.size} 个容器
                        </Typography.Text>
                        <Button
                          type="link"
                          size="small"
                          disabled={!chosen.size || saving}
                          onClick={() => setChosen(new Map())}
                        >
                          取消勾选
                        </Button>
                      </div>
                      <Button
                        block
                        disabled={
                          !can("baseline.write") ||
                          !chosen.size ||
                          !groupName.trim() ||
                          saving
                        }
                        onClick={() => void stage()}
                      >
                        加入待保存变更
                      </Button>
                    </Form>
                    <div className="inventory-pending-heading">
                      <Typography.Text strong>待保存变更</Typography.Text>
                      <Typography.Text type="secondary">
                        新增 {additions.size} · 移出 {removals.size}
                      </Typography.Text>
                    </div>
                    <div className="inventory-pending-list">
                      {!additions.size && !removals.size && (
                        <AntEmpty
                          image={AntEmpty.PRESENTED_IMAGE_SIMPLE}
                          description="添加容器后，在这里确认变更"
                        />
                      )}
                      {[...additions].map(([k, a]) => (
                        <div className="inventory-pending-item" key={k}>
                          <div className="inventory-pending-identity">
                            <Typography.Text
                              strong
                              ellipsis
                              title={a.container_name}
                            >
                              {a.container_name}
                            </Typography.Text>
                            <Button
                              type="text"
                              size="small"
                              aria-label={`取消新增 ${a.container_name}`}
                              disabled={saving}
                              onClick={() =>
                                setAdditions((old) => {
                                  const next = new Map(old);
                                  next.delete(k);
                                  return next;
                                })
                              }
                            >
                              ×
                            </Button>
                          </div>
                          <Typography.Text type="secondary">
                            {a.host_address} ·{" "}
                            {a.cluster_name || a.cluster_code}
                          </Typography.Text>
                          <Typography.Text type="secondary">
                            TCP：
                            {a.checks.map((c) => c.port).join(", ") || "未配置"}
                          </Typography.Text>
                        </div>
                      ))}
                      {[...removals].map((id) => {
                        const n = members.find((n) => n.id === id);
                        return (
                          <div className="inventory-pending-item" key={id}>
                            <div className="inventory-pending-identity">
                              <Typography.Text
                                ellipsis
                                title={n?.container_name || id}
                              >
                                移出：{n?.container_name || id}
                              </Typography.Text>
                              <Button
                                type="text"
                                size="small"
                                aria-label={`取消移出 ${n?.container_name || id}`}
                                disabled={saving}
                                onClick={() => {
                                  if (n)
                                    remove(id, n.agent_id, n.container_name);
                                }}
                              >
                                ×
                              </Button>
                            </div>
                            <Typography.Text type="secondary">
                              {n?.host_address}
                            </Typography.Text>
                          </div>
                        );
                      })}
                    </div>
                    {notice && noticeType === "error" && (
                      <Alert type="error" message={notice} showIcon />
                    )}
                    <div className="inventory-save-actions">
                      <Button
                        disabled={
                          saving ||
                          (!chosen.size && !additions.size && !removals.size)
                        }
                        onClick={clear}
                      >
                        清空变更
                      </Button>
                      <Button
                        type="primary"
                        loading={saving}
                        disabled={
                          !can("baseline.write") ||
                          saving ||
                          !baseline ||
                          !!chosen.size ||
                          (!additions.size && !removals.size)
                        }
                        onClick={() => void save()}
                      >
                        保存并生效
                      </Button>
                    </div>
                    {!!chosen.size && !!(additions.size + removals.size) && (
                      <Typography.Text
                        className="inventory-save-hint"
                        type="secondary"
                      >
                        还有 {chosen.size} 个已选容器未加入待保存变更
                      </Typography.Text>
                    )}
                  </Card>
                </div>
                {!!(chosen.size + additions.size + removals.size) && (
                  <Button
                    type="primary"
                    className="inventory-mobile-jump"
                    onClick={() =>
                      document
                        .querySelector(".inventory-workbench")
                        ?.scrollIntoView({ behavior: "smooth", block: "start" })
                    }
                  >
                    已选 {chosen.size} · 待保存 {additions.size + removals.size}{" "}
                    · 查看并保存
                  </Button>
                )}
              </section>
            )}
            {tab === "operations" &&
              (can("operations.read") || can("restart.result")) && (
                <>
                  <Operations
                    operations={operations}
                    services={status}
                    canAudit={can("audit.read")}
                    canResolve={can("restart.resolve")}
                    onOpen={(id) => void showOperation(id)}
                    onResolve={setResolve}
                    onRefresh={() => void reload()}
                  />
                </>
              )}
            {tab === "capacity" && can("monitor.read") && (
              <StorageStatus api={api} />
            )}
            {tab === "access" && canManageKeys && (
              <AccessKeys api={api} canManage={canManageKeys} />
            )}
            {tab === "accounts" &&
              (can("users.manage") || can("roles.manage")) && (
                <Accounts
                  api={api}
                  canUsers={can("users.manage")}
                  canRoles={can("roles.manage")}
                  pane={route.page === "roles" ? "roles" : "users"}
                />
              )}
            {tab === "notifications" && (
              <section>
                {route.page === "config" && can("notifications.write") && (
                  <NotificationSettingsForm
                    api={api}
                    onSaved={() => void reload()}
                  />
                )}
                {route.page === "delivery" && (
                  <>
                    <Card
                      title={
                        notifications.enabled ? "通知已启用" : "通知未启用"
                      }
                      className="page-card"
                    >
                      <Row gutter={[16, 16]}>
                        <Col xs={12} sm={6}>
                          <Card size="small">
                            <Statistic
                              title="待发"
                              value={notifications.pending || 0}
                            />
                          </Card>
                        </Col>
                        <Col xs={12} sm={6}>
                          <Card size="small">
                            <Statistic
                              title="累计已送达"
                              value={notifications.delivered || 0}
                              valueStyle={{ color: "#389e0d" }}
                            />
                          </Card>
                        </Col>
                        <Col xs={12} sm={6}>
                          <Card size="small">
                            <Statistic
                              title={
                                <Button
                                  type="link"
                                  onClick={() =>
                                    setNotificationResult("FAILED")
                                  }
                                >
                                  累计失败
                                </Button>
                              }
                              value={notifications.failed || 0}
                              valueStyle={{ color: "#cf1322" }}
                            />
                          </Card>
                        </Col>
                        <Col xs={12} sm={6}>
                          <Card size="small">
                            <Statistic
                              title="累计容量丢弃"
                              value={notifications.dropped || 0}
                            />
                          </Card>
                        </Col>
                      </Row>
                      {notifications.storage_available === false && (
                        <Alert
                          message="通知存储不可用，请核查数据目录。"
                          type="error"
                          showIcon
                        />
                      )}
                    </Card>
                    <Card
                      title="最近投递"
                      className="page-card"
                      extra={
                        <div className="notification-filters">
                          <Select
                            aria-label="投递结果"
                            value={notificationResult}
                            onChange={setNotificationResult}
                            options={[
                              { value: "", label: "全部结果" },
                              { value: "DELIVERED", label: "已送达" },
                              { value: "FAILED", label: "失败" },
                              { value: "PENDING", label: "待发" },
                            ]}
                          />
                          <Select
                            aria-label="事件类型"
                            style={{ minWidth: 180 }}
                            value={notificationCategory}
                            onChange={setNotificationCategory}
                            options={[
                              { value: "", label: "全部事件" },
                              { value: "NORMAL", label: "正常状态" },
                              {
                                value: "CONTAINER_ABNORMAL",
                                label: "容器异常",
                              },
                              {
                                value: "COLLECTION_ABNORMAL",
                                label: "采集异常",
                              },
                              { value: "RECOVERED", label: "恢复正常" },
                              { value: "RTC_ABNORMAL", label: "RTC 业务异常" },
                              { value: "RTC_RECOVERED", label: "RTC 业务恢复" },
                              { value: "RTC_UNKNOWN", label: "RTC 持续未知" },
                            ]}
                          />
                        </div>
                      }
                    >
                      <List
                        dataSource={(notifications.recent || []).filter(
                          (d) =>
                            (!notificationResult ||
                              d.status === notificationResult) &&
                            (!notificationCategory ||
                              (notificationCategory === "NORMAL"
                                ? d.event.current_status === "HEALTHY" &&
                                  d.event.event_type !== "WEBHOOK_TEST"
                                : d.event.event_category ===
                                  notificationCategory)),
                        )}
                        locale={{ emptyText: "暂无投递记录" }}
                        renderItem={(d) => (
                          <List.Item extra={<Badge value={d.status} />}>
                            <List.Item.Meta
                              title={
                                d.event.event_type === "WEBHOOK_TEST"
                                  ? "Webhook 健康检查"
                                  : `${({ NORMAL: "正常状态", CONTAINER_ABNORMAL: "容器异常", COLLECTION_ABNORMAL: "采集异常", RECOVERED: "恢复正常", RTC_ABNORMAL: "RTC 业务异常", RTC_RECOVERED: "RTC 业务恢复", RTC_UNKNOWN: "RTC 持续未知" } as Record<string, string>)[d.event.event_category || ""] || "状态通知"} · ${d.event.node_id || d.event.service_code}`
                              }
                              description={`${d.event.previous_status} → ${d.event.current_status} · ${stamp(d.event.occurred_at)} · 已尝试 ${d.attempts} 次${d.last_error ? ` · ${d.last_error}` : ""}`}
                            />
                          </List.Item>
                        )}
                      />
                    </Card>
                  </>
                )}
              </section>
            )}
          </div>
        </main>
      </div>
      {portsNode && baseline && (
        <NodeSettingsModal
          api={api}
          node={portsNode}
          onClose={() => setPortsNode(null)}
          onSaved={(b) => {
            setBaseline(b);
            setPortsNode(null);
            setNotice("端口已保存并生效");
            void reload();
          }}
          onRemove={() => {
            remove(portsNode.id, portsNode.agent_id, portsNode.container_name);
            setPortsNode(null);
          }}
        />
      )}
      {detail !== null && (
        <Modal title="容器启动配置" onClose={() => setDetail(null)}>
          <p>敏感值已在 Agent 上报前脱敏，配置仅供核对。</p>
          <pre>{JSON.stringify(detail, null, 2)}</pre>
        </Modal>
      )}
      {restart && (
        <RestartModal
          node={restart}
          api={api}
          onClose={() => setRestart(null)}
          onAccepted={(o) => {
            setRestart(null);
            setOperation(o);
            setTab("operations");
            void reload();
          }}
        />
      )}
      {operation && (
        <OperationDetails
          operation={operation}
          services={status}
          api={api}
          canAudit={can("audit.read")}
          onClose={() => {
            setOperation(null);
          }}
        />
      )}
      {resolve && (
        <ResolveModal
          operation={resolve}
          api={api}
          onClose={() => setResolve(null)}
          onResolved={() => {
            setResolve(null);
            void reload();
          }}
        />
      )}
    </>
  );
}
function NodeSettingsModal({
  api,
  node,
  onClose,
  onSaved,
  onRemove,
}: {
  api: API;
  node: Node;
  onClose: () => void;
  onSaved: (b: Baseline) => void;
  onRemove: () => void;
}) {
  const [ports, setPorts] = useState(
      [...new Set(node.checks.map((c) => c.port))].join(", "),
    ),
    [restartEnabled, setRestartEnabled] = useState(node.restart_enabled),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      let baseline = await api.get<Baseline>("/api/v1/baseline");
      const checks = checksForPorts(ports, node.checks);
      if (JSON.stringify(checks) !== JSON.stringify(node.checks)) {
        baseline = await api.post<Baseline>("/api/v1/baseline/checks", {
          expected_revision: baseline.revision,
          node_id: node.id,
          checks,
        });
      }
      if (restartEnabled !== node.restart_enabled) {
        baseline = await api.post<Baseline>("/api/v1/baseline/restart-policy", {
          expected_revision: baseline.revision,
          node_id: node.id,
          enabled: restartEnabled,
        });
      }
      onSaved(baseline);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="节点设置" onClose={() => !busy && onClose()}>
      <p>
        {node.host_address} / {node.container_name}
      </p>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="TCP 端口与业务探测" htmlFor="app-field-8">
          <Input
            id="app-field-8"
            autoFocus
            value={ports}
            placeholder="例如 443, 8002, 8003"
            onChange={(e) => setPorts(e.target.value)}
          />
        </Form.Item>
        <p>
          多个端口用逗号分隔；留空表示不配置 TCP 探测，仅使用容器状态和已有
          Healthcheck。
        </p>
        <Form.Item>
          <Checkbox
            checked={restartEnabled}
            onChange={(e) => setRestartEnabled(e.target.checked)}
          >
            开放该节点的重启权限（仍需独立重启 Token）
          </Checkbox>
        </Form.Item>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <div className="actions">
          <Button htmlType="button" disabled={busy} onClick={onRemove}>
            移出台账
          </Button>
          <Button htmlType="submit" type="primary" disabled={busy}>
            {busy ? "正在保存…" : "保存设置"}
          </Button>
        </div>
      </Form>
    </Modal>
  );
}

function RestartModal({
  node,
  api,
  onClose,
  onAccepted,
}: {
  node: NodeStatus;
  api: API;
  onClose: () => void;
  onAccepted: (o: Operation) => void;
}) {
  const requestKey = useRef(crypto.randomUUID()),
    [restartSecret, setRestartSecret] = useState(""),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (!restartSecret) throw new Error("请填写有效的重启 Token");
      const r = await api.postRestart<{ operation: Operation }>(
        "/api/v1/operations/restart",
        {
          node_id: node.node_id,
          reason,
          request_key: requestKey.current,
        },
        restartSecret,
      );
      onAccepted(r.operation);
    } catch (e) {
      setError(
        `${message(e)}；若未确认受理结果，再次提交将保留同一个请求标识。`,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title="确认重启原容器"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p>
        重启会中断该容器的连接。请确认当前服务影响；成功只表示当前检查层级复查通过。
      </p>
      <Card className="page-card">
        <strong>{node.node_id}</strong>
        <p>
          {node.host_address} · {node.container_name}
        </p>
        <small>
          {node.image} · {node.check_level}
        </small>
      </Card>
      {!node.restart_allowed && (
        <Alert
          showIcon
          type="warning"
          message={`当前暂不可重启：${node.restart_block_reason || "前置条件未满足"}`}
          className="page-alert"
        />
      )}
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="重启 Token" htmlFor="app-field-10">
          <Input.Password
            id="app-field-10"
            required
            value={restartSecret}
            autoComplete="off"
            onChange={(e) => setRestartSecret(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="重启原因" htmlFor="app-field-12">
          <Input.TextArea
            id="app-field-12"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            required
            minLength={2}
            maxLength={500}
          />
        </Form.Item>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <Button
          htmlType="submit"
          type="primary"
          disabled={busy || !node.restart_allowed}
        >
          {busy ? "正在受理…" : "确认重启"}
        </Button>
      </Form>
    </Modal>
  );
}
function ResolveModal({
  operation,
  api,
  onClose,
  onResolved,
}: {
  operation: Operation;
  api: API;
  onClose: () => void;
  onResolved: () => void;
}) {
  const [restartSecret, setRestartSecret] = useState(""),
    [reason, setReason] = useState(""),
    [evidence, setEvidence] = useState(""),
    [outcome, setOutcome] = useState("NOT_EXECUTED"),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!checked || busy) return;
    setBusy(true);
    try {
      if (!restartSecret) throw new Error("请填写有效的重启 Token");
      await api.postRestart(
        `/api/v1/operations/${operation.operation_id}/resolve`,
        {
          reason,
          evidence,
          outcome,
        },
        restartSecret,
      );
      onResolved();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title="现场核实未知结果"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p>
        {operation.node_id} · 关闭操作锁不会执行重启，也不会改变采集健康状态。
      </p>
      <Form layout="vertical" onSubmitCapture={submit}>
        <Form.Item label="重启 Token" htmlFor="app-field-14">
          <Input.Password
            id="app-field-14"
            required
            autoComplete="off"
            value={restartSecret}
            onChange={(e) => setRestartSecret(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="核实结论" htmlFor="app-field-16">
          <Select
            id="app-field-16"
            style={{ minWidth: 180, width: "100%" }}
            value={outcome}
            onChange={(value) => setOutcome(value)}
          >
            <Select.Option value="NOT_EXECUTED">确认未执行</Select.Option>
            <Select.Option value="EXECUTED">确认已执行</Select.Option>
          </Select>
        </Form.Item>
        <Form.Item label="核实原因" htmlFor="app-field-17">
          <Input
            id="app-field-17"
            required
            minLength={2}
            maxLength={500}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Form.Item>
        <Form.Item label="现场证据" htmlFor="app-field-18">
          <Input.TextArea
            id="app-field-18"
            required
            minLength={10}
            maxLength={1000}
            value={evidence}
            onChange={(e) => setEvidence(e.target.value)}
          />
        </Form.Item>
        <Form.Item>
          <Checkbox
            checked={checked}
            onChange={(e) => setChecked(e.target.checked)}
          >
            已核查现场，不依据旧快照推测结果
          </Checkbox>
        </Form.Item>
        {error && (
          <Alert showIcon type="error" message={error} className="page-alert" />
        )}
        <Button htmlType="submit" type="primary" disabled={!checked || busy}>
          记录核实并关闭操作锁
        </Button>
      </Form>
    </Modal>
  );
}
