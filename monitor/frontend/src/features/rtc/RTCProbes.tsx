import { DownOutlined, MoreOutlined, PlusOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Dropdown from "antd/es/dropdown";
import Empty from "antd/es/empty";
import Input from "antd/es/input";
import Modal from "antd/es/modal";
import Pagination from "antd/es/pagination";
import Select from "antd/es/select";
import Skeleton from "antd/es/skeleton";
import Space from "antd/es/space";
import Tabs from "antd/es/tabs";
import Tag from "antd/es/tag";
import { useCallback, useEffect, useRef, useState } from "react";
import type { RTCPane } from "../../app/navigation";
import { rtcHref, rtcRunHref } from "../../app/routes";
import { APIError, type API } from "../../shared/api";
import ProbeHosts from "./ProbeHosts";
import { RTCRetention } from "./RTCAdministration";
import RTCBreadcrumbs, { type RTCBreadcrumbTrail } from "./RTCBreadcrumbs";
import RTCEditor from "./RTCEditor";
import RTCLink, { visitRTC } from "./RTCLink";
import {
  clientLabel,
  errorText,
  isInteropClient,
  isParentRun,
  State,
  taskLabel,
  taskRuntime,
  terminal,
  time,
  type Edit,
  type Run,
  type Snapshot,
  type Target,
  type Task,
} from "./RTCModel";
import NativeCoverageDetails, { phaseLabels } from "./RTCNativeCoverage";
import RTCRunDetails from "./RTCRunDetails";
import RTCTargetDetails from "./RTCTargetDetails";
import {
  BusinessState,
  DetailBack,
  healthReason,
  PageHeading,
  rtcNavigate,
  useRTCDetail,
} from "./RTCUI";
export default function RTCProbes({
  api,
  canManage,
  canDiagnose,
  canManageHosts,
  pane: requestedPane,
  onBreadcrumbs,
}: {
  api: API;
  canManage: boolean;
  canDiagnose: boolean;
  canManageHosts: boolean;
  pane: RTCPane;
  onBreadcrumbs?: (trail: RTCBreadcrumbTrail) => void;
}) {
  const [data, setData] = useState<Snapshot | null>(null),
    [error, setError] = useState(""),
    [disabled, setDisabled] = useState(false),
    [busy, setBusy] = useState(""),
    [edit, setEdit] = useState<Edit | null>(null),
    [query, setQuery] = useState(""),
    [filter, setFilter] = useState("all"),
    [rules, setRules] = useState<Record<string, boolean>>({});
  const [history, setHistory] = useState<Run[]>([]),
    [historyTotal, setHistoryTotal] = useState(0),
    [historyPage, setHistoryPage] = useState(1),
    [historyType, setHistoryType] = useState(""),
    [historyResult, setHistoryResult] = useState(""),
    [historyTask, setHistoryTask] = useState(""),
    [historyError, setHistoryError] = useState(""),
    [historyLoading, setHistoryLoading] = useState(false),
    [retry, setRetry] = useState(0),
    [detail, setDetail] = useState<Run | null>(null),
    [detailError, setDetailError] = useState("");
  const params = useRTCDetail(),
    page = params.get("page"),
    pane = (
      ["tasks", "targets", "probes", "history", "settings"].includes(page || "")
        ? page
        : requestedPane
    ) as RTCPane,
    taskID = pane === "tasks" ? params.get("rtc_task") : null,
    runID = params.get("rtc_run"),
    targetID = pane === "targets" ? params.get("rtc_target") : null,
    phase = params.get("rtc_phase") || "AP",
    runTab = params.get("rtc_tab") || "result";
  const active = useRef(true),
    fetching = useRef<Promise<void> | null>(null),
    writeLock = useRef(false);
  const refresh = useCallback(
    async (force = false) => {
      if (fetching.current) {
        await fetching.current;
        if (!force) return;
      }
      const load = async () => {
        try {
          const v = await api.get<Snapshot>("/api/v1/rtc/status");
          if (active.current) {
            setData({
              ...v,
              tasks: v.tasks || [],
              targets: v.targets || [],
              runtimes: v.runtimes,
              hosts: v.hosts,
              runs: v.runs || [],
            });
            setError("");
            setDisabled(false);
          }
        } catch (e) {
          if (active.current) {
            setDisabled(
              e instanceof APIError && e.code === "RTC_PROBE_DISABLED",
            );
            setError(errorText(e));
          }
        }
      };
      const pending = load();
      fetching.current = pending;
      try {
        await pending;
      } finally {
        if (fetching.current === pending) fetching.current = null;
      }
    },
    [api],
  );
  useEffect(() => {
    active.current = true;
    void refresh();
    const timer = setInterval(() => {
      if (!document.hidden) void refresh();
    }, 5000);
    return () => {
      active.current = false;
      clearInterval(timer);
    };
  }, [refresh]);
  const operate = async (key: string, path: string, body: unknown = {}) => {
    if (writeLock.current) return;
    writeLock.current = true;
    setBusy(key);
    setError("");
    try {
      await api.post(path, body);
      await refresh(true);
    } catch (e) {
      setError(errorText(e));
      throw e;
    } finally {
      writeLock.current = false;
      setBusy("");
    }
  };
  const trigger = (key: string, path: string, body: unknown = {}) => {
    void operate(key, path, body).catch(() => {});
  };
  useEffect(() => {
    let live = true;
    if (!runID) {
      setDetail(null);
      setDetailError("");
      return;
    }
    void api
      .get<Run>(`/api/v1/rtc/runs/${encodeURIComponent(runID)}`)
      .then((v) => {
        if (live) {
          setDetail(v);
          setDetailError("");
        }
      })
      .catch((e) => {
        if (live) setDetailError(errorText(e));
      });
    return () => {
      live = false;
    };
  }, [api, runID, data, retry]);
  useEffect(() => {
    if (pane !== "history" || runID) return;
    let live = true;
    setHistoryLoading(true);
    const search = new URLSearchParams({
      limit: "10",
      offset: String((historyPage - 1) * 10),
    });
    if (historyType) search.set("client_type", historyType);
    if (historyResult) search.set("status", historyResult);
    if (historyTask) search.set("task_id", historyTask);
    void api
      .get<{ runs: Run[]; total: number }>(`/api/v1/rtc/history?${search}`)
      .then((v) => {
        if (live) {
          setHistory(v.runs || []);
          setHistoryTotal(v.total);
          setHistoryError("");
        }
      })
      .catch((e) => {
        if (live) setHistoryError(errorText(e));
      })
      .finally(() => {
        if (live) setHistoryLoading(false);
      });
    return () => {
      live = false;
    };
  }, [
    api,
    pane,
    runID,
    historyPage,
    historyType,
    historyResult,
    historyTask,
    retry,
    data?.runs,
  ]);
  const openTask = (id: string, p = "AP") =>
    rtcNavigate("tasks", { task: id, phase: p });
  const openRun = (id: string, parentID?: string) =>
    visitRTC(rtcRunHref(id, parentID));
  const back = () => {
    const parent = params.get("rtc_parent");
    const values: Record<string, string> =
      pane === "tasks" && taskID ? { task: taskID, phase } : {};
    if (pane === "tasks" && taskID) openTask(taskID, phase);
    else if (parent) rtcNavigate(pane, { ...values, run: parent });
    else rtcNavigate(pane);
  };
  if (disabled)
    return (
      <section className="rtc-screen rtc-suite">
        <PageHeading title="服务自主探测器" />
        <div className="rtc-panel rtc-pad">
          <Empty description="尚未配置 RTC 探测服务" />
        </div>
      </section>
    );
  if (!data)
    return (
      <section className="rtc-screen rtc-suite">
        <PageHeading title="服务自主探测器" />
        <div className="rtc-panel rtc-pad">
          {error ? (
            <Alert
              type="error"
              message={error}
              action={<Button onClick={() => void refresh()}>重试</Button>}
            />
          ) : (
            <Skeleton active />
          )}
        </div>
      </section>
    );
  const targetName = (id: string) =>
    data.targets.find((t) => t.target_id === id)?.name || id;
  const targetIP = (target?: Target) => {
    const rtc = target?.rtc;
    return (
      rtc?.native_coverage?.groups.flatMap((g) => g.entrypoints).join(", ") ||
      rtc?.web_access?.coverage?.groups
        .flatMap((g) => g.entrypoints)
        .join(", ") ||
      rtc?.access_ips?.join(", ") ||
      rtc?.media_edges?.host ||
      "—"
    );
  };
  const currentTask = data.tasks.find((t) => t.task_id === taskID),
    currentTarget = data.targets.find((t) => t.target_id === targetID),
    currentRun =
      data.runs.find((r) => r.run_id === runID) ||
      (detail?.run_id === runID ? detail : null);
  const runContextMismatch =
    currentRun &&
    ((params.get("rtc_case") === "1" && isParentRun(currentRun)) ||
      (params.has("rtc_parent") &&
        params.get("rtc_parent") !== currentRun.parent_run_id) ||
      (pane === "tasks" && taskID && currentRun.task_id !== taskID) ||
      (pane === "tasks" &&
        currentRun.native_plan?.phase &&
        currentRun.native_plan.phase !== phase));
  const runTask = (task: Task) => {
    const existing = data.runs.find(
      (r) =>
        r.task_id === task.task_id && !r.parent_run_id && !terminal(r.status),
    );
    if (existing) {
      openRun(existing.run_id);
      return;
    }
    trigger(
      task.task_id,
      `/api/v1/rtc/tasks/${encodeURIComponent(task.task_id)}/run`,
    );
  };
  const toggleTask = (task: Task) => {
    const save = () =>
      operate(
        task.task_id,
        `/api/v1/rtc/tasks/${encodeURIComponent(task.task_id)}/enabled`,
        { enabled: !task.enabled },
      );
    if (task.enabled)
      Modal.confirm({
        className: "rtc-confirm",
        title: "停用任务？",
        content: `「${taskLabel(task)}」将停止调度并取消未完成的执行。`,
        okText: "停用任务",
        cancelText: "取消",
        onOk: save,
      });
    else void save().catch(() => {});
  };
  const taskMenu = (task: Task) =>
    canManage ? (
      <Dropdown
        overlayClassName="rtc-menu"
        trigger={["click"]}
        menu={{
          items: [
            {
              key: "run",
              label: "运行一次",
              disabled: !task.enabled || !!busy,
            },
            { key: "edit", label: "编辑任务", disabled: !!busy },
            {
              key: "enable",
              label: task.enabled ? "停用任务" : "启用任务",
              danger: task.enabled,
              disabled: !!busy,
            },
          ],
          onClick: ({ key }) =>
            key === "run"
              ? runTask(task)
              : key === "edit"
                ? setEdit({ kind: "task", value: task })
                : toggleTask(task),
        }}
      >
        <Button
          aria-label={`更多操作 ${taskLabel(task)}`}
          icon={<MoreOutlined />}
        />
      </Dropdown>
    ) : null;
  const taskRules = (task: Task) => {
    const probe = taskRuntime(task, data);
    return (
      <div
        className="rtc-panel rtc-pad"
        id="rtc-task-rules"
        hidden={!rules[task.task_id]}
      >
        <h3>执行规则</h3>
        <dl className="rtc-rule-grid">
          {[
            [
              "执行服务器",
              `${probe?.host_name || task.host_id} · ${probe?.host_address || "—"}`,
            ],
            ["AP 接入间隔", `完成后 ${task.interval_seconds / 60} 分钟`],
            [
              "Edge 直连间隔",
              `完成后 ${(task.sweep_interval_seconds || 900) / 60} 分钟`,
            ],
            [
              "跨 Edge 互通间隔",
              `完成后 ${(task.cross_interval_seconds || 1800) / 60} 分钟`,
            ],
            ["单用例超时", `${task.timeout_seconds} 秒`],
            [
              "失败确认 / 恢复",
              `${task.failure_threshold} 次失败 / ${task.recovery_threshold} 次成功`,
            ],
            ["持续待验证通知", `${task.unknown_notify_seconds / 60} 分钟`],
            ["描述", task.description || "—"],
          ].map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      </div>
    );
  };
  let content;
  if (runID)
    content = runContextMismatch ? (
      <>
        <PageHeading
          title="执行路径不匹配"
          actions={<DetailBack onClick={back} />}
        />
        <Alert
          type="error"
          showIcon
          message="该路径不属于当前执行或覆盖类别，请返回上一级重新查看。"
        />
      </>
    ) : currentRun ? (
      <RTCRunDetails
        key={runID}
        run={currentRun}
        data={data}
        api={api}
        onBack={back}
        operate={operate}
        busy={busy}
        canManage={canManage}
        canDiagnose={canDiagnose}
        tab={runTab}
        onTab={(tab) =>
          rtcNavigate(pane, {
            run: runID,
            case:
              pane === "tasks" &&
              (params.get("rtc_case") === "1" || currentRun.parent_run_id)
                ? "1"
                : "",
            parent: params.get("rtc_parent") || "",
            paths: params.get("rtc_paths") || "",
            task: pane === "tasks" ? taskID || "" : "",
            phase,
            tab,
          })
        }
      />
    ) : (
      <>
        <PageHeading title="执行详情" actions={<DetailBack onClick={back} />} />
        {detailError ? (
          <Alert
            type="error"
            message={detailError}
            action={
              <Button onClick={() => setRetry((n) => n + 1)}>重试</Button>
            }
          />
        ) : (
          <Skeleton active />
        )}
      </>
    );
  else if (taskID)
    content = currentTask ? (
      <>
        <PageHeading
          title={taskLabel(currentTask)}
          subtitle={`${clientLabel(currentTask)} · ${targetName(currentTask.target_id)}`}
          status={<BusinessState task={currentTask} data={data} />}
          actions={
            <>
              <Button
                aria-expanded={!!rules[taskID]}
                aria-controls="rtc-task-rules"
                onClick={() =>
                  setRules((v) => ({ ...v, [taskID]: !v[taskID] }))
                }
              >
                执行规则{" "}
                <DownOutlined
                  aria-hidden="true"
                  rotate={rules[taskID] ? 180 : 0}
                />
              </Button>
              {canManage && (
                <Button
                  type="primary"
                  loading={busy === taskID}
                  disabled={!currentTask.enabled || !!busy}
                  onClick={() => runTask(currentTask)}
                >
                  运行一次
                </Button>
              )}
            </>
          }
        />
        {taskRules(currentTask)}
        {healthReason(currentTask, data) && (
          <Alert
            className="page-alert"
            showIcon
            type={
              data.task_health?.[taskID]?.state === "UNHEALTHY"
                ? "error"
                : "warning"
            }
            message={healthReason(currentTask, data)}
          />
        )}
        <h3 className="rtc-section-title">当前覆盖</h3>
        {data.native_coverage?.[taskID] ? (
          <NativeCoverageDetails
            key={taskID}
            api={api}
            state={data.native_coverage[taskID]}
            runs={data.runs}
            phase={phase}
            onPhase={(p) => openTask(taskID, p)}
          />
        ) : (
          <div className="rtc-panel rtc-pad">
            {data.runs
              .filter((r) => r.task_id === taskID && !r.parent_run_id)
              .slice(0, 1)
              .map((r) => (
                <RTCLink key={r.run_id} href={rtcRunHref(r.run_id)}>
                  查看最近执行
                </RTCLink>
              ))}
            {!data.runs.some((r) => r.task_id === taskID) && (
              <Empty description="等待首次执行" />
            )}
          </div>
        )}
      </>
    ) : (
      <Empty description="任务不存在" />
    );
  else if (targetID)
    content = currentTarget ? (
      <RTCTargetDetails
        target={currentTarget}
        tasks={data.tasks}
        canManage={canManage}
        onEdit={() => setEdit({ kind: "target", value: currentTarget })}
        onOpenTask={openTask}
        onBack={() => rtcNavigate("targets")}
      />
    ) : (
      <Empty description="目标服务不存在" />
    );
  else if (pane === "tasks") {
    const rows = data.tasks.filter((t) => {
      const running = data.runs.some(
        (r) => r.task_id === t.task_id && !terminal(r.status),
      );
      return (
        (filter === "all" ||
          (filter === "bad" &&
            t.enabled &&
            data.task_health?.[t.task_id]?.state === "UNHEALTHY") ||
          (filter === "running" && running) ||
          (filter === "paused" && !t.enabled)) &&
        `${taskLabel(t)} ${targetName(t.target_id)} ${t.host_id} ${taskRuntime(t, data)?.host_address || ""} ${clientLabel(t)}`
          .toLowerCase()
          .includes(query.toLowerCase())
      );
    });
    content = (
      <>
        <PageHeading
          title="探测任务"
          actions={
            canManage && (
              <Button
                type="primary"
                icon={<PlusOutlined aria-hidden="true" />}
                onClick={() => setEdit({ kind: "task" })}
              >
                创建任务
              </Button>
            )
          }
        />
        <div className="rtc-panel">
          <div className="rtc-toolbar">
            <div className="rtc-chips" role="group" aria-label="任务筛选">
              {[
                ["all", "全部"],
                ["bad", "异常"],
                ["running", "执行中"],
                ["paused", "已停用"],
              ].map(([key, label]) => (
                <Button
                  type={filter === key ? "default" : "text"}
                  className={filter === key ? "selected" : ""}
                  key={key}
                  aria-pressed={filter === key}
                  onClick={() => setFilter(key)}
                >
                  {label}
                  <span className="rtc-filter-count">
                    {key === "all"
                      ? data.tasks.length
                      : key === "paused"
                        ? data.tasks.filter((t) => !t.enabled).length
                        : key === "bad"
                          ? data.tasks.filter(
                              (t) =>
                                t.enabled &&
                                data.task_health?.[t.task_id]?.state ===
                                  "UNHEALTHY",
                            ).length
                          : data.tasks.filter((t) =>
                              data.runs.some(
                                (r) =>
                                  r.task_id === t.task_id &&
                                  !r.parent_run_id &&
                                  !terminal(r.status),
                              ),
                            ).length}
                  </span>
                </Button>
              ))}
            </div>
            <Input.Search
              aria-label="搜索任务或服务"
              placeholder="搜索任务、服务或服务器"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              allowClear
            />
          </div>
          {rows.length ? (
            <table className="rtc-table" aria-label="探测任务清单">
              <thead>
                <tr>
                  <th scope="col">任务 / 目标服务</th>
                  <th scope="col">客户端</th>
                  <th scope="col">业务状态</th>
                  <th scope="col">最近有效验证</th>
                  <th scope="col">操作</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((t) => (
                  <tr key={t.task_id}>
                    <td data-label="任务">
                      <Button
                        type="link"
                        className="rtc-name-link"
                        onClick={() => openTask(t.task_id)}
                      >
                        {taskLabel(t)}
                      </Button>
                      <div className="rtc-secondary">
                        {targetName(t.target_id)}
                      </div>
                    </td>
                    <td data-label="客户端">
                      <Tag>{clientLabel(t)}</Tag>
                    </td>
                    <td data-label="业务状态">
                      <BusinessState task={t} data={data} />
                      {healthReason(t, data) && (
                        <div className="rtc-secondary">
                          {healthReason(t, data)}
                        </div>
                      )}
                    </td>
                    <td data-label="最近有效验证">
                      {time(data.task_health?.[t.task_id]?.last_success_at)}
                    </td>
                    <td data-label="操作">
                      <div className="rtc-actions">
                        <Button type="link" onClick={() => openTask(t.task_id)}>
                          详情
                        </Button>
                        {taskMenu(t)}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Empty
              description={
                data.tasks.length ? "没有匹配的任务" : "创建第一条 RTC 探测任务"
              }
            >
              {data.tasks.length ? (
                <Button
                  onClick={() => {
                    setQuery("");
                    setFilter("all");
                  }}
                >
                  清除筛选
                </Button>
              ) : (
                canManage && (
                  <Space>
                    <Button onClick={() => rtcNavigate("targets")}>
                      配置目标服务
                    </Button>
                    <Button onClick={() => rtcNavigate("probes")}>
                      查看探测服务器
                    </Button>
                  </Space>
                )
              )}
            </Empty>
          )}
          <div className="rtc-table-footer">{rows.length} 个任务</div>
        </div>
      </>
    );
  } else if (pane === "targets")
    content = (
      <>
        <PageHeading
          title="目标服务"
          actions={
            canManage && (
              <Button
                type="primary"
                icon={<PlusOutlined aria-hidden="true" />}
                onClick={() => setEdit({ kind: "target" })}
              >
                创建目标服务
              </Button>
            )
          }
        />
        <div className="rtc-panel">
          {data.targets.length ? (
            <table className="rtc-table" aria-label="目标服务清单">
              <thead>
                <tr>
                  <th scope="col">服务名称 / 接入 IP</th>
                  <th scope="col">鉴权</th>
                  <th scope="col">客户端覆盖</th>
                  <th scope="col">关联任务</th>
                  <th scope="col">操作</th>
                </tr>
              </thead>
              <tbody>
                {data.targets.map((t) => (
                  <tr key={t.target_id}>
                    <td data-label="服务">
                      <strong>{t.name}</strong>
                      <div className="rtc-secondary">{targetIP(t)}</div>
                    </td>
                    <td data-label="鉴权">
                      {t.rtc?.auth_mode === "none"
                        ? "未开启鉴权"
                        : t.has_certificate
                          ? "Certificate 已配置"
                          : "接入未配置"}
                    </td>
                    <td data-label="覆盖">
                      <Tag>Native</Tag>
                      {t.rtc?.web_access && <Tag color="blue">Web</Tag>}
                    </td>
                    <td data-label="任务">
                      {
                        data.tasks.filter((v) => v.target_id === t.target_id)
                          .length
                      }{" "}
                      个任务
                    </td>
                    <td data-label="操作">
                      <Button
                        type="link"
                        onClick={() =>
                          rtcNavigate("targets", { target: t.target_id })
                        }
                      >
                        查看配置
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Empty description="暂无目标服务" />
          )}
        </div>
      </>
    );
  else if (pane === "history")
    content = (
      <>
        <PageHeading title="执行记录" />
        <div className="rtc-panel">
          <div className="rtc-toolbar">
            <Space wrap>
              <Select
                aria-label="客户端筛选"
                value={historyType}
                onChange={(v) => {
                  setHistoryType(v);
                  setHistoryPage(1);
                }}
                options={[
                  { value: "", label: "全部客户端" },
                  { value: "native", label: "Native" },
                  { value: "web", label: "Web" },
                  { value: "interop", label: "Native ↔ Web" },
                ]}
              />
              <Select
                aria-label="结果筛选"
                value={historyResult}
                onChange={(v) => {
                  setHistoryResult(v);
                  setHistoryPage(1);
                }}
                options={[
                  { value: "", label: "全部结果" },
                  { value: "SUCCESS", label: "通过" },
                  { value: "FAILED", label: "业务失败" },
                  { value: "ERROR", label: "执行错误" },
                  { value: "CANCELLED", label: "已取消" },
                ]}
              />
              <Select
                aria-label="任务筛选"
                value={historyTask}
                onChange={(v) => {
                  setHistoryTask(v);
                  setHistoryPage(1);
                }}
                options={[
                  { value: "", label: "全部任务" },
                  ...data.tasks.map((t) => ({
                    value: t.task_id,
                    label: taskLabel(t),
                  })),
                ]}
              />
            </Space>
          </div>
          {historyError ? (
            <Alert
              type="error"
              message={historyError}
              action={
                <Button onClick={() => setRetry((n) => n + 1)}>重试</Button>
              }
            />
          ) : historyLoading && !history.length ? (
            <Skeleton active />
          ) : !history.length ? (
            <Empty description="没有匹配的执行记录" />
          ) : (
            <table className="rtc-table" aria-label="执行记录清单">
              <thead>
                <tr>
                  <th scope="col">任务 / 客户端</th>
                  <th scope="col">验证类别</th>
                  <th scope="col">开始时间</th>
                  <th scope="col">本次结果</th>
                  <th scope="col">完成项 / 耗时</th>
                  <th scope="col">操作</th>
                </tr>
              </thead>
              <tbody>
                {history.map((r) => (
                  <tr key={r.run_id}>
                    <td data-label="任务">
                      {data.tasks.find((t) => t.task_id === r.task_id)
                        ? taskLabel(
                            data.tasks.find((t) => t.task_id === r.task_id)!,
                          )
                        : isInteropClient(r.client_type)
                          ? "Native ↔ Web 互通"
                          : r.task_id}
                      <div className="rtc-secondary">
                        {clientLabel({
                          client_type:
                            r.client_type ||
                            (r.driver === "agora-web" ? "web" : "native"),
                        })}{" "}
                        · {targetName(r.target_id)}
                      </div>
                    </td>
                    <td data-label="验证类别">
                      {phaseLabels[r.native_plan?.phase || ""] || "音视频验证"}
                    </td>
                    <td data-label="开始时间">{time(r.created_at)}</td>
                    <td data-label="本次结果">
                      <State value={r.status} />
                    </td>
                    <td data-label="完成项 / 耗时">
                      {
                        (r.cases || r.edges || []).filter((v) =>
                          terminal(v.status),
                        ).length
                      }{" "}
                      / {(r.cases || r.edges || []).length}
                      <div className="rtc-secondary">
                        {r.finished_at
                          ? `${Math.max(0, (Date.parse(r.finished_at) - Date.parse(r.created_at)) / 1000).toFixed(1)} 秒`
                          : "执行中"}
                      </div>
                    </td>
                    <td data-label="操作">
                      <RTCLink href={rtcRunHref(r.run_id)}>查看执行</RTCLink>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <div className="rtc-table-footer">
            <span>共 {historyTotal} 条记录</span>
            <Pagination
              size="small"
              current={historyPage}
              total={historyTotal}
              pageSize={10}
              showSizeChanger={false}
              onChange={setHistoryPage}
            />
          </div>
        </div>
      </>
    );
  else
    content = (
      <>
        <PageHeading title="运行设置" />
        <Tabs
          activeKey={params.get("rtc_setting") || "servers"}
          onChange={(setting) => visitRTC(rtcHref("settings", { setting }))}
          items={[
            {
              key: "servers",
              label: "探测服务器",
              children: (
                <ProbeHosts
                  hosts={data.hosts}
                  canManage={canManageHosts}
                  busy={busy}
                  operate={operate}
                />
              ),
            },
            {
              key: "retention",
              label: "数据保留",
              children: data.settings && (
                <RTCRetention
                  api={api}
                  settings={data.settings}
                  canManage={canManage}
                  onSaved={refresh}
                  dailyRuns={data.tasks
                    .filter((t) => t.enabled)
                    .reduce((n, t) => n + 86400 / t.interval_seconds, 0)}
                />
              ),
            },
          ]}
        />
      </>
    );
  return (
    <section className="rtc-screen rtc-suite">
      <RTCBreadcrumbs
        api={api}
        data={data}
        run={runContextMismatch ? null : currentRun}
        params={params}
        onChange={onBreadcrumbs}
      />
      {error && (
        <Alert className="page-alert" type="error" showIcon message={error} />
      )}
      {data.storage_available === false && (
        <Alert
          className="page-alert"
          type="error"
          showIcon
          message="探测数据存储不可用，执行结果暂无法可靠保存"
        />
      )}
      {content}
      {edit && (
        <RTCEditor
          api={api}
          edit={edit}
          snapshot={data}
          onClose={() => setEdit(null)}
          onSave={async (value, revision) => {
            const targets = [...data.targets],
              tasks = [...data.tasks];
            if (edit.kind === "target") {
              const v = {
                ...value,
                target_id: edit.value?.target_id || "",
                driver: "agora",
              } as Target;
              const i = targets.findIndex((t) => t.target_id === v.target_id);
              if (i < 0) targets.push(v);
              else targets[i] = v;
            } else {
              const v = value as Task,
                i = tasks.findIndex((t) => t.task_id === v.task_id);
              if (i < 0) tasks.push(v);
              else tasks[i] = v;
            }
            await api.post("/api/v1/rtc/config", {
              expected_revision: revision,
              targets: targets.map((t) => ({
                target_id: t.target_id,
                name: t.name,
                cluster_code: t.cluster_code,
                driver: t.driver,
                rtc: t.rtc,
                credentials: t.credentials,
              })),
              tasks: tasks.map((t) => ({
                task_id: t.task_id,
                name: t.name,
                client_type: t.client_type,
                target_id: t.target_id,
                host_id: t.host_id,
                service_type: t.service_type,
                interval_seconds: t.interval_seconds,
                sweep_interval_seconds: t.sweep_interval_seconds,
                cross_interval_seconds: t.cross_interval_seconds,
                timeout_seconds: t.timeout_seconds,
                failure_threshold: t.failure_threshold,
                recovery_threshold: t.recovery_threshold,
                unknown_notify_seconds: t.unknown_notify_seconds,
                description: t.description,
              })),
            });
            await refresh(true);
            setEdit(null);
          }}
        />
      )}
    </section>
  );
}
