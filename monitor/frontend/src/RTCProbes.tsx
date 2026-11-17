import { useCallback, useEffect, useRef, useState } from "react";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Descriptions from "antd/es/descriptions";
import Drawer from "antd/es/drawer";
import Empty from "antd/es/empty";
import Form from "antd/es/form";
import Input from "antd/es/input";
import InputNumber from "antd/es/input-number";
import Modal from "antd/es/modal";
import Select from "antd/es/select";
import Space from "antd/es/space";
import Switch from "antd/es/switch";
import Table from "antd/es/table";
import Tabs from "antd/es/tabs";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { APIError, type API } from "./api";
import {
  ProbeRegistration,
  RTCAuditHistory,
  RTCRetention,
  type RTCStorageSettings,
} from "./RTCAdministration";
import RTCSettings, { rtcDefaults, type RTCParameters } from "./RTCSettings";

interface Target {
 rtc?: RTCParameters;
 credentials?: {app_certificate?:string;sender_token?:string;receiver_token?:string};
 has_certificate?:boolean;
 has_sender_token?:boolean;
 has_receiver_token?:boolean;
  target_id: string;
  name: string;
  cluster_code?: string;
  driver: string;
}
interface Task {
  task_id: string;
  target_id: string;
  sender_probe_id: string;
  receiver_probe_id: string;
  enabled: boolean;
  failure_threshold: number;
  recovery_threshold: number;
  unknown_notify_seconds: number;
  description?: string;
  interval_seconds: number;
  timeout_seconds: number;
  mock_scenario?: string;

}
interface Probe {
  capabilities?: string[];
  resource_error?: string;
  storage_bytes?: number;
  name?: string;
  enabled?: boolean;
  probe_id: string;
  driver?: string;
  location: string;
  host_address?: string;
  online: boolean;
  last_heartbeat?: string;
  platform?: string;
  sdk_version?: string;
  probe_version?: string;
  capacity?: number;
}
interface Event {
  event_id: string;
  source_seq: number;
  occurred_at: string;
  monotonic_elapsed_ms: number;
  kind: string;
  stage: string;
  name?: string;
  return_code?: number;
  return_kind?: string;
  duration_ms?: number;
  fields?: Record<string, unknown>;
  call_id?: string;
  related_call_id?: string;
  connection_id?: string;
  service_id?: string;
  duration_us?: number;
}
interface Evidence {
 first_frame_elapsed_ms?: number;
 first_video_elapsed_ms?: number;
  connected: boolean;
  published: boolean;
  subscribed: boolean;
  sent_frames: number;
  sent_bytes: number;
  received_frames: number;
  valid_samples: number;
  sent_video_frames?: number;
  received_video_frames?: number;
  video_width?: number;
  video_height?: number;
  media_duration_ms: number;
}
interface Endpoint {
  job: {
 probe_location?: string;
 probe_version?: string;
 sdk_version?: string;
 platform?: string; probe_id: string; endpoint_role: string };
  ready: boolean;
  events: Event[];
  result?: {
    stages?: {stage:string;started_elapsed_ms:number;duration_ms:number}[];
    trace_complete?:boolean;trace_dropped?:number;
    process_exit?:{exit_code?:number;signal?:string;deadline_exceeded:boolean};
    outcome: string;
    reason: string;
    cleanup: string;
    evidence: Evidence;
  };
}
interface LogFile {
  partial?: boolean;
  error?: string;
  request_id: string;
  endpoint_role: string;
  status: string;
  bytes?: number;
  sha256?: string;
}
interface EdgeResult {endpoint:string;run_id?:string;status:string;reason?:string;receiver_evidence?:Evidence;cleanup?:string;started_at?:string;finished_at?:string;}
interface Run {
 kind?:string;
 parent_run_id?:string;
 edge_endpoint?:string;
 edges?:EdgeResult[];
  run_id: string;
  task_id: string;
  target_id: string;
  config_revision: string;
  driver: string;
  status: string;
  reason: string;
  created_at: string;
  finished_at?: string;
  endpoints: Record<string, Endpoint>;
  logs: Record<string, LogFile>;
}
interface Snapshot {
  mode: string;
  config_revision: string;
  rtc_service_status: string;
  storage_available: boolean;
  probes: Probe[];
  targets: Target[];
  tasks: Task[];
  runs: Run[];
  settings: RTCStorageSettings;
  task_health: Record<
    string,
    {
      state: string;
      reason: string;
      failure_count: number;
      success_count: number;
      failure_threshold: number;
      recovery_threshold: number;
      last_success_at?: string;
      latest_result?: string;
      last_run_id?: string;
      last_result_at?: string;
    }
  >;
}
type Edit = { kind: "target"; value?: Target } | { kind: "task"; value?: Task };
const terminal = (s: string) =>
  ["SUCCESS", "FAILED", "ERROR", "CANCELLED"].includes(s);
const time = (s?: string) =>
  s ? new Date(s).toLocaleString("zh-CN", { hour12: false }) : "—";
