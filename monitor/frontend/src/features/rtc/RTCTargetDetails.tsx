import {
  ApiOutlined,
  ArrowRightOutlined,
  CodeOutlined,
  DesktopOutlined,
  LinkOutlined,
  SafetyCertificateOutlined,
} from "@ant-design/icons";
import Button from "antd/es/button";
import Empty from "antd/es/empty";
import Tag from "antd/es/tag";
import type { ReactNode } from "react";
import { type Target, type Task, clientLabel, taskLabel } from "./RTCModel";
import type { NativeCoverageConfig } from "./RTCSettings";
import { DetailBack, PageHeading } from "./RTCUI";

function Fact({
  label,
  children,
  mono = false,
}: {
  label: string;
  children: ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="rtc-target-fact">
      <dt>{label}</dt>
      <dd className={mono ? "rtc-target-mono" : undefined}>{children}</dd>
    </div>
  );
}

function Coverage({
  config,
  fallback,
}: {
  config?: NativeCoverageConfig | null;
  fallback: ReactNode;
}) {
  if (!config) return <div className="rtc-target-default">{fallback}</div>;
  return (
    <>
      <dl className="rtc-target-coverage-facts">
        <Fact label="覆盖方式">自动发现</Fact>
        <Fact label="跨 Edge 互通">
          {config.cross_edge ? "动态有向环" : "未配置"}
        </Fact>
      </dl>
      <div className="rtc-target-ap-list">
        {config.groups.length ? (
          config.groups.map((group, index) => (
            <div
              className="rtc-target-ap"
              key={`${group.agent_id || group.group}-${index}`}
            >
              <div className="rtc-target-ap-label">
                <span>AP 分组</span>
                <code>{group.group || "待发现"}</code>
              </div>
              <div className="rtc-target-ap-entries">
                <span>客户端入口</span>
                <div>
                  {group.entrypoints.length ? (
                    group.entrypoints.map((entry, index) => (
                      <code key={`${entry}-${index}`}>{entry}</code>
                    ))
                  ) : (
                    <span>等待发现入口</span>
                  )}
                </div>
              </div>
            </div>
          ))
        ) : (
          <p className="rtc-target-muted">尚无 AP 分组，请核对接入配置。</p>
        )}
      </div>
    </>
  );
}

