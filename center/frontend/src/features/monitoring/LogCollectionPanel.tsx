import {
  ArrowRightOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  DownloadOutlined,
  ExclamationCircleOutlined,
  FileSearchOutlined,
  FileZipOutlined,
  FolderOpenOutlined,
  LoadingOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Segmented from "antd/es/segmented";
import Skeleton from "antd/es/skeleton";
import Tag from "antd/es/tag";
import Typography from "antd/es/typography";
import { type ReactNode, useEffect, useRef, useState } from "react";
import type { API } from "../../shared/api";
import LogServicePicker, { type LogService } from "./LogServicePicker";
type Source = { agent_id: string; source: string; services?: LogService[] };
type Collection = Source & {
  id: string;
  status: "COLLECTING" | "READY" | "FAILED";
  message: string;
  filename: string;
  mode: string;
  time: string;
  bytes: number;
  file_count: number;
  expires_at: string;
  created_at: string;
};

const bytesLabel = (value: number) =>
  value >= 1048576
    ? `${(value / 1048576).toFixed(2)} MiB`
    : value >= 1024
      ? `${(value / 1024).toFixed(1)} KiB`
      : `${value} B`;
const localDateTime = (value: Date) =>
  new Date(value.getTime() - value.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 19);

export default function LogCollectionPanel({
  api,
  agentID,
  hostAddress,
  agentOnline,
  hostSelection,
}: {
  api: API;
  agentID: string;
  hostAddress: string;
  agentOnline: boolean;
  hostSelection: ReactNode;
}) {
  const [form] = Form.useForm<{ mode: string; time: string }>();
  const mode = Form.useWatch("mode", form) || "point";
  const selectedTime = Form.useWatch("time", form);
  const previousTimes = useRef({
    point: localDateTime(new Date()),
    day: new Date().toISOString().slice(0, 10),
  });
  const [source, setSource] = useState<Source>();
  const [sourceError, setSourceError] = useState("");
  const [sourceRevision, setSourceRevision] = useState(0);
  const [serviceIDs, setServiceIDs] = useState<string[]>([]);
  const [scope, setScope] = useState<"containers" | "host">("containers");
  const [job, setJob] = useState<Collection>();
  const [error, setError] = useState("");
  const [submitError, setSubmitError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [restoring, setRestoring] = useState(true);
  const [downloading, setDownloading] = useState(false);
  const [downloaded, setDownloaded] = useState(false);
  const storageKey = `agora-host-log-collection:${agentID}`;

  useEffect(() => {
    let live = true;
    setSource(undefined);
    setSourceError("");
    const load = () =>
      api
        .get<Source>(
          `/api/v1/log-collections/source?agent_id=${encodeURIComponent(agentID)}`,
        )
        .then((value) => {
          if (live) {
            setSource(value);
            setSourceError("");
          }
        })
        .catch((e) => {
          if (live) {
            setSource(undefined);
            setSourceError(e.message);
          }
        });
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 15000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [api, agentID, sourceRevision, agentOnline]);

  useEffect(() => {
    let live = true;
    let id = "";
    try {
      id = sessionStorage.getItem(storageKey) || "";
    } catch {}
    if (!id) {
      setRestoring(false);
      return;
    }
    api
      .get<Collection>(`/api/v1/log-collections/${encodeURIComponent(id)}`)
      .then((value) => {
        if (!live) return;
        setJob(value);
        setServiceIDs(
          value.services?.map((service) => service.container_id) || [],
        );
        setScope(value.services?.length ? "containers" : "host");
        form.setFieldsValue({
          mode: value.mode,
          time:
            value.mode === "day"
              ? value.time.replace(/^(\d{4})(\d{2})(\d{2})$/, "$1-$2-$3")
              : localDateTime(new Date(value.time)),
        });
      })
      .catch((e) => {
        if (live) setError(e.message);
        if (e.status === 404) {
          try {
            sessionStorage.removeItem(storageKey);
          } catch {}
        }
      })
      .finally(() => {
        if (live) setRestoring(false);
      });
    return () => {
      live = false;
    };
  }, [api, storageKey, form]);

  useEffect(() => {
    if (!job || job.status !== "COLLECTING") return;
    let live = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const value = await api.get<Collection>(
          `/api/v1/log-collections/${job.id}`,
        );
        if (!live) return;
        setJob(value);
        setError("");
        if (value.status !== "COLLECTING") return;
      } catch (e) {
        if (!live) return;
        setError((e as Error).message);
        if ((e as { status?: number }).status === 404) {
          setJob(undefined);
          return;
        }
      }
      timer = setTimeout(() => void poll(), 2000);
    };
    timer = setTimeout(() => void poll(), 1000);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [api, job?.id, job?.status]);

  async function collect(values: { mode: string; time: string }) {
    if (scope === "containers" && (!serviceIDs.length || serviceUnavailable))
      return;
    setSubmitting(true);
    setSubmitError("");
    try {
      const value = await api.post<Collection>("/api/v1/log-collections", {
        agent_id: agentID,
        service_container_ids: scope === "host" ? [] : serviceIDs,
        mode: values.mode,
        time:
          values.mode === "day"
            ? values.time.replaceAll("-", "")
            : new Date(values.time).toISOString(),
      });
      setJob(value);
      setError("");
      setDownloaded(false);
      try {
        sessionStorage.setItem(storageKey, value.id);
      } catch {}
    } catch (e) {
      setSubmitError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  async function download() {
    if (!job) return;
    setDownloading(true);
    setError("");
    try {
      await api.download(
        `/api/v1/log-collections/${job.id}/download`,
        job.filename,
      );
      setDownloaded(true);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setDownloading(false);
    }
  }

  const busy = submitting || restoring || job?.status === "COLLECTING";
  const selectedServices = serviceIDs.map((id) =>
    source?.services?.find((service) => service.container_id === id),
  );
  const serviceUnavailable =
    scope === "containers" &&
    !!source &&
    selectedServices.some((service) => !service?.available);
  const timezone =
    new Intl.DateTimeFormat(undefined, { timeZoneName: "shortOffset" })
      .formatToParts(new Date())
      .find((part) => part.type === "timeZoneName")?.value || "本地时区";
  const expired = !!job && new Date(job.expires_at).getTime() <= Date.now();
  const ready = job?.status === "READY" && !expired;
  const collecting = job?.status === "COLLECTING";
  const failed = job?.status === "FAILED";
  const state = expired
    ? { label: "已过期", color: "default" }
    : ready
      ? { label: "可下载", color: "success" }
      : failed
        ? { label: "拉取失败", color: "error" }
        : collecting
          ? { label: "进行中", color: "processing" }
          : { label: "等待提交", color: "default" };
  const scopeLabel =
    scope === "host"
      ? "整台机器 · 全部服务"
      : serviceIDs.length
        ? `${serviceIDs.length} 个服务容器 · 合并为 1 个包`
        : "请选择至少一个服务容器";
  const selectedTimeLabel =
    mode === "day"
      ? `${selectedTime || "未选择日期"} · UTC 全天`
      : selectedTime && !Number.isNaN(new Date(selectedTime).getTime())
        ? `${new Date(selectedTime).toLocaleString()} · 前后 30 分钟`
        : "未选择时间";
  const jobTime =
    job?.mode === "day"
      ? `${job.time.slice(0, 4)}-${job.time.slice(4, 6)}-${job.time.slice(6, 8)} · UTC 全天`
      : job
        ? `${new Date(job.time).toLocaleString()} · 前后 30 分钟`
        : "";
  const resultTitle = expired
    ? "日志包已过期"
    : ready
      ? "日志包已就绪"
      : failed
        ? "日志拉取失败"
        : collecting
          ? "正在收集日志"
          : "等待创建任务";
  return (
    <div className="log-workbench">
      <Card
        className="log-config-card"
        title={
          <div className="log-card-heading">
            <span className="log-heading-icon">
              <FileSearchOutlined aria-hidden />
            </span>
            <div>
              <Typography.Title level={3}>新建日志拉取</Typography.Title>
            </div>
          </div>
        }
      >
        <section
          className="log-config-section"
          aria-labelledby="log-source-heading"
        >
          <div className="log-section-heading">
            <span className="log-section-number" aria-hidden>
              01
            </span>
            <h4 id="log-source-heading">拉取范围</h4>
            <span
              className={`log-agent-state${agentOnline ? " is-online" : ""}`}
            >
              {agentOnline ? "主机在线" : "主机状态待确认"}
            </span>
          </div>
          <div className="log-target-fields">
            {hostSelection}
            <div className="log-scope-field">
              <span id="log-scope-label">收集范围</span>
              <Segmented
                block
                aria-labelledby="log-scope-label"
                value={scope}
                disabled={busy}
                options={[
                  { value: "containers", label: "指定容器" },
                  { value: "host", label: "整台机器" },
                ]}
                onChange={(value) => {
                  setScope(value as "containers" | "host");
                  setSubmitError("");
                }}
              />
            </div>
          </div>
          {scope === "containers" ? (
            <Form layout="vertical" component={false}>
              <Form.Item
                label="服务容器"
                htmlFor="log-service"
                className="log-service-field"
              >
                <LogServicePicker
                  services={source?.services || job?.services || []}
                  value={serviceIDs}
                  disabled={busy || !source}
                  onChange={(ids) => {
                    setServiceIDs(ids);
                    setSubmitError("");
                  }}
                />
              </Form.Item>
            </Form>
          ) : (
            <p className="log-host-scope-note">
              收集该机器的全部服务日志，包括当前容器重启前的日志。
            </p>
          )}
          {sourceError && (
            <Alert
              className="log-inline-alert"
              type="warning"
              showIcon
              message={sourceError}
              action={
                <Button
                  size="small"
                  onClick={() => setSourceRevision((n) => n + 1)}
                >
                  重试
                </Button>
              }
            />
          )}
          {!source && !sourceError && (
            <div className="log-source-loading" role="status">
              正在读取可用服务…
            </div>
          )}
          {serviceUnavailable && (
            <Alert
              type="warning"
              showIcon
              className="log-inline-alert"
              message={"部分已选容器已不可用，请移除或重新选择后再拉取。"}
            />
          )}
        </section>
        <section
          className="log-config-section log-time-section"
          aria-labelledby="log-time-heading"
        >
          <div className="log-section-heading">
            <span className="log-section-number" aria-hidden>
              02
            </span>
            <h4 id="log-time-heading">收集时间</h4>
          </div>
          <Form
            form={form}
            layout="vertical"
            onFinish={(values) => void collect(values)}
            initialValues={{ mode: "point", time: localDateTime(new Date()) }}
          >
            <div className="log-time-fields">
              <Form.Item name="mode" label="收集方式">
                <Segmented
                  block
                  disabled={busy}
                  onChange={(next) => {
                    previousTimes.current[mode as "point" | "day"] =
                      form.getFieldValue("time");
                    form.setFieldValue(
                      "time",
                      previousTimes.current[next as "point" | "day"],
                    );
                  }}
                  options={[
                    { value: "point", label: "指定时间" },
                    { value: "day", label: "UTC 全天" },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="time"
                label={mode === "day" ? "UTC 日期" : "目标时间"}
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    message:
                      mode === "day" ? "请选择 UTC 日期" : "请选择目标时间",
                  },
                ]}
              >
                <Input
                  type={mode === "day" ? "date" : "datetime-local"}
                  step={mode === "day" ? undefined : 1}
                  disabled={busy}
                />
              </Form.Item>
            </div>
            <div className="log-time-hint">
              <ClockCircleOutlined aria-hidden />
              <span>
                {mode === "day"
                  ? "收集所选 UTC 日期的全天日志。"
                  : `按 ${timezone} 选择，覆盖目标时间前后 30 分钟。`}
              </span>
              <Button
                type="link"
                size="small"
                disabled={busy}
                onClick={() =>
                  form.setFieldValue(
                    "time",
                    mode === "day"
                      ? new Date().toISOString().slice(0, 10)
                      : localDateTime(new Date()),
                  )
                }
              >
                {mode === "day" ? "今天（UTC）" : "当前时间"}
              </Button>
            </div>
            <details className="log-collection-help">
              <summary>日志来源与匹配说明</summary>
              <div>
                {source && (
                  <p className="log-source-path">
                    <FolderOpenOutlined aria-hidden /> 日志目录{" "}
                    <Typography.Text code copyable>
                      {source.source}
                    </Typography.Text>
                  </p>
                )}
                {(scope === "containers" ? selectedServices : [])
                  .filter(
                    (service): service is LogService => !!service?.available,
                  )
                  .map((service) => (
                    <p key={service.container_id}>
                      {service.container_name} 匹配进程：
                      {service.processes
                        .map(
                          (process) => `${process.name} · PID ${process.pid}`,
                        )
                        .join("，")}
                    </p>
                  ))}
                <p>
                  日志按 UTC 三小时分片，压缩包包含命中分片的完整 DEBUG
                  日志文件。
                </p>
                <p>
                  {scope === "containers"
                    ? "按当前上报 PID 精确匹配。重启前旧 PID 的日志可选择整机拉取。"
                    : "全部服务会收集所选机器日志目录内所有匹配的服务日志。"}
                </p>
              </div>
            </details>
            {submitError && (
              <Alert
                className="log-inline-alert"
                type="error"
                showIcon
                message={submitError}
              />
            )}
            <div className="log-submit-bar">
              <div>
                <span>
                  {collecting
                    ? "任务执行中，完成后可创建下一次拉取"
                    : "本次拉取"}
                </span>
                <strong title={scopeLabel}>{scopeLabel}</strong>
              </div>
              <Button
                type={ready ? "default" : "primary"}
                htmlType="submit"
                icon={!collecting && <ArrowRightOutlined aria-hidden />}
                loading={busy}
                disabled={
                  !source ||
                  !agentOnline ||
                  downloading ||
                  serviceUnavailable ||
                  (scope === "containers" && !serviceIDs.length)
                }
              >
                {collecting ? "正在拉取日志" : "开始拉取"}
              </Button>
            </div>
          </Form>
        </section>
      </Card>
      <Card
        className={`log-result-card${ready ? " is-ready" : failed ? " is-failed" : ""}`}
        title={
          <div className="log-result-heading">
            <Typography.Title level={3}>
              {job ? "最近一次拉取" : "拉取结果"}
            </Typography.Title>
            <Tag color={state.color}>{state.label}</Tag>
          </div>
        }
      >
        {restoring ? (
          <Skeleton active paragraph={{ rows: 5 }} />
        ) : (
          <>
            <div className={`log-result-hero${job ? " has-job" : ""}`}>
              <span className="log-result-symbol" aria-hidden>
                {collecting ? (
                  <LoadingOutlined />
                ) : failed ? (
                  <ExclamationCircleOutlined />
                ) : ready ? (
                  <CheckCircleOutlined />
                ) : (
                  <FileZipOutlined />
                )}
              </span>
              <h4 role="status" aria-live="polite" aria-atomic="true">
                {resultTitle}
              </h4>
              <p>
                {expired
                  ? "重新拉取即可生成新的日志包。"
                  : ready
                    ? "文件已打包完成，可以直接下载。"
                    : failed
                      ? "请调整收集条件后重新拉取。"
                      : collecting
                        ? "正在所选机器上收集、打包，任务会继续执行。"
                        : "选好范围和时间后，生成一个日志压缩包。"}
              </p>
            </div>
            {job ? (
              <div className="log-result-content">
                <dl className="log-job-details">
                  <div>
                    <dt>主机 IP</dt>
                    <dd className="log-monospace">{hostAddress}</dd>
                  </div>
                  <div>
                    <dt>服务范围</dt>
                    <dd>
                      {job.services?.length ? (
                        <details className="log-result-services">
                          <summary>{job.services.length} 个服务容器</summary>
                          <ul>
                            {job.services.map((service) => (
                              <li key={service.container_id}>
                                {service.container_name}
                              </li>
                            ))}
                          </ul>
                        </details>
                      ) : (
                        "全部服务（整台机器）"
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt>创建时间</dt>
                    <dd>{new Date(job.created_at).toLocaleString()}</dd>
                  </div>
                  <div>
                    <dt>日志时间</dt>
                    <dd>{jobTime}</dd>
                  </div>
                </dl>
                {failed && (
                  <Alert
                    className="log-inline-alert"
                    type="error"
                    showIcon
                    message={job.message}
                  />
                )}
                {collecting && (
                  <div className="log-collecting-note">
                    <LoadingOutlined aria-hidden />
                    离开页面后仍可返回查看结果。
                  </div>
                )}
                {ready && (
                  <>
                    <div className="log-result-metrics">
                      <div>
                        <span>日志文件</span>
                        <strong>
                          {job.file_count}
                          <small>个</small>
                        </strong>
                      </div>
                      <div>
                        <span>压缩包大小</span>
                        <strong>{bytesLabel(job.bytes)}</strong>
                      </div>
                    </div>
                    <Button
                      className="log-download-button"
                      type="primary"
                      block
                      icon={<DownloadOutlined aria-hidden />}
                      loading={downloading}
                      disabled={!agentOnline}
                      onClick={() => void download()}
                    >
                      下载日志压缩包
                    </Button>
                    {downloaded && (
                      <p className="log-download-feedback" role="status">
                        已开始下载，可在浏览器下载列表中查看。
                      </p>
                    )}
                    <p className="log-retention">
                      <ClockCircleOutlined aria-hidden />
                      保留至 {new Date(job.expires_at).toLocaleString()}
                    </p>
                    <details className="log-file-details">
                      <summary>压缩包信息</summary>
                      <Typography.Text copyable className="log-archive-name">
                        {job.filename}
                      </Typography.Text>
                    </details>
                  </>
                )}
              </div>
            ) : (
              <dl className="log-job-details log-draft-preview">
                <div>
                  <dt>主机 IP</dt>
                  <dd className="log-monospace">{hostAddress}</dd>
                </div>
                <div>
                  <dt>服务范围</dt>
                  <dd>{scopeLabel}</dd>
                </div>
                <div>
                  <dt>日志时间</dt>
                  <dd>{selectedTimeLabel}</dd>
                </div>
              </dl>
            )}
          </>
        )}
        {error && (
          <Alert
            className="log-download-error"
            type="error"
            showIcon
            message={error}
          />
        )}
      </Card>
    </div>
  );
}