const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "操作未完成";
const statuses: Record<string, string> = {
  PREPARING: "准备中",
  RUNNING: "执行中",
  SUCCESS: "通过",
  FAILED: "失败",
  ERROR: "探测执行错误",
  CANCELLED: "已取消",
  PENDING: "待探测",
  REQUESTED: "等待回捞",
  AVAILABLE: "可下载",
  EXPIRED: "已过期",
  UNKNOWN: "未知",
};
const reasons: Record<string, string> = {
 MEDIA_EDGE_SWEEP_RUNNING:"正在逐个探测 UDP edge",
 ALL_MEDIA_EDGES_VERIFIED:"全部 UDP edge 音视频接收通过",
 MEDIA_EDGE_FAILED:"存在探测失败的 UDP edge",
 MEDIA_EDGE_INCOMPLETE:"部分 UDP edge 未完成有效探测",
 SWEEP_DEADLINE_EXCEEDED:"端口范围探测超过整轮期限",
 SDK_CONFIGURE_MEDIA_EDGE_FAILED:"SDK 未能设置指定 UDP edge",

  PROBE_CAPABILITY_MISMATCH: "探针不支持本任务所需的音视频能力",
  LOCAL_MEDIA_UNAVAILABLE: "探针 demo 素材不可读，请检查离线镜像与文件",
  WORKER_START_FAILED: "探测工作进程未能启动",
  WORKER_DEADLINE_EXCEEDED: "工作进程超过硬超时并已终止",
  SDK_PROCESS_CRASHED: "SDK 工作进程异常终止，请查看退出信息与原生日志",
  WORKER_EXITED_WITHOUT_RESULT: "工作进程退出前未产生结果，请查看进程日志",
  LOCAL_RESOURCE_EXHAUSTED: "探针本地空间不足，请调整保留时间或磁盘预算",
  PROBE_STORAGE_EXHAUSTED: "探针诊断空间不足，已暂停新任务",
  CONTROL_START_TIMEOUT: "等待中心开始指令超时，请检查探针连接和中心负载",
  PROBE_OFFLINE: "探针离线，请检查探测服务与中心连接",
  PROBE_DISABLED: "探针已停用",
  RESULT_EXPIRED: "结果已过期，等待新一轮有效探测",
  STORAGE_UNAVAILABLE: "中心存储不可用，请检查数据库与容量",
  WAITING_FIRST_RESULT: "等待首次有效探测",
  AUDIO_RECEIVE_TIMEOUT: "音频接收超时",
  VIDEO_RECEIVE_TIMEOUT: "视频接收超时",
  SIMULATION_ONLY: "历史执行结果",
  MEDIA_VERIFIED: "音视频接收证据通过",
  MEDIA_RECEIVE_TIMEOUT: "音视频接收超时",
  SDK_JOIN_FAILED: "加入频道失败",
  SDK_INITIALIZE_FAILED: "SDK 初始化失败",
  MOCK_EXECUTION: "模拟执行",
  MEDIA_TIMEOUT: "媒体接收超时",
  MOCK_INITIALIZATION_ERROR: "模拟 SDK 初始化错误",
  MOCK_AUTH_REJECTED: "模拟鉴权拒绝",
  MOCK_PUBLISH_FAILED: "模拟发布失败",
  MANUAL_CANCEL: "手动取消",
  TASK_DISABLED: "任务已停用",
  CONFIG_CHANGED: "配置变更，本轮已取消",
  CENTER_RESTARTED: "中心重启，本轮已中断",
  EXECUTION_DEADLINE_EXCEEDED: "工作进程超过执行与清理期限",
};
function State({ value }: { value: string }) {
  return (
    <Tag
      color={
        value === "SUCCESS" || value === "AVAILABLE"
          ? "success"
          : value === "FAILED" || value === "ERROR"
            ? "error"
            : value === "RUNNING" ||
                value === "PREPARING" ||
                value === "REQUESTED"
              ? "processing"
              : "default"
      }
    >
      {statuses[value] || value}
    </Tag>
  );
}

