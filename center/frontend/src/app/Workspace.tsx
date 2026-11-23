import {
  CheckCircleOutlined,
  CloseOutlined,
  CodeOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DeleteOutlined,
  PlusOutlined,
  SearchOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import AutoComplete from "antd/es/auto-complete";
import Breadcrumb from "antd/es/breadcrumb";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Col from "antd/es/col";
import Drawer from "antd/es/drawer";
import AntEmpty from "antd/es/empty";
import Form from "antd/es/form";
import Input from "antd/es/input";
import List from "antd/es/list";
import Row from "antd/es/row";
import Select from "antd/es/select";
import Skeleton from "antd/es/skeleton";
import Statistic from "antd/es/statistic";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { useCallback, useEffect, useRef, useState } from "react";
import Accounts from "../features/accounts/Accounts";
import AccessKeys from "../features/admin/AccessKeys";
import StorageStatus from "../features/admin/StorageStatus";
import Overview from "../features/dashboard/Overview";
import { resolveGroup } from "../features/inventory/groups";
import InventoryDiscovery from "../features/inventory/InventoryDiscovery";
import {
  InventoryContainerDetails,
  InventoryNote,
} from "../features/inventory/InventoryVisual";
import NotificationSettingsForm from "../features/notifications/NotificationSettings";
import Operations, {
  OperationDetails,
} from "../features/operations/Operations";
import type { RTCBreadcrumbTrail } from "../features/rtc/RTCBreadcrumbs";
import { followRTC } from "../features/rtc/RTCLink";
import RTCProbes from "../features/rtc/RTCProbes";
import { type API } from "../shared/api";
import { checksForPorts } from "../shared/checks";
import type {
  Addition,
  Agent,
  Baseline,
  Container,
  Node,
  NodeStatus,
  Notifications,
  Operation,
  ServiceStatus,
} from "../shared/types";
import { routeHref, useWorkspaceNavigation, type RTCPane } from "./navigation";
import { locationKey, readLocationParams } from "./routes";
import WorkspaceNavigation from "./WorkspaceNavigation";

import ContainerServices, {
  useContainerFilters,
} from "../features/monitoring/ContainerServices";
import LogCollection from "../features/monitoring/LogCollection";
import NodeSettingsModal from "../features/monitoring/NodeSettingsModal";
import ResolveModal from "../features/operations/ResolveModal";
import RestartModal from "../features/operations/RestartModal";
import {
  errorMessage as message,
  formatTimestamp as stamp,
} from "../shared/format";
import Modal from "../shared/ui/Modal";
import Badge from "../shared/ui/StatusBadge";
const key = (c: Container) => `${c.agent_id}/${c.container_id}`;
export default function Workspace({
  api,
  canManageKeys,
  permissions,
}: {
  api: API;
  canManageKeys: boolean;
  permissions: string[];
}) {
  const containerFilters = useContainerFilters();
  const can = (p: string) => permissions.includes(p);
  const { route, sections, current, section, navigate } =
    useWorkspaceNavigation(permissions, canManageKeys);
  const tab = route.view;
  const [rtcTrail, setRTCTrail] = useState<RTCBreadcrumbTrail>({
    location: "",
    items: [],
  });
  const rtcParams = readLocationParams();
  const rtcDetail =
    tab === "rtc" &&
    ["rtc_run", "rtc_task", "rtc_target"].some((key) => rtcParams.has(key));
  const trail =
    tab === "rtc" && rtcTrail.location === locationKey() ? rtcTrail.items : [];
  const crumbLink = (title: string, href: string) => (
    <a href={href} onClick={(event) => followRTC(event, href)}>
      {title}
    </a>
  );
  const setTab = (view: typeof tab) => {
    const item = sections
      .flatMap((section) => section.items)
      .find((item) => item.route.view === view);
    if (view === "overview") navigate({ view, page: "" });
    else if (item) navigate(item.route);
  };
  const [loading, setLoading] = useState(false);
  const [baselineError, setBaselineError] = useState("");
  const reloadInFlight = useRef<{
    api: API;
    key: string;
    pending: Promise<void>;
  } | null>(null);
  const baselineInFlight = useRef<{ pending: Promise<void> } | null>(null);
  const [overviewRefreshVersion, setOverviewRefreshVersion] = useState(0);
  const [loaded, setLoaded] = useState<Record<string, boolean>>({});
  const [updatedAt, setUpdatedAt] = useState<Record<string, string>>({});
  const [loadErrors, setLoadErrors] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState(""),
    [noticeType, setNoticeType] = useState<"info" | "error">("info"),
    [baseline, setBaseline] = useState<Baseline | null>(null),
    [agents, setAgents] = useState<Agent[]>([]),
    [status, setStatus] = useState<ServiceStatus[]>([]),
    [notificationCategory, setNotificationCategory] = useState(""),
    [notificationResult, setNotificationResult] = useState(""),
    [operations, setOperations] = useState<Operation[]>([]),
    [notifications, setNotifications] = useState<Notifications>({
      enabled: false,
    });
  const [filters, setFilters] = useState({
      agent_id: "",
      runtime_status: "running",
      name: "",
    }),
    [query, setQuery] = useState(filters),
    [page, setPage] = useState(1),
    [total, setTotal] = useState(0),
    [containers, setContainers] = useState<Container[]>([]);
  const [chosen, setChosen] = useState<Map<string, Container>>(new Map()),
    [additions, setAdditions] = useState<Map<string, Addition>>(new Map()),
    [removals, setRemovals] = useState<Set<string>>(new Set()),
    [saving, setSaving] = useState(false);
  const [groupName, setGroupName] = useState("");
  const inventoryPane = route.page;
  const [tcpPort, setTCPPort] = useState("");
  const [detailPending, setDetailPending] = useState("");
  const [yamlDownloading, setYamlDownloading] = useState(false);
  const [assignmentOpen, setAssignmentOpen] = useState(false);
  useEffect(() => {
    if (
      assignmentOpen &&
      (tab !== "inventory" ||
        inventoryPane !== "discovery" ||
        chosen.size === 0)
    )
      setAssignmentOpen(false);
  }, [assignmentOpen, tab, inventoryPane, chosen.size]);
  const [detail, setDetail] = useState<unknown>(null),
    [portsNode, setPortsNode] = useState<Node | null>(null),
    [restart, setRestart] = useState<NodeStatus | null>(null),
    [operation, setOperation] = useState<Operation | null>(null),
    [resolve, setResolve] = useState<Operation | null>(null);
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
  const reload = useCallback(
    async (force = false) => {
      if (!permissions.includes("monitor.read")) return;
      const key = JSON.stringify([tab, query, page]);
      if (
        !force &&
        reloadInFlight.current?.api === api &&
        reloadInFlight.current.key === key
      )
        return reloadInFlight.current.pending;
      const generation = ++requestGeneration.current;
      const load = async () => {
        setLoading(true);
        const applyBaseline = (b: Baseline) =>
          setBaseline((old) => {
            if (!old || !draft.current) return b;
            if (old.revision !== b.revision)
              showNotice(
                "基准已被其他请求更新。请清空变更并刷新后重新确认。",
                "error",
              );
            return old;
          });
        if (tab === "status") {
          if (
            permissions.includes("baseline.read") &&
            (!baselineInFlight.current || force)
          ) {
            const request = { pending: Promise.resolve() };
            baselineInFlight.current = request;
            request.pending = api
              .get<Baseline>("/api/v1/baseline")
              .then((value) => {
                if (baselineInFlight.current === request) {
                  applyBaseline(value);
                  setBaselineError("");
                }
              })
              .catch((e) => {
                if (baselineInFlight.current === request)
                  setBaselineError(message(e));
              })
              .finally(() => {
                if (baselineInFlight.current === request)
                  baselineInFlight.current = null;
              });
          }
          await api
            .get<{ services: ServiceStatus[] }>("/api/v1/services/status")
            .then((value) => {
              if (generation !== requestGeneration.current) return;
              setStatus(value.services);
              setLoaded((old) => ({ ...old, status: true }));
              setUpdatedAt((old) => ({
                ...old,
                status: new Date().toISOString(),
              }));
              setLoadErrors((old) => ({ ...old, status: "" }));
            })
            .catch((e) => {
              if (generation === requestGeneration.current)
                setLoadErrors((old) => ({ ...old, status: message(e) }));
            });
          if (generation === requestGeneration.current) setLoading(false);
          return;
        }
        try {
          const params = new URLSearchParams({
            ...query,
            selectable_only: "true",
            page: String(page),
            page_size: "50",
          });
          const [b, a, s, c, , n] = await Promise.all([
            ["inventory", "status"].includes(tab) &&
            permissions.includes("baseline.read")
              ? api.get<Baseline>("/api/v1/baseline")
              : Promise.resolve(null),
            tab === "inventory"
              ? api.get<{ agents: Agent[] }>("/api/v1/agents")
              : Promise.resolve(null),
            ["overview", "status", "inventory", "operations"].includes(tab)
              ? api.get<{ services: ServiceStatus[] }>(
                  "/api/v1/services/status",
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
      };
      const pending = load();
      const request = { api, key, pending };
      reloadInFlight.current = request;
      try {
        await pending;
      } finally {
        if (reloadInFlight.current === request) reloadInFlight.current = null;
      }
    },
    [api, query, page, permissions, tab, loadOperations],
  );
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
      baselineInFlight.current = null;
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
      await reload(true);
    } catch (e) {
      showNotice(
        `${message(e)}。变更已保留；若请求超时，请先核对容器服务。`,
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
    setDetailPending(key(c));
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
    } finally {
      setDetailPending((current) => (current === key(c) ? "" : current));
    }
  }
  async function downloadBaseline() {
    if (!baseline?.yaml_url) return;
    setYamlDownloading(true);
    try {
      await api.download(baseline.yaml_url);
    } catch (e) {
      showNotice(message(e), "error");
    } finally {
      setYamlDownloading(false);
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
            aria-label="导航路径"
            items={[
              {
                title: can("monitor.read")
                  ? crumbLink(
                      "工作台",
                      routeHref({ view: "overview", page: "" }),
                    )
                  : "工作台",
              },
              ...(section
                ? [
                    {
                      title:
                        tab === "rtc"
                          ? crumbLink(
                              section.label,
                              routeHref({ view: "rtc", page: "tasks" }),
                            )
                          : section.label,
                    },
                  ]
                : []),
              {
                title: rtcDetail
                  ? crumbLink(current?.label || "执行记录", routeHref(route))
                  : current?.label || "运行总览",
              },
              ...trail.map((item) => ({
                title: item.href
                  ? crumbLink(item.title, item.href)
                  : item.title,
              })),
            ]}
          />
          {!["rtc", "operations"].includes(tab) && (
            <div
              className={`workspace-page-heading${tab === "inventory" ? " inventory-page-heading" : tab === "overview" ? " overview-page-heading" : ""}`}
            >
              <div
                className={
                  tab === "inventory"
                    ? "inventory-page-heading-start"
                    : undefined
                }
              >
                {tab === "overview" && (
                  <span className="overview-page-icon" aria-hidden="true">
                    <DashboardOutlined />
                  </span>
                )}
                {tab === "inventory" && (
                  <span className="inventory-page-icon" aria-hidden>
                    {inventoryPane === "discovery" ? (
                      <SearchOutlined />
                    ) : (
                      <DatabaseOutlined />
                    )}
                  </span>
                )}
                <div>
                  <Typography.Title
                    level={2}
                    id="workspace-page-title"
                    tabIndex={-1}
                  >
                    {current?.label || "运行总览"}
                  </Typography.Title>
                  {tab !== "overview" && current?.description && (
                    <Typography.Paragraph type="secondary">
                      {current.description}
                    </Typography.Paragraph>
                  )}
                </div>
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
                  <Button
                    loading={loading}
                    onClick={() => {
                      if (tab === "overview")
                        setOverviewRefreshVersion((v) => v + 1);
                      void reload();
                    }}
                  >
                    刷新
                  </Button>
                </div>
              )}
            </div>
          )}
          {[
            "overview",
            "status",
            "inventory",
            "notifications",
            "operations",
          ].includes(tab) &&
          !loaded[tab] &&
          loading &&
          !(tab === "overview" && loadErrors[tab]) ? (
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
              ].includes(tab) &&
              !loaded[tab] &&
              !(tab === "overview" && loadErrors[tab])
            }
          >
            {tab === "overview" && can("monitor.read") && (
              <Overview
                api={api}
                services={observedStatus}
                notifications={notifications}
                onNavigate={navigate}
                canManageInventory={can("inventory.read")}
                canConfigureNotifications={can("notifications.write")}
                refreshVersion={overviewRefreshVersion}
                snapshotUnavailable={!!loadErrors.overview}
                hasSnapshot={!!loaded.overview}
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
            {tab === "logs" && can("monitor.read") && (
              <LogCollection api={api} />
            )}
            {tab === "status" && (
              <>
                {baselineError && (
                  <Alert
                    className="page-alert"
                    type="warning"
                    showIcon
                    message="监控配置暂时不可用，容器状态仍正常更新"
                    description={baselineError}
                    action={
                      <Button onClick={() => void reload()}>重试配置</Button>
                    }
                  />
                )}
                <ContainerServices
                  filters={containerFilters}
                  api={api}
                  services={observedStatus}
                  baseline={baseline}
                  members={members}
                  removals={removals}
                  canDiscover={can("inventory.read")}
                  canConfigure={can("baseline.write")}
                  canRestart={can("restart.execute")}
                  settingsBlocked={
                    saving || draft.current || !!baselineError || !baseline
                  }
                  actionOpen={!!portsNode || !!restart}
                  yamlDownloading={yamlDownloading}
                  downloadBaseline={downloadBaseline}
                  onDiscover={() =>
                    navigate({ view: "inventory", page: "discovery" })
                  }
                  onSettings={setPortsNode}
                  onRestart={setRestart}
                />
              </>
            )}
            {tab === "rtc" && can("monitor.read") && (
              <RTCProbes
                api={api}
                canManage={can("rtc.manage")}
                canDiagnose={can("rtc.diagnostics")}
                canManageHosts={can("probes.manage")}
                onBreadcrumbs={setRTCTrail}
                pane={route.page as RTCPane}
              />
            )}
            {tab === "inventory" && can("inventory.read") && (
              <InventoryDiscovery
                setPage={setPage}
                setQuery={setQuery}
                filters={filters}
                setFilters={setFilters}
                agents={agents}
                inventoryPane={inventoryPane}
                chosen={chosen}
                saving={saving}
                setChosen={setChosen}
                can={can}
                setAssignmentOpen={setAssignmentOpen}
                additions={additions}
                removals={removals}
                containers={containers}
                eligible={eligible}
                select={select}
                detailPending={detailPending}
                showDetail={showDetail}
                page={page}
                total={total}
                notice={notice}
                noticeType={noticeType}
                assignmentOpen={assignmentOpen}
              />
            )}
            <div className="inventory-suite">
              {["status", "inventory"].includes(tab) &&
                additions.size + removals.size > 0 && (
                  <Card
                    className="inventory-workbench inventory-draft-review"
                    title={
                      <div className="inventory-workbench-title">
                        <CheckCircleOutlined aria-hidden />
                        <h3 id="inventory-draft-title" tabIndex={-1}>
                          待保存变更
                        </h3>
                      </div>
                    }
                    extra={
                      <Tag>
                        新增 {additions.size} · 移出 {removals.size}
                      </Tag>
                    }
                  >
                    <div className="inventory-draft-explanation">
                      加入变更后仍是草稿，保存后才会生效。
                    </div>
                    <div className="inventory-pending-list">
                      {!additions.size && !removals.size && (
                        <AntEmpty
                          image={AntEmpty.PRESENTED_IMAGE_SIMPLE}
                          description="添加容器后，在这里确认变更"
                        />
                      )}
                      {[...additions].map(([k, a]) => (
                        <div
                          className="inventory-pending-item inventory-pending-add"
                          key={k}
                        >
                          <span className="inventory-change-kind">
                            <PlusOutlined aria-hidden />
                            新增
                          </span>
                          <div className="inventory-pending-identity">
                            <Typography.Text
                              strong
                              className="inventory-pending-name"
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
                              <CloseOutlined aria-hidden />
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
                          <div
                            className="inventory-pending-item inventory-pending-remove"
                            key={id}
                          >
                            <span className="inventory-change-kind">
                              <DeleteOutlined aria-hidden />
                              移出
                            </span>
                            <div className="inventory-pending-identity">
                              <Typography.Text
                                className="inventory-pending-name"
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
                                <CloseOutlined aria-hidden />
                              </Button>
                            </div>
                            <Typography.Text type="secondary">
                              {n?.host_address}
                            </Typography.Text>
                          </div>
                        );
                      })}
                    </div>

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
                        icon={<CheckCircleOutlined aria-hidden />}
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
                )}
            </div>
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
            {tab === "accounts" && can("users.manage") && (
              <Accounts api={api} canUsers={can("users.manage")} />
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
      {tab === "inventory" && (
        <Drawer
          rootClassName="inventory-assignment-drawer"
          title={
            <div className="inventory-drawer-title">
              <SettingOutlined aria-hidden />
              <span>设置纳管规则</span>
            </div>
          }
          placement="right"
          width={440}
          open={assignmentOpen && inventoryPane === "discovery"}
          destroyOnHidden
          onClose={() => setAssignmentOpen(false)}
          afterOpenChange={(open) => {
            if (open) document.getElementById("app-field-5")?.focus();
            else if (
              tab === "inventory" &&
              inventoryPane === "discovery" &&
              chosen.size === 0 &&
              additions.size > 0
            ) {
              const heading = document.getElementById("inventory-draft-title");
              heading?.focus({ preventScroll: true });
              heading?.scrollIntoView({
                block: "nearest",
                behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
                  .matches
                  ? "auto"
                  : "smooth",
              });
            }
          }}
          footer={
            <div className="inventory-assignment-footer">
              <Button onClick={() => setAssignmentOpen(false)}>取消</Button>
              <Button
                type="primary"
                icon={<PlusOutlined aria-hidden />}
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
            </div>
          }
        >
          <InventoryNote>
            为已选 {chosen.size}{" "}
            个容器设置分组和检查规则。加入后仍是草稿，统一保存才会生效。
          </InventoryNote>
          <details className="inventory-selected-preview">
            <summary>查看已选容器（{chosen.size}）</summary>
            <ul>
              {[...chosen.values()].map((container) => (
                <li key={key(container)}>
                  <strong>{container.container_name}</strong>
                  <code>{container.host_address}</code>
                </li>
              ))}
            </ul>
          </details>
          <Form
            layout="vertical"
            component="div"
            className="inventory-assignment"
          >
            <Form.Item label="分组名" required htmlFor="app-field-5">
              <AutoComplete
                id="app-field-5"
                autoFocus
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
          </Form>
          {notice && noticeType === "error" && (
            <Alert type="error" showIcon message={notice} />
          )}
        </Drawer>
      )}
      {portsNode && baseline && (
        <NodeSettingsModal
          api={api}
          node={portsNode}
          onClose={() => setPortsNode(null)}
          onSaved={(b) => {
            setBaseline(b);
            setPortsNode(null);
            setNotice("端口已保存并生效");
            void reload(true);
          }}
          onRemove={() => {
            remove(portsNode.id, portsNode.agent_id, portsNode.container_name);
            setPortsNode(null);
          }}
        />
      )}
      {detail !== null && (
        <Modal
          title={
            <h2 className="inventory-dialog-title">
              <CodeOutlined aria-hidden />
              容器启动配置
            </h2>
          }
          className="inventory-dialog inventory-detail-dialog"
          width={960}
          onClose={() => setDetail(null)}
        >
          <InventoryContainerDetails value={detail} />
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
            void reload(true);
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
            void reload(true);
          }}
        />
      )}
    </>
  );
}
