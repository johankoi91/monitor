import {
  CloudServerOutlined,
  DownloadOutlined,
  FileZipOutlined,
  LinkOutlined,
  OrderedListOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Descriptions from "antd/es/descriptions";
import Empty from "antd/es/empty";
import Modal from "antd/es/modal";
import Tabs from "antd/es/tabs";
import { useState } from "react";
import { rtcRunHref } from "../../app/routes";
import type { API } from "../../shared/api";
import RTCLink from "./RTCLink";
import {
  type Run,
  type Snapshot,
  State,
  isParentRun,
  reasonLabel,
  taskLabel,
  terminal,
  time,
} from "./RTCModel";
import { NativeCases, casePath, phaseLabels } from "./RTCNativeCoverage";
import { DetailBack, PageHeading } from "./RTCUI";
import { RTCSectionHeading } from "./RTCVisual";
export function runTitle(run: Run) {
  const item =
    run.native_case ||
    run.endpoints?.sender?.job.native_case ||
    run.endpoints?.receiver?.job.native_case;
  return isParentRun(run)
    ? `${phaseLabels[run.native_plan?.phase || ""] || "探测"}执行`
    : item
      ? casePath(item)
      : run.edge_endpoint || "路径详情";
}
export default function RTCRunDetails({
  run,
  data,
  api,
  onBack,
  operate,
  busy,
  canManage,
  canDiagnose,
  tab,
  onTab,
}: {
  run: Run;
  data: Snapshot;
  api: API;
  onBack: () => void;
  operate: (key: string, path: string) => Promise<void>;
  busy: string;
  canManage: boolean;
  canDiagnose: boolean;
  tab: string;
  onTab: (tab: string) => void;
}) {
  const [downloadError, setDownloadError] = useState("");
  const sender = run.endpoints?.sender,
    receiver = run.endpoints?.receiver,
    media = receiver?.result?.evidence,
    nativeCase =
      run.native_case || sender?.job.native_case || receiver?.job.native_case;
  const parent = isParentRun(run),
    task = data.tasks.find((t) => t.task_id === run.task_id);
  const title = runTitle(run);
  const cleanups = Object.values(run.endpoints || {}).map(
      (e) => e.result?.cleanup,
    ),
    cleanup =
      cleanups.length && cleanups.every((c) => c === "SUCCESS")
        ? "完成"
        : cleanups.some((c) => c && c !== "SUCCESS")
          ? "清理异常"
          : "待完成";
  const cancel = () =>
    Modal.confirm({
      className: "rtc-confirm",
      title: "取消本次执行？",
      content: "仅取消本次执行，任务自动调度保持启用。",
      okText: "取消执行",
      cancelText: "继续执行",
      onOk: () =>
        operate(
          "cancel",
          `/api/v1/rtc/runs/${encodeURIComponent(run.run_id)}/cancel`,
        ),
    });
  const logs = parent ? null : (
    <div className="rtc-panel rtc-pad rtc-diagnostics-panel">
      <div className="rtc-section-toolbar">
        <RTCSectionHeading
          title="本次诊断包"
          description="查看诊断状态，按需回捞或下载。"
          icon={<FileZipOutlined />}
        />
        {canDiagnose && (
          <Button
            loading={busy === "logs"}
            disabled={
              !!busy ||
              Object.values(run.logs || {}).some(
                (f) => f.status === "REQUESTED",
              )
            }
            onClick={() =>
              void operate(
                "logs",
                `/api/v1/rtc/runs/${encodeURIComponent(run.run_id)}/logs`,
              ).catch(() => {})
            }
          >
            回捞诊断日志
          </Button>
        )}
      </div>
      {downloadError && <Alert type="error" message={downloadError} />}
      {Object.values(run.logs || {}).length ? (
        Object.values(run.logs || {}).map((file) => (
          <div className="rtc-log-row rtc-log-file" key={file.request_id}>
            <div className="rtc-log-identity">
              <span className="rtc-file-icon">
                <FileZipOutlined aria-hidden />
              </span>
              <div>
                <strong>
                  {file.endpoint_role === "pair"
                    ? "收发合并诊断"
                    : nativeCase?.duplex
                      ? file.endpoint_role === "sender"
                        ? `Native ${nativeCase.sender.transport.toUpperCase()} 诊断`
                        : "Web 诊断"
                      : file.endpoint_role === "sender"
                        ? "发送端诊断（历史记录）"
                        : "接收端诊断（历史记录）"}
                </strong>{" "}
                <State value={file.status} />
                {file.bytes !== undefined && (
                  <span className="rtc-secondary">
                    {(file.bytes / 1048576).toFixed(2)} MiB
                  </span>
                )}
                {file.partial && (
                  <span className="rtc-secondary"> · 过程快照</span>
                )}
                {file.error && (
                  <div className="rtc-secondary">{reasonLabel(file.error)}</div>
                )}
              </div>
            </div>
            {canDiagnose && file.status === "AVAILABLE" && (
              <Button
                icon={<DownloadOutlined aria-hidden />}
                onClick={() => {
                  setDownloadError("");
                  void api
                    .download(
                      `/api/v1/rtc/logs/${encodeURIComponent(file.request_id)}`,
                      `rtc-${run.run_id}${file.endpoint_role === "pair" ? "" : `-${file.endpoint_role}`}.zip`,
                    )
                    .catch((e) =>
                      setDownloadError(
                        e instanceof Error ? e.message : "下载失败",
                      ),
                    );
                }}
              >
                下载诊断包
              </Button>
            )}
          </div>
        ))
      ) : (
        <Empty description="尚无诊断包" />
      )}
    </div>
  );
  const metrics = (
    <div className="rtc-panel rtc-pad rtc-evidence-panel">
      <dl className="rtc-metrics">
        {nativeCase?.duplex ? (
          [sender, receiver].map((endpoint, index) => (
            <div key={index}>
              <dt>{index === 0 ? "Native" : "Web"} 接收证据</dt>
              <dd className="rtc-small-metric">
                {endpoint?.result
                  ? `${(endpoint.result.evidence.media_duration_ms / 1000).toFixed(2)} 秒音频 / ${endpoint.result.evidence.received_video_frames || 0} 帧视频`
                  : "待验证"}
              </dd>
            </div>
          ))
        ) : (
          <>
            <div>
              <dt>有效音频</dt>
              <dd>
                {media
                  ? `${(media.media_duration_ms / 1000).toFixed(2)} 秒`
                  : "—"}
              </dd>
            </div>
            <div>
              <dt>解码视频</dt>
              <dd>{media ? `${media.received_video_frames ?? 0} 帧` : "—"}</dd>
            </div>
            <div>
              <dt>接收分辨率</dt>
              <dd>
                {media?.video_width && media?.video_height
                  ? `${media.video_width} × ${media.video_height}`
                  : "—"}
              </dd>
            </div>
          </>
        )}
        <div>
          <dt>资源清理</dt>
          <dd>
            <span className="rtc-small-metric">{cleanup}</span>
          </dd>
        </div>
        <div>
          <dt>执行时间</dt>
          <dd className="rtc-small-metric">
            <time dateTime={run.created_at}>{time(run.created_at)}</time>
          </dd>
        </div>
      </dl>
    </div>
  );
  const paths = (
    <div className="rtc-two-grid rtc-run-layout">
      <section className="rtc-panel rtc-pad rtc-routing-panel">
        <RTCSectionHeading title="连接与媒体路径" icon={<LinkOutlined />} />
        {[sender, receiver].map((ep, i) => {
          const route = ep?.result?.route,
            browser = ep?.result?.browser;
          return (
            <div key={i} className="rtc-role-path">
              <h4 className="rtc-endpoint-heading">
                <span>{i === 0 ? "A" : "B"}</span>
                {nativeCase?.duplex
                  ? i === 0
                    ? `Native ${nativeCase.sender.transport.toUpperCase()} · 双向收发`
                    : "Web · 双向收发"
                  : i === 0
                    ? "发送端"
                    : "接收端"}
              </h4>
              <Descriptions
                size="small"
                column={1}
                items={[
                  {
                    key: "expected",
                    label: "预期入口",
                    children:
                      route?.expected ||
                      (nativeCase
                        ? nativeCase.case_kind === "AP_JOIN"
                          ? `AP ${nativeCase.ap_entrypoint}`
                          : i === 0
                            ? nativeCase.sender.endpoint
                            : nativeCase.receiver.endpoint
                        : run.edge_endpoint) ||
                      "—",
                  },
                  {
                    key: "actual",
                    label: browser ? "实际 WSS" : "实际连接",
                    children:
                      route?.observed?.map((v) => v.endpoint).join(", ") ||
                      "未记录",
                  },
                  {
                    key: "proof",
                    label: "路径验证",
                    children: route ? (
                      <>
                        <State value={route.status} />
                        {reasonLabel(route.reason)}
                      </>
                    ) : (
                      "未记录"
                    ),
                  },
                  ...(browser
                    ? [
                        {
                          key: "ice",
                          label: "ICE 媒体",
                          children:
                            browser.ice_peers
                              ?.map((v) => `${v.ip}:${v.port} · ${v.protocol}`)
                              .join(", ") || "未协商",
                        },
                        {
                          key: "fallback",
                          label: "443 回退",
                          children: browser.signaling_fallback
                            ? "已使用"
                            : browser.signaling_fallback_attempted
                              ? "已尝试"
                              : "未发生",
                        },
                      ]
                    : []),
                ]}
              />
            </div>
          );
        })}
      </section>
      <section className="rtc-panel rtc-pad rtc-environment-panel">
        <RTCSectionHeading title="执行环境" icon={<CloudServerOutlined />} />
        <Descriptions
          size="small"
          column={1}
          items={[
            {
              key: "probe",
              label: "执行客户端",
              children:
                sender?.job.runtime_location ||
                sender?.job.runtime_id ||
                receiver?.job.runtime_id ||
                "—",
            },
            {
              key: "sdk",
              label: "SDK",
              children:
                sender?.job.sdk_version ||
                receiver?.job.sdk_version ||
                "未记录",
            },
            {
              key: "duration",
              label: "执行耗时",
              children: run.finished_at
                ? `${Math.max(0, (Date.parse(run.finished_at) - Date.parse(run.created_at)) / 1000).toFixed(1)} 秒`
                : "执行中",
            },
          ]}
        />
        <details>
          <summary>技术上下文</summary>
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: "task",
                label: "任务",
                children: task ? taskLabel(task) : run.task_id,
              },
              { key: "id", label: "执行 ID", children: run.run_id },
              {
                key: "revision",
                label: "配置版本",
                children: run.config_revision,
              },
              {
                key: "channel",
                label: "频道",
                children:
                  sender?.job.channel || receiver?.job.channel || "未记录",
              },
            ]}
          />
        </details>
      </section>
    </div>
  );
  const stages = receiver?.result?.stages || sender?.result?.stages || [],
    stageNames: Record<string, string> = {
      INITIALIZE: "初始化",
      BROWSER_SETUP: "浏览器准备",
      JOIN: "加入频道",
      WAIT_START: "等待收发",
      WAIT_PUBLISHER: "等待发布",
      PUBLISH: "发布",
      SUBSCRIBE: "订阅",
      MEDIA: "媒体收发",
      CLEANUP: "清理",
    };
  const results = parent ? (
    <>
      <div className="rtc-panel rtc-pad">
        <dl className="rtc-metrics">
          <div>
            <dt>完成项</dt>
            <dd>
              {
                (run.cases || run.edges || []).filter((r) => terminal(r.status))
                  .length
              }{" "}
              / {(run.cases || run.edges || []).length}
            </dd>
          </div>
          <div>
            <dt>开始时间</dt>
            <dd className="rtc-small-metric">{time(run.created_at)}</dd>
          </div>
          <div>
            <dt>结束时间</dt>
            <dd className="rtc-small-metric">{time(run.finished_at)}</dd>
          </div>
        </dl>
      </div>
      <div className="rtc-panel" id="rtc-validation-paths">
        <div className="rtc-panel-heading">
          <RTCSectionHeading title="验证路径" icon={<LinkOutlined />} />
        </div>
        {run.kind === "NATIVE_COVERAGE" ? (
          <NativeCases run={run} />
        ) : (
          <table className="rtc-table">
            <thead>
              <tr>
                <th scope="col">验证路径</th>
                <th scope="col">本次结果</th>
                <th scope="col">操作</th>
              </tr>
            </thead>
            <tbody>
              {(run.edges || []).map((e) => (
                <tr key={e.endpoint}>
                  <td data-label="验证路径">{e.endpoint}</td>
                  <td data-label="本次结果">
                    <State value={e.status} />
                  </td>
                  <td data-label="操作">
                    <RTCLink
                      href={
                        e.run_id ? rtcRunHref(e.run_id, run.run_id) : undefined
                      }
                      disabled={!e.run_id}
                    >
                      查看
                    </RTCLink>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  ) : (
    <>
      {metrics}
      {paths}
      {!!stages.length && (
        <section className="rtc-panel rtc-pad">
          <RTCSectionHeading title="阶段耗时" icon={<OrderedListOutlined />} />
          <div className="rtc-stage-line">
            {stages.map((stage, i) => (
              <span key={`${stage.stage}-${i}`}>
                {stageNames[stage.stage] || stage.stage}{" "}
                <small>{stage.duration_ms} ms</small>
              </span>
            ))}
          </div>
        </section>
      )}
    </>
  );
  return (
    <>
      <PageHeading
        title={title}
        status={<State value={run.status} />}
        actions={
          <>
            <DetailBack onClick={onBack} />
            {canManage && !terminal(run.status) && (
              <Button danger disabled={!!busy} onClick={cancel}>
                取消本次执行
              </Button>
            )}
          </>
        }
      />
      {["FAILED", "ERROR", "CANCELLED"].includes(run.status) && (
        <Alert
          className="page-alert"
          type={run.status === "CANCELLED" ? "info" : "error"}
          showIcon
          message={reasonLabel(run.reason)}
        />
      )}
      {parent ? (
        results
      ) : (
        <Tabs
          className="rtc-detail-tabs"
          activeKey={tab === "logs" ? "logs" : "result"}
          onChange={onTab}
          items={[
            { key: "result", label: "结果与路径", children: results },
            { key: "logs", label: "诊断日志", children: logs },
          ]}
        />
      )}
    </>
  );
}