export default function RTCProbes({
  api,
  canManage,
  canDiagnose,
}: {
  api: API;
  canManage: boolean;
  canDiagnose: boolean;
}) {
  const [data, setData] = useState<Snapshot | null>(null),
    [error, setError] = useState(""),
    [disabled, setDisabled] = useState(false),
    [busy, setBusy] = useState(""),
    [selected, setSelected] = useState<string | null>(null),
    [edit, setEdit] = useState<Edit | null>(null),
    [pane, setPane] = useState("tasks");
  const [registerProbe, setRegisterProbe] = useState(false);
  const [history, setHistory] = useState<Run[]>([]),
    [historyTotal, setHistoryTotal] = useState(0),
    [historyPage, setHistoryPage] = useState(1);
  const active = useRef(true),
    fetching = useRef(false);
  const refresh = useCallback(async () => {
    if (fetching.current) return;
    fetching.current = true;
    try {
      const result = await api.get<Snapshot>("/api/v1/rtc/status");
      if (active.current) {
        setData({
          ...result,
          targets: result.targets || [],
          tasks: result.tasks || [],
          probes: result.probes || [],
          runs: result.runs || [],
        });
        setError("");
        setDisabled(false);
      }
    } catch (e) {
      if (active.current) {
        setDisabled(e instanceof APIError && e.code === "RTC_PROBE_DISABLED");
        setError(errorText(e));
      }
    } finally {
      fetching.current = false;
    }
  }, [api]);
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
    if (busy) return;
    setBusy(key);
    setError("");
    try {
      const result = await api.post<Run>(path, body);
      if (result.run_id && path.endsWith("/run")) setSelected(result.run_id);
      await refresh();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  };
  const targetRTC = (task:Task)=>data?.targets.find(t=>t.target_id===task.target_id)?.rtc;
  const targetName = (id: string) =>
    data?.targets.find((t) => t.target_id === id)?.name || id;
  const probeName = (id: string) => {
    const p = data?.probes.find((p) => p.probe_id === id);
    return p ? `${p.location} · ${id}` : id;
  };
  const sameHost = (t: Task) => {
    const a = data?.probes.find((p) => p.probe_id === t.sender_probe_id),
      b = data?.probes.find((p) => p.probe_id === t.receiver_probe_id);
    return !!a?.host_address && a.host_address === b?.host_address;
  };
  const [archivedDetail,setArchivedDetail]=useState<Run|null>(null);
  useEffect(()=>{
    let live=true;
    if(!selected){setArchivedDetail(null);return;}
    void api.get<Run>(`/api/v1/rtc/runs/${selected}`).then(run=>{if(live)setArchivedDetail(run);}).catch(e=>{if(live)setError(errorText(e));});
    return()=>{live=false;};
  },[api,selected]);
  const current =
    data?.runs.find((r) => r.run_id === selected) ||
    (archivedDetail?.run_id===selected?archivedDetail:undefined) ||
    history.find((r) => r.run_id === selected);
  useEffect(() => {
    if (pane !== "history") return;
    let live = true;
    api
      .get<{ runs: Run[]; total: number }>(
        `/api/v1/rtc/history?limit=10&offset=${(historyPage - 1) * 10}`,
      )
      .then((v) => {
        if (live) {
          setHistory(v.runs);
          setHistoryTotal(v.total);
        }
      })
      .catch((e) => {
        if (live) setError(errorText(e));
      });
    return () => {
      live = false;
    };
  }, [api, pane, historyPage, data?.runs.length]);
  return (
    <section className="rtc-screen">
      <div className="rtc-heading">
        <div>
          <Typography.Title level={2}>RTC 业务探测</Typography.Title>
          <Typography.Text type="secondary">
            验证整体 RTC 服务在已配置路径上的加入、发布和媒体接收功能。
          </Typography.Text>
        </div>
        <Button
          onClick={() => void refresh()}
          loading={fetching.current && !data}
        >
          刷新
        </Button>
      </div>
      {disabled ? (
        <Card>
          <Empty description="尚未配置 RTC 探测服务" />
          <Typography.Paragraph type="secondary">
            登记独立探针身份后，可在这里创建整体服务目标与双端探测任务。
          </Typography.Paragraph>
        </Card>
      ) : (
        <>
          <Alert
            type="info"
            showIcon
            message="真实 RTC SDK 音视频探测"
            description="使用 demo 固定音视频文件，从同一探测服务的两个 RTC 连接验证整体服务；请先在目标服务配置实际 AppID 与接入参数，再创建关联任务。"
          />
          {error && (
            <Alert
              className="page-alert"
              type="error"
              showIcon
              message={error}
            />
          )}
          {data?.storage_available === false && (
            <Alert
              className="page-alert"
              type="error"
              showIcon
              message="探测数据存储不可用，执行结果暂无法可靠保存"
            />
          )}
          <div className="rtc-summary">
            <Card size="small">
              <Typography.Text type="secondary">
                整体服务业务状态
              </Typography.Text>
              <div className="rtc-summary-value">
                {data?.rtc_service_status === "HEALTHY"
                  ? "已配置路径正常"
                  : data?.rtc_service_status === "UNHEALTHY"
                    ? "存在异常路径"
                    : "待有效证据"}
              </div>
              <Typography.Text type="secondary">
                {data?.targets.length || 0} 个目标服务
              </Typography.Text>
            </Card>
            <Card size="small">
              <Typography.Text type="secondary">在线探针</Typography.Text>
              <div className="rtc-summary-value">
                {data?.probes.filter((p) => p.online).length || 0}
                <small> / {data?.probes.length || 0}</small>
              </div>
              <Typography.Text type="secondary">
                连接在线与业务状态分别显示
              </Typography.Text>
            </Card>
            <Card size="small">
              <Typography.Text type="secondary">已启用任务</Typography.Text>
              <div className="rtc-summary-value">
                {data?.tasks.filter((t) => t.enabled).length || 0}
              </div>
              <Typography.Text type="secondary">
                {data?.runs.filter((r) => !r.parent_run_id && !terminal(r.status)).length || 0}{" "}
                轮执行中
              </Typography.Text>
            </Card>
            <Card size="small">
              <Typography.Text type="secondary">最近执行记录</Typography.Text>
              <div className="rtc-summary-value">{data?.runs.filter(r=>!r.parent_run_id).length || 0}</div>
              <Typography.Text type="secondary">
                按路径保留双端证据
              </Typography.Text>
            </Card>
          </div>
          <Card className="rtc-content-card">
            <Tabs
              activeKey={pane}
              onChange={setPane}
              items={[
                {
                  key: "tasks",
                  label: "探测任务",
                  children: (
                    <>
                      <div className="rtc-section-toolbar">
                        <Typography.Text type="secondary">
                          一条任务对应一个整体服务目标和一条发送 → 接收路径。
                        </Typography.Text>
                        {canManage && (
                          <Button
                            type="primary"
                            onClick={() => setEdit({ kind: "task" })}
                            disabled={
                              !data?.targets.length ||
                              !data?.probes.some((p) => p.driver === "agora")
                            }
                          >
                            创建任务
                          </Button>
                        )}
                      </div>
                      <Table<Task>
                        rowKey="task_id"
                        dataSource={data?.tasks || []}
                        scroll={{ x: 1000 }}
                        size="small"
                        pagination={false}
                        locale={{
                          emptyText: "暂无探测任务，请先登记探针并创建目标服务",
                        }}
                        columns={[
                          {
                            title: "任务 / 目标服务",
                            key: "name",
                            width: 210,
                            render: (_, t) => (
                              <>
                                <Typography.Text strong>
                                  {t.task_id}
                                </Typography.Text>
                                <div>{targetName(t.target_id)}</div>
                              </>
                            ),
                          },
                          {
                            title: "双端路径",
                            key: "path",
                            width: 280,
                            render: (_, t) => (
                              <>
                                <div>{probeName(t.sender_probe_id)}</div>
                                <div className="rtc-path-arrow">↓</div>
                                <div>{probeName(t.receiver_probe_id)}</div>
                                {sameHost(t) && (
                                  <Tag>
                                    {t.sender_probe_id === t.receiver_probe_id
                                      ? "单探针双连接"
                                      : "同机探测"}
                                  </Tag>
                                )}
                              </>
                            ),
                          },
                          {
                            title: "调度",
                            key: "schedule",
                            width: 120,
                            render: (_, t) => (
                              <>
                                <div>每 {t.interval_seconds} 秒</div>
                                <Typography.Text type="secondary">
                                  单端口 / 会话超时 {t.timeout_seconds} 秒
                                </Typography.Text>
                                {targetRTC(t)?.media_edges?.enabled && <div><Tag>UDP {targetRTC(t)!.media_edges!.host}:{targetRTC(t)!.media_edges!.start_port}–{targetRTC(t)!.media_edges!.end_port}</Tag></div>}
                              </>
                            ),
                          },
                          {
                            title: "最近结果 / 业务状态",
                            key: "latest",
                            width: 140,
                            render: (_, t) => {
                              const r = data?.runs.find(
                                (r) => r.task_id === t.task_id && !r.parent_run_id,
                              );
                              const health=data?.task_health?.[t.task_id];
                              const runID=r?.run_id||health?.last_run_id;
                              const status=r?.status||health?.latest_result;
                              return runID && status ? (
                                <Button
                                  type="link"
                                  onClick={() => setSelected(runID)}
                                >
                                  <State value={status} />
                                </Button>
                              ) : (
                                "尚未执行"
                              );
                            },
                          },
                          {
                            title: "业务状态",
                            key: "health",
                            width: 170,
                            render: (_, t) => {
                              const h = data?.task_health?.[t.task_id];
                              return h ? (
                                <>
                                  <Tag
                                    color={
                                      h.state === "HEALTHY"
                                        ? "success"
                                        : h.state === "UNHEALTHY"
                                          ? "error"
                                          : "default"
                                    }
                                  >
                                    {h.state === "HEALTHY"
                                      ? "正常"
                                      : h.state === "UNHEALTHY"
                                        ? "异常"
                                        : "未知"}
                                  </Tag>
                                  <div>
                                    <Typography.Text type="secondary">
                                      {h.reason === "FAILURE_CONFIRMING"
                                        ? `失败确认 ${h.failure_count}/${h.failure_threshold}`
                                        : h.reason === "RECOVERY_CONFIRMING"
                                          ? `恢复确认 ${h.success_count}/${h.recovery_threshold}`
                                          : reasons[h.reason] || h.reason}
                                    </Typography.Text>
                                  </div>
                                </>
                              ) : (
                                "等待新证据"
                              );
                            },
                          },
                          {
                            title: "启用",
                            key: "enabled",
                            width: 80,
                            render: (_, t) => (
                              <Switch
                                aria-label={`启用任务 ${t.task_id}`}
                                checked={t.enabled}
                                disabled={!canManage || !!busy}
                                onChange={(v) =>
                                  void operate(
                                    t.task_id,
                                    `/api/v1/rtc/tasks/${t.task_id}/enabled`,
                                    { enabled: v },
                                  )
                                }
                              />
                            ),
                          },
                          {
                            title: "操作",
                            key: "actions",
                            width: 180,
                            render: (_, t) => (
                              <Space wrap>
                                {canManage && (
                                  <>
                                    <Button
                                      size="small"
                                      type="primary"
                                      loading={busy === t.task_id}
                                      disabled={
                                        !t.enabled ||
                                        !!busy ||
                                        !!data?.runs.some(
                                          (r) =>
                                            r.task_id === t.task_id &&
                                            !terminal(r.status),
                                        )
                                      }
                                      onClick={() =>
                                        void operate(
                                          t.task_id,
                                          `/api/v1/rtc/tasks/${t.task_id}/run`,
                                        )
                                      }
                                    >
                                      立即探测
                                    </Button>
                                    <Button
                                      size="small"
                                      onClick={() =>
                                        setEdit({ kind: "task", value: t })
                                      }
                                    >
                                      编辑
                                    </Button>
                                  </>
                                )}
                              </Space>
                            ),
                          },
                        ]}
                      />
                    </>
                  ),
                },
                {
                  key: "targets",
                  label: "目标服务",
                  children: (
                    <>
                      <div className="rtc-section-toolbar">
                        <Typography.Text type="secondary">
                          目标服务统一管理 RTC 接入、鉴权、私有化部署和端口范围，任务关联后自动使用。
                        </Typography.Text>
                        {canManage && (
                          <Button
                            type="primary"
                            onClick={() => setEdit({ kind: "target" })}
                          >
                            创建目标服务
                          </Button>
                        )}
                      </div>
                      <Table<Target>
                        rowKey="target_id"
                        dataSource={data?.targets || []}
                        pagination={false}
                        scroll={{ x: 650 }}
                        columns={[
                          { title: "服务名称", dataIndex: "name" },
                          { title: "目标标识", dataIndex: "target_id" },
                          {title:"RTC 接入",key:"rtc",render:(_,t)=>t.rtc?<><div>{t.rtc.app_id}</div><Tag>{({none:"无 Token",certificate:"中心签发 Token",token:"预签发 Token"} as Record<string,string>)[t.rtc.auth_mode]}</Tag></>:<Tag color="warning">接入未配置</Tag>},
                          {title:"UDP edge 范围",key:"edges",render:(_,t)=>t.rtc?.media_edges?.enabled?`${t.rtc.media_edges.host}:${t.rtc.media_edges.start_port}–${t.rtc.media_edges.end_port}`:"默认接入策略"},
                          {
                            title: "关联集群",
                            dataIndex: "cluster_code",
                            render: (v) => v || "—",
                          },
                          {
                            title: "业务状态",
                            key: "health",
                            render: () => <Tag>待真实验证</Tag>,
                          },
                          {
                            title: "操作",
                            key: "actions",
                            render: (_, t) =>
                              canManage ? (
                                <Button
                                  size="small"
                                  onClick={() =>
                                    setEdit({ kind: "target", value: t })
                                  }
                                >
                                  编辑目标
                                </Button>
                              ) : (
                                "只读"
                              ),
                          },
                        ]}
                      />
                    </>
                  ),
                },
                {
                  key: "probes",
                  label: "探针",
                  children: (
                    <>
                      <Typography.Paragraph type="secondary">
                        可登记独立探针身份，查看连接与执行能力；停用会停止分配并清理活动任务。
                      </Typography.Paragraph>
                      {canManage && (
                        <Button
                          type="primary"
                          onClick={() => setRegisterProbe(true)}
                          style={{ marginBottom: 16 }}
                        >
                          登记探针
                        </Button>
                      )}
                      <Table<Probe>
                        rowKey="probe_id"
                        dataSource={data?.probes || []}
                        pagination={false}
                        scroll={{ x: 850 }}
                        columns={[
                          { title: "探针", dataIndex: "probe_id" },
 {title:"本地诊断空间",key:"storage",render:(_,p)=><>{((p.storage_bytes||0)/1048576).toFixed(1)} MiB{p.resource_error&&<div><Tag color="error">本地资源不足</Tag></div>}</>},
 {title:"能力",key:"capabilities",render:(_,p)=>(p.capabilities||[]).map(c=><Tag key={c}>{({rtc_audio_v1:"音频",rtc_video_v1:"视频",retention_v1:"保留与配额",log_chunks_v1:"断点上传"} as Record<string,string>)[c]||c}</Tag>)},
                          {
                            title: "启用",
                            key: "enabled",
                            render: (_, p) => (
                              <Switch
                                aria-label={`启用探针 ${p.probe_id}`}
                                checked={p.enabled !== false}
                                disabled={!canManage || !!busy}
                                onChange={(v) =>
                                  void operate(
                                    p.probe_id,
                                    `/api/v1/rtc/probes/${p.probe_id}/enabled`,
                                    { enabled: v },
                                  )
                                }
                              />
                            ),
                          },
                          { title: "部署位置", dataIndex: "location" },
                          {
                            title: "主机",
                            dataIndex: "host_address",
                            render: (v) => v || "未登记",
                          },
                          {
                            title: "连接",
                            key: "online",
                            render: (_, p) => (
                              <Tag color={p.online ? "success" : "default"}>
                                {p.online ? "在线" : "离线"}
                              </Tag>
                            ),
                          },
                          {
                            title: "最近心跳",
                            dataIndex: "last_heartbeat",
                            render: time,
                          },
                          {
                            title: "平台 / SDK",
                            key: "sdk",
                            render: (_, p) => (
                              <>
                                {p.platform || "—"}
                                <div>{p.sdk_version || "—"}</div>
                              </>
                            ),
                          },
                          {
                            title: "并发容量",
                            dataIndex: "capacity",
                            render: (v) => v || "—",
                          },
                        ]}
                      />
                    </>
                  ),
                },
                {
                  key: "retention",
                  label: "历史与容量",
                  children: data?.settings ? (
                    <RTCRetention
                      api={api}
                      settings={data.settings}
                      canManage={canManage}
                      dailyRuns={(data.tasks || []).filter(t => t.enabled).reduce((sum, t) => sum + 86400 / t.interval_seconds, 0)}
                      onSaved={refresh}
                    />
                  ) : null,
                },
                {key:"audit",label:"状态与操作审计",children:<RTCAuditHistory api={api}/>},
                {
                  key: "history",
                  label: "执行记录",
                  children: (
                    <Table<Run>
                      rowKey="run_id"
                      dataSource={history}
                      scroll={{ x: 900 }}
                      pagination={{
                        pageSize: 10,
                        total: historyTotal,
                        current: historyPage,
                        onChange: setHistoryPage,
                      }}
                      locale={{ emptyText: "暂无执行记录" }}
                      columns={[
                        {
                          title: "任务 / 整体服务",
                          key: "task",
                          render: (_, r) => (
                            <>
                              {r.task_id}
                              <div>{targetName(r.target_id)}</div>
                            </>
                          ),
                        },
                        {
                          title: "开始时间",
                          dataIndex: "created_at",
                          render: time,
                        },
                        {
                          title: "结束时间",
                          dataIndex: "finished_at",
                          render: time,
                        },
                        {
                          title: "执行结果",
                          dataIndex: "status",
                          render: (v, r) =>
                            r.driver === "mock" ? (
                              <Tag>历史模拟 · {statuses[v] || v}</Tag>
                            ) : (
                              <State value={v} />
                            ),
                        },
                        {
                          title: "说明",
                          dataIndex: "reason",
                          render: (v) => reasons[v] || v,
                        },
                        {
                          title: "操作",
                          key: "actions",
                          render: (_, r) => (
                            <Button onClick={() => setSelected(r.run_id)}>
                              查看过程
                            </Button>
                          ),
                        },
                      ]}
                    />
                  ),
                },
              ]}
            />
          </Card>
        </>
      )}
      <Drawer
        open={!!selected}
        onClose={() => setSelected(null)}
        title="探测执行详情"
        width="min(1040px, 100vw)"
        destroyOnClose
      >
        {current ? (
          <>
            <Space wrap className="rtc-run-actions">
              <State value={current.status} />
              <Tag>
                {current.driver === "agora"
                  ? "Agora 2.6.6 · 真实 SDK"
                  : "历史模拟记录"}
              </Tag>
              <Button onClick={() => void refresh()}>刷新过程</Button>
              {canManage && !terminal(current.status) && (
                <Button
                  danger
                  disabled={!!busy}
                  onClick={() =>
                    void operate(
                      "cancel",
                      `/api/v1/rtc/runs/${current.run_id}/cancel`,
                    )
                  }
                >
                  取消本轮
                </Button>
              )}
              {canDiagnose && (
                <Button
                  loading={busy === "logs"}
                  onClick={() =>
                    void operate(
                      "logs",
                      `/api/v1/rtc/runs/${current.run_id}/logs`,
                    )
                  }
                >
                  {current.kind==="EDGE_SWEEP"?"回捞各端口日志":"回捞两端日志"}
                </Button>
              )}
            </Space>
            <Descriptions
              size="small"
              column={2}
              bordered
              items={[
                {
                  key: "target",
                  label: "整体 RTC 服务",
                  children: targetName(current.target_id),
                },
                { key: "task", label: "任务", children: current.task_id },
                { key: "run", label: "执行 ID", children: current.run_id },
                {
                  key: "revision",
                  label: "配置版本",
                  children: current.config_revision,
                },
                {
                  key: "created",
                  label: "开始",
                  children: time(current.created_at),
                },
                {
                  key: "finished",
                  label: "结束",
                  children: time(current.finished_at),
                },
              ]}
            />
            {current.parent_run_id && <Button type="link" onClick={()=>setSelected(current.parent_run_id!)}>返回整轮探测</Button>}
            {current.edge_endpoint && <Typography.Paragraph><Tag>指定 UDP edge：{current.edge_endpoint}</Tag></Typography.Paragraph>}
            {current.kind==="EDGE_SWEEP" ? <>
              <Typography.Paragraph>端口进度：{(current.edges||[]).filter(e=>terminal(e.status)).length} / {(current.edges||[]).length}。各端口单独验证音视频收发。</Typography.Paragraph>
              <Table<EdgeResult> rowKey="endpoint" dataSource={current.edges||[]} pagination={false} scroll={{x:760}} columns={[
                {title:"UDP edge",dataIndex:"endpoint"},
                {title:"结果",dataIndex:"status",render:value=><State value={value}/>},
                {title:"有效音频",key:"audio",render:(_,e)=>e.receiver_evidence?`${e.receiver_evidence.media_duration_ms} ms`:"—"},
                {title:"视频帧",key:"video",render:(_,e)=>e.receiver_evidence?.received_video_frames??"—"},
                {title:"原因",dataIndex:"reason",render:value=>reasons[value]||value||"—"},
                {title:"操作",key:"open",render:(_,e)=>e.run_id?<Button type="link" onClick={()=>setSelected(e.run_id!)}>调用与日志</Button>:(terminal(e.status)?"未执行":"待执行")}
              ]}/>
            </> : <Tabs
              className="rtc-endpoint-tabs"
              items={["sender", "receiver"].map((role) => {
                const ep = current.endpoints[role],
                  evidence = ep?.result?.evidence;
                return {
                  key: role,
                  label: role === "sender" ? "发送端 A" : "接收端 B",
                  children: ep ? (
                    <>
                      <Descriptions
                        size="small"
                        column={3}
                        items={[
                          {
                            key: "probe",
                            label: "探针",
                            children: ep.job.probe_id,
                          },
                          {key:"location",label:"位置",children:ep.job.probe_location||"历史未记录"},
                          {key:"versions",label:"探针 / SDK",children:`${ep.job.probe_version||"—"} / ${ep.job.sdk_version||"—"}`},
                          {
                            key: "ready",
                            label: "准备状态",
                            children: ep.ready ? "已准备" : "等待准备",
                          },
                          {
                            key: "cleanup",
                            label: "清理",
                            children:
                              ep.result?.cleanup === "SUCCESS"
                                ? "完成"
                                : ep.result?.cleanup || "待完成",
                          },
                          {key:"trace",label:"调用记录",children:ep.result?`${ep.result.trace_complete?"完整":"未完整确认"} / 丢弃 ${ep.result.trace_dropped??0} 条`:"—"},
                          {key:"process",label:"进程退出",children:ep.result?.process_exit?`${ep.result.process_exit.signal||String(ep.result.process_exit.exit_code??"未启动")}${ep.result.process_exit.deadline_exceeded?"（硬超时）":""}`:"—"},
                          {
                            key: "sent",
                            label: "送帧次数",
                            children: evidence?.sent_frames ?? "—",
                          },
                          {
                            key: "received",
                            label: "有效接收帧",
                            children: evidence?.received_frames ?? "—",
                          },
                          {
                            key: "duration",
                            label: "有效音频",
                            children: evidence
                              ? `${evidence.media_duration_ms} ms`
                              : "—",
                          },
                        ]}
                      />
                      {ep.result && (
                        <Typography.Paragraph
                          type={
                            ep.result.outcome === "ERROR" ||
                            ep.result.outcome === "FAILED"
                              ? "danger"
                              : "secondary"
                          }
                        >
                          {reasons[ep.result.reason] || ep.result.reason}
                        </Typography.Paragraph>
                      )}
                      <Descriptions
                        size="small"
                        column={3}
                        items={[
                          {key:"first-audio",label:"音频首帧",children:evidence?.first_frame_elapsed_ms?`${evidence.first_frame_elapsed_ms} ms`:"—"},
                          {key:"first-video",label:"视频首帧",children:evidence?.first_video_elapsed_ms?`${evidence.first_video_elapsed_ms} ms`:"—"},
                          {
                            key: "sv",
                            label: "发送视频帧",
                            children: evidence?.sent_video_frames ?? "—",
                          },
                          {
                            key: "rv",
                            label: "接收视频帧",
                            children: evidence?.received_video_frames ?? "—",
                          },
                          {
                            key: "size",
                            label: "接收分辨率",
                            children: evidence?.video_width
                              ? `${evidence.video_width} × ${evidence.video_height}`
                              : "—",
                          },
                        ]}
                      />
                      {ep.result?.stages && <Table
                        size="small" rowKey="stage" dataSource={ep.result.stages} pagination={false}
                        title={()=>"本轮阶段耗时"}
                        columns={[{title:"阶段",dataIndex:"stage"},{title:"开始时间",dataIndex:"started_elapsed_ms",render:v=>`${v} ms`},{title:"耗时",dataIndex:"duration_ms",render:v=>`${v} ms`}]}
                      />}
                      <Table<Event>
                        rowKey="event_id"
                        size="small"
                        dataSource={[...(ep.events || [])].sort(
                          (a, b) => a.source_seq - b.source_seq,
                        )}
                        scroll={{ x: 800 }}
                        pagination={{ pageSize: 12 }}
                        expandable={{
                          expandedRowRender: (e) => (
                            <pre className="rtc-event-fields">
                              {JSON.stringify(
                                {
                                  service_id: e.service_id,
                                  connection_id: e.connection_id,
                                  call_id: e.call_id,
                                  related_call_id: e.related_call_id,
                                  ...(e.fields || {}),
                                },
                                null,
                                2,
                              )}
                            </pre>
                          ),
                        }}
                        columns={[
                          {
                            title: "相对耗时",
                            dataIndex: "monotonic_elapsed_ms",
                            width: 100,
                            render: (v) => `${v} ms`,
                          },
                          { title: "阶段", dataIndex: "stage", width: 100 },
                          {
                            title: "类型",
                            dataIndex: "kind",
                            width: 140,
                            render: (v) =>
                              ({
                                api_call_begin: "方法调用",
                                api_call_end: "方法返回",
                                sdk_callback: "SDK 回调",
                                sdk_trace_summary: "API / 回调汇总",
                                media_summary: "媒体汇总",
                                worker_event: "工作进程",
                              })[v as string] || v,
                          },
                          {
                            title: "方法 / 回调",
                            dataIndex: "name",
                            width: 230,
                          },
                          {
                            title: "返回值",
                            key: "code",
                            width: 90,
                            render: (_, e) =>
                              e.return_kind === "object"
                                ? "对象"
                                : (e.return_code ?? "—"),
                          },
                          {
                            title: "方法耗时",
                            dataIndex: "duration_ms",
                            width: 100,
                            render: (v, e) =>
                              e.kind === "api_call_end"
                                ? v
                                  ? `${v} ms`
                                  : `${e.duration_us || 0} µs`
                                : "—",
                          },
                        ]}
                      />
                    </>
                  ) : (
                    <Empty />
                  ),
                };
              })}
            />}
            <Typography.Title level={5}>本轮日志</Typography.Title>
            <Space direction="vertical" style={{ width: "100%" }}>
              {Object.values(current.logs || {}).length ? (
                Object.values(current.logs)
                  .sort((a, b) =>
                    a.endpoint_role === b.endpoint_role
                      ? 0
                      : a.endpoint_role === "sender"
                        ? -1
                        : 1,
                  )
                  .map((file) => (
                    <Card size="small" key={file.request_id}>
                      <div className="rtc-log-row">
                        <Space wrap>
                          <Typography.Text strong>
                            {file.endpoint_role === "sender"
                              ? "发送端 A"
                              : "接收端 B"}
                          </Typography.Text>
                          <State value={file.status} />
                          {file.partial&&<Tag>过程快照</Tag>}
                          {file.error&&<Typography.Text type="secondary">{({LOCAL_RETENTION_EXPIRED:"探针本地日志已过保留期",SDK_NOT_STARTED:"SDK 未启动，无原生日志",LOG_BUNDLE_LIMIT_EXCEEDED:"诊断包超过大小上限"} as Record<string,string>)[file.error]||file.error}</Typography.Text>}
                          {file.bytes && (
                            <Typography.Text type="secondary">
                              {(file.bytes / 1024).toFixed(1)} KB
                            </Typography.Text>
                          )}
                        </Space>
                        {canDiagnose && file.status === "AVAILABLE" && (
                          <Button
                            onClick={() =>
                              void api
                                .download(
                                  `/api/v1/rtc/logs/${file.request_id}`,
                                  `rtc-${current.run_id}-${file.endpoint_role}.zip`,
                                )
                                .catch((e) => setError(errorText(e)))
                            }
                          >
                            下载日志包
                          </Button>
                        )}
                      </div>
                    </Card>
                  ))
              ) : (
                <Typography.Text type="secondary">
                  {current.kind==="EDGE_SWEEP"?"每个端口结束后自动回捞日志，点击对应端口“调用与日志”查看。":"尚未请求日志；执行失败或清理异常时会自动回捞。"}
                </Typography.Text>
              )}
            </Space>
          </>
        ) : (
          <Empty description="正在读取本轮执行" />
        )}
      </Drawer>
      {edit && data && (
        <Editor
          key={edit.kind + (edit.value ? "edit" : "create")}
          edit={edit}
          snapshot={data}
          onClose={() => setEdit(null)}
          onSave={async (value) => {
            const targets = [...data.targets],
              tasks = [...data.tasks];
            if (edit.kind === "target") {
              const v = { ...(value as Target), driver: "agora" };
              const i = targets.findIndex((t) => t.target_id === v.target_id);
              if (i >= 0 && !edit.value) throw new Error("目标标识已存在");
              if (i < 0) targets.push(v);
              else targets[i] = v;
            } else {
              const v = value as Task;
              v.receiver_probe_id = v.sender_probe_id;
              const i = tasks.findIndex((t) => t.task_id === v.task_id);
              if (i >= 0 && !edit.value) throw new Error("任务标识已存在");
              if (i < 0) tasks.push(v);
              else tasks[i] = v;
            }
            await api.post("/api/v1/rtc/config", {
              expected_revision: data.config_revision,
              targets,
              tasks,
            });
            setEdit(null);
            await refresh();
          }}
        />
      )}
      {registerProbe && (
        <ProbeRegistration
          api={api}
          onClose={() => setRegisterProbe(false)}
          onSaved={refresh}
        />
      )}
    </section>
  );
}