export default function RTCTargetDetails({
  target,
  tasks,
  canManage,
  onEdit,
  onOpenTask,
  onBack,
}: {
  target: Target;
  tasks: Task[];
  canManage: boolean;
  onEdit: () => void;
  onOpenTask: (id: string) => void;
  onBack: () => void;
}) {
  const rtc = target.rtc;
  const linked = tasks.filter((task) => task.target_id === target.target_id);
  const web = rtc?.web_access;
  const certificateMode = rtc?.auth_mode && rtc.auth_mode !== "none";
  const authLabel = !rtc?.auth_mode
    ? "待配置"
    : rtc.auth_mode === "none"
      ? "未开启鉴权"
      : "App Certificate";
  return (
    <section className="rtc-target-details" aria-label="目标服务接入详情">
      <PageHeading
        title={target.name}
        actions={
          <>
            <DetailBack onClick={onBack} />
            {canManage && (
              <Button type="primary" onClick={onEdit}>
                编辑接入配置
              </Button>
            )}
          </>
        }
      />

      <section
        className="rtc-panel rtc-target-overview"
        aria-labelledby="rtc-target-overview-title"
      >
        <div className="rtc-target-overview-heading">
          <div className="rtc-target-section-title">
            <span className="rtc-target-icon">
              <SafetyCertificateOutlined aria-hidden />
            </span>
            <div>
              <h3 id="rtc-target-overview-title">基础与鉴权</h3>
              <p>共享接入身份，客户端覆盖策略分别管理。</p>
            </div>
          </div>
          <Tag>
            {rtc
              ? rtc.private_deployment
                ? "私有化接入"
                : "公共接入"
              : "接入待配置"}
          </Tag>
        </div>
        <dl className="rtc-target-identity-grid">
          <Fact label="AppID" mono>
            {rtc?.app_id || "未配置"}
          </Fact>
          <Fact label="鉴权方式">
            <span className="rtc-target-auth">
              {authLabel}
              {certificateMode && (
                <Tag color={target.has_certificate ? "success" : "warning"}>
                  {target.has_certificate ? "已配置" : "未配置"}
                </Tag>
              )}
            </span>
          </Fact>
          <Fact label="关联集群" mono>
            {target.cluster_code || "未绑定"}
          </Fact>
        </dl>
        <div className="rtc-target-id">
          <span>目标标识</span>
          <code>{target.target_id}</code>
        </div>
      </section>

      <div className="rtc-target-section-heading">
        <h3 id="rtc-target-clients-title">客户端接入与覆盖</h3>
        <span>配置概览 · 业务结果见关联任务</span>
      </div>
      <div
        className="rtc-target-clients"
        aria-labelledby="rtc-target-clients-title"
      >
        <section
          className="rtc-panel rtc-target-client"
          aria-labelledby="rtc-target-native-title"
        >
          <div className="rtc-target-client-heading">
            <div className="rtc-target-section-title">
              <span className="rtc-target-icon">
                <CodeOutlined aria-hidden />
              </span>
              <div>
                <h4 id="rtc-target-native-title">Native</h4>
                <p>Go Linux SDK</p>
              </div>
            </div>
            <Tag>
              {rtc?.native_coverage
                ? "自动发现"
                : rtc?.media_edges?.enabled
                  ? "手动端口范围"
                  : "默认接入"}
            </Tag>
          </div>
          <div className="rtc-target-client-body">
            <Coverage
              config={rtc?.native_coverage}
              fallback={
                <>
                  <ApiOutlined aria-hidden />
                  <div>
                    <strong>
                      {rtc?.media_edges?.enabled ? "指定媒体端口" : "默认接入"}
                    </strong>
                    <p>
                      {rtc?.media_edges?.enabled ? (
                        <code>
                          {rtc.media_edges.host}:{rtc.media_edges.start_port}–
                          {rtc.media_edges.end_port}
                        </code>
                      ) : (
                        "按目标服务的现有接入配置执行。"
                      )}
                    </p>
                  </div>
                </>
              }
            />
          </div>
        </section>

        <section
          className="rtc-panel rtc-target-client"
          aria-labelledby="rtc-target-web-title"
        >
          <div className="rtc-target-client-heading">
            <div className="rtc-target-section-title">
              <span className="rtc-target-icon">
                <DesktopOutlined aria-hidden />
              </span>
              <div>
                <h4 id="rtc-target-web-title">Web</h4>
                <p>无头浏览器</p>
              </div>
            </div>
            <Tag color={web ? "blue" : undefined}>
              {web ? "已配置" : "未配置"}
            </Tag>
          </div>
          <div className="rtc-target-client-body">
            {web ? (
              <>
                <Coverage
                  config={web.coverage}
                  fallback={
                    <>
                      <ApiOutlined aria-hidden />
                      <div>
                        <strong>默认接入</strong>
                        <p>尚未配置自动发现覆盖范围。</p>
                      </div>
                    </>
                  }
                />
                <div className="rtc-target-web-settings">
                  <dl className="rtc-target-port-grid">
                    <Fact label="AP HTTPS 端口" mono>
                      {web.port ?? "—"}
                    </Fact>
                    <Fact label="事件上报端口" mono>
                      {web.report_port ?? "—"}
                    </Fact>
                    <Fact label="SDK 日志端口" mono>
                      {web.log_port ?? "—"}
                    </Fact>
                  </dl>
                  <div className="rtc-target-certificate">
                    <span>浏览器证书</span>
                    <Tag
                      color={web.ignore_certificates ? "warning" : undefined}
                    >
                      {web.ignore_certificates ? "忽略校验" : "正常校验"}
                    </Tag>
                  </div>
                </div>
              </>
            ) : (
              <div className="rtc-target-unconfigured">
                <DesktopOutlined aria-hidden />
                <strong>尚未配置 Web 接入</strong>
                <p>配置后可在此查看 AP 覆盖和浏览器接入参数。</p>
              </div>
            )}
          </div>
        </section>
      </div>

      <section
        className="rtc-panel rtc-target-tasks"
        aria-labelledby="rtc-target-tasks-title"
      >
        <div className="rtc-target-tasks-heading">
          <div className="rtc-target-section-title">
            <LinkOutlined aria-hidden />
            <h3 id="rtc-target-tasks-title">关联任务</h3>
            <span className="rtc-target-task-count">{linked.length} 个</span>
          </div>
          <span>查看各任务的调度状态与探测结果</span>
        </div>
        {linked.length ? (
          <ul className="rtc-target-task-list">
            {linked.map((task) => (
              <li key={task.task_id}>
                <div className="rtc-target-task-name">
                  <Button type="link" onClick={() => onOpenTask(task.task_id)}>
                    {taskLabel(task)}
                  </Button>
                  <code>{task.task_id}</code>
                </div>
                <div className="rtc-target-task-meta">
                  <Tag>{clientLabel(task)}</Tag>
                  <span className="rtc-target-task-state">
                    <span
                      className={`rtc-target-state-dot${task.enabled ? " is-enabled" : ""}`}
                    />
                    {task.enabled ? "已启用" : "已停用"}
                  </span>
                </div>
                <Button
                  type="text"
                  className="rtc-target-task-open"
                  onClick={() => onOpenTask(task.task_id)}
                  aria-label={`查看任务 ${taskLabel(task)}`}
                >
                  查看任务 <ArrowRightOutlined aria-hidden />
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <div className="rtc-target-empty">
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="暂无关联任务"
            />
          </div>
        )}
      </section>
    </section>
  );
}