function Editor({
  edit,
  snapshot,
  onClose,
  onSave,
}: {
  edit: Edit;
  snapshot: Snapshot;
  onClose: () => void;
  onSave: (value: unknown) => Promise<void>;
}) {
  const [error, setError] = useState(""),
    [saving, setSaving] = useState(false);
  const target = edit.kind === "target";
  const [form]=Form.useForm();
  const selectedTargetID=Form.useWatch("target_id",form);
  const selectedTarget=snapshot.targets.find(t=>t.target_id===selectedTargetID);
  const idRules = [
    { required: true, message: "请填写标识" },
    {
      pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$/,
      message: "使用字母、数字、下划线或连字符",
    },
  ];
  return (
    <Modal
      open
      title={`${edit.value ? "编辑" : "创建"}${target ? "目标服务" : "探测任务"}`}
      footer={null}
      onCancel={onClose}
      destroyOnClose
    >
      <Typography.Paragraph type="secondary">
        {target
          ? "配置整体 RTC 服务的接入、鉴权、私有化部署与 UDP edge 端口范围。"
          : "选择目标服务后自动使用其接入配置，再选择探针和执行规则。"}
        保存配置会取消关联任务中正在执行的旧轮次。
      </Typography.Paragraph>
      <Form
        form={form}
        layout="vertical"
        initialValues={
          (target && edit.value?{...edit.value,rtc:(edit.value as Target).rtc||rtcDefaults}:edit.value) ||
          (target
            ? {rtc:rtcDefaults}
            : {
                enabled: true,
                interval_seconds: 60,
                timeout_seconds: 30,
                failure_threshold: 3,
                recovery_threshold: 2,
                unknown_notify_seconds: 120,
              })
        }
        onFinish={async (value) => {
          setSaving(true);
          setError("");
          try {
            await onSave(value);
          } catch (e) {
            setError(errorText(e));
          } finally {
            setSaving(false);
          }
        }}
      >
        {target ? (
          <>
            <Form.Item label="目标标识" name="target_id" rules={idRules}>
              <Input disabled={!!edit.value} />
            </Form.Item>
            <Form.Item
              label="服务名称"
              name="name"
              rules={[{ required: true, message: "请填写服务名称" }]}
            >
              <Input maxLength={128} />
            </Form.Item>
            <Form.Item label="关联集群编号（可选）" name="cluster_code">
              <Input />
            </Form.Item>
            <RTCSettings
              hasCertificate={edit.kind==="target" && edit.value?.has_certificate}
              hasSenderToken={edit.kind==="target" && edit.value?.has_sender_token}
              hasReceiverToken={edit.kind==="target" && edit.value?.has_receiver_token}
            />
          </>
        ) : (
          <>
            <Form.Item label="任务标识" name="task_id" rules={idRules}>
              <Input disabled={!!edit.value} />
            </Form.Item>
            <Form.Item
              label="目标服务"
              name="target_id"
              rules={[{ required: true, message: "请选择目标服务" }]}
            >
              <Select onChange={id=>{const target=snapshot.targets.find(t=>t.target_id===id);if(target?.rtc?.media_edges?.enabled && form.getFieldValue("interval_seconds")<=60){form.setFieldValue("interval_seconds",600);}}}
                options={snapshot.targets
                  .filter((t) => t.driver === "agora" && !!t.rtc)
                  .map((t) => ({
                    value: t.target_id,
                    label: t.name,
                  }))}
              />
            </Form.Item>
            {selectedTarget && <TargetConnectionSummary target={selectedTarget}/>}
            <Form.Item
              label="执行探针"
              name="sender_probe_id"
              rules={[{ required: true, message: "请选择真实 SDK 探针" }]}
            >
              <Select
                options={snapshot.probes
                  .filter((p) => p.driver === "agora")
                  .map((p) => ({
                    value: p.probe_id,
                    label: `${p.location} · ${p.probe_id}`,
                  }))}
              />
            </Form.Item>
            <div className="rtc-form-grid">
              <Form.Item
                label="探测周期（秒）"
                name="interval_seconds"
                rules={[{ required: true }]}
              >
                <InputNumber min={30} max={3600} precision={0} />
              </Form.Item>
              <Form.Item
                label="单端口 / 会话超时（秒）"
                name="timeout_seconds"
                dependencies={["interval_seconds"]}
                rules={[
                  { required: true },
                  ({ getFieldValue }) => ({
                    validator: (_, v) =>
                      v < getFieldValue("interval_seconds")
                        ? Promise.resolve()
                        : Promise.reject(new Error("超时需小于周期")),
                  }),
                ]}
              >
                <InputNumber min={7} max={120} precision={0} />
              </Form.Item>
            </div>
            <div className="rtc-form-grid">
              <Form.Item
                label="连续失败确认次数"
                name="failure_threshold"
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={10} />
              </Form.Item>
              <Form.Item
                label="连续成功恢复次数"
                name="recovery_threshold"
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={10} />
              </Form.Item>
            </div>
            <Form.Item
              label="持续未知通知等待（秒）"
              name="unknown_notify_seconds"
              rules={[{ required: true }]}
            >
              <InputNumber min={30} max={3600} />
            </Form.Item>
            <Form.Item label="任务描述" name="description">
              <Input.TextArea maxLength={1000} />
            </Form.Item>

            <Form.Item label="启用任务" name="enabled" valuePropName="checked">
              <Switch />
            </Form.Item>
          </>
        )}
        {error && (
          <Alert className="page-alert" type="error" showIcon message={error} />
        )}
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" htmlType="submit" loading={saving}>
            保存配置
          </Button>
        </Space>
      </Form>
    </Modal>
  );
}

function TargetConnectionSummary({target}:{target:Target}) {
 const rtc=target.rtc;
 if(!rtc)return <Alert type="warning" message="该目标尚未配置 RTC 接入，请先编辑目标服务"/>;
 return <Card size="small" title="来自目标服务的接入配置" style={{marginBottom:16}}>
  <Descriptions size="small" column={1} items={[
    {key:"service",label:"目标服务",children:target.name},
    {key:"app",label:"AppID",children:rtc.app_id},
    {key:"auth",label:"鉴权",children:({none:"项目未启用 Token",certificate:`中心签发 Token（凭证${target.has_certificate?"已配置":"未配置"}）`,token:`预签发 Token（发送${target.has_sender_token?"已配置":"未配置"}，接收${target.has_receiver_token?"已配置":"未配置"}）`} as Record<string,string>)[rtc.auth_mode]},
    {key:"uids",label:"发送 / 接收 UID",children:`${rtc.sender_uid} / ${rtc.receiver_uid}`},
    {key:"private",label:"私有化部署",children:rtc.private_deployment?`${[...(rtc.access_ips||[]),...(rtc.access_domains||[])].join(", ")} · ${rtc.verify_domain||"未指定校验域名"}`:"公共接入"},
    {key:"edges",label:"UDP edge",children:rtc.media_edges?.enabled?`${rtc.media_edges.host}:${rtc.media_edges.start_port}–${rtc.media_edges.end_port}（逐端口）`:"默认接入策略"},
  ]}/>
  <Typography.Text type="secondary">在目标服务中修改后，关联任务自动使用新配置；执行历史保留原参数。</Typography.Text>
 </Card>;
}
