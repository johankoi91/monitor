import { useEffect, useMemo, useRef, useState } from "react";
import {
  init,
  use as useECharts,
  connect,
  disconnect,
  type ECharts,
} from "echarts/core";
import { LineChart } from "echarts/charts";
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import Alert from "antd/es/alert";
import Card from "antd/es/card";
import Segmented from "antd/es/segmented";
import Select from "antd/es/select";
import Typography from "antd/es/typography";
import type { API } from "./api";
import type { ResourceMetrics } from "./types";
type DiskDeviceMetrics = NonNullable<ResourceMetrics["disk_devices"]>[number];

useECharts([
  LineChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  CanvasRenderer,
]);
type Sample = {
  at: string;
  container_id: string;
  started_at: string;
  cpu: number | null;
  cpu_user: number | null;
  cpu_system: number | null;
  memory: number | null;
  rss: number | null;
  memory_usage: number | null;
  disk_status: string;
  disks: DiskDeviceMetrics[] | null;
  disk_read: number | null;
  disk_write: number | null;
  disk_read_iops: number | null;
  disk_write_iops: number | null;
  network_scope: string;
  rx: number | null;
  tx: number | null;
};
type History = {
  from: string;
  to: string;
  step_seconds: number;
  retention_days: number;
  samples: Sample[];
};
type Line = { name: string; data: [number, number | null][] };
const numeric = (value: number | null | undefined) =>
  value == null || !Number.isFinite(value) ? null : value;
const number = (v: number | null | undefined, digits = 2) =>
  v == null
    ? "—"
    : v.toLocaleString("zh-CN", { maximumFractionDigits: digits });
function bytes(v: number | null | undefined) {
  if (v == null) return "—";
  for (const [unit, size] of [
    ["GiB", 1024 ** 3],
    ["MiB", 1024 ** 2],
    ["KiB", 1024],
  ] as const)
    if (v >= size) return `${number(v / size)} ${unit}`;
  return `${number(v, 0)} B`;
}
const cores = (v: number | null | undefined) =>
  v == null ? "—" : `${number(v, v < 0.001 ? 6 : 3)} 核`;
const rate = (v: number | null | undefined) =>
  v == null ? "—" : `${bytes(v)}/s`;
const deviceKey = (d: DiskDeviceMetrics) => `${d.major}:${d.minor}`;

function points(
  samples: Sample[],
  step: number,
  read: (s: Sample) => number | null | undefined,
): [number, number | null][] {
  const result: [number, number | null][] = [];
  let previous: Sample | undefined;
  for (const s of samples) {
    const at = Date.parse(s.at);
    if (
      previous &&
      (at - Date.parse(previous.at) > step * 3000 ||
        previous.container_id !== s.container_id ||
        previous.started_at !== s.started_at)
    )
      result.push([at - 1, null]);
    result.push([at, numeric(read(s))]);
    previous = s;
  }
  return result;
}

function TrendChart({
  lines,
  from,
  to,
  group,
  format,
  empty,
}: {
  lines: Line[];
  from: number;
  to: number;
  group: string;
  format: (v: number) => string;
  empty: string;
}) {
  const ref = useRef<HTMLDivElement>(null),
    instance = useRef<ECharts | null>(null);
  useEffect(() => {
    if (!ref.current) return;
    const chart = init(ref.current);
    instance.current = chart;
    chart.group = group;
    connect(group);
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(ref.current);
    return () => {
      observer.disconnect();
      chart.dispose();
      instance.current = null;
    };
  }, [group]);
  useEffect(() => {
    instance.current?.setOption(
      {
        animation: false,
        color: ["#1677ff", "#19a984", "#e9a23b"],
        grid: { left: 14, right: 18, top: 43, bottom: 12, containLabel: true },
        legend: {
          top: 2,
          itemWidth: 14,
          itemHeight: 5,
          textStyle: { fontSize: 11, color: "#66788a" },
        },
        tooltip: {
          trigger: "axis",
          confine: true,
          axisPointer: { type: "line" },
          valueFormatter: (v: unknown) => {
            const value = Array.isArray(v) ? v[v.length - 1] : v;
            return value == null || !Number.isFinite(Number(value))
              ? "—"
              : format(Number(value));
          },
        },
        xAxis: {
          type: "time",
          min: from,
          max: to,
          axisLine: { lineStyle: { color: "#dce3eb" } },
          axisLabel: { fontSize: 10, color: "#8a97a6", hideOverlap: true },
          splitLine: { show: false },
        },
        yAxis: {
          type: "value",
          min: 0,
          splitNumber: 3,
          axisLabel: {
            fontSize: 10,
            color: "#8a97a6",
            formatter: (v: number) => format(v),
          },
          splitLine: { lineStyle: { color: "#edf1f6", type: "dashed" } },
        },
        series: lines.map((line) => ({
          name: line.name,
          type: "line",
          data: line.data,
          connectNulls: false,
          smooth: false,
          showSymbol: true,
          symbolSize: 3,
          lineStyle: { width: 2 },
          emphasis: { focus: "series" },
        })),
      },
      { replaceMerge: ["series"] },
    );
  }, [lines, from, to, format]);
  const hasData = lines.some((line) => line.data.some(([, v]) => v != null));
  return (
    <div className="resource-trend-plot">
      <div
        ref={ref}
        className="resource-trend-canvas"
        role="img"
        aria-label="资源指标时间趋势图"
      />
      {!hasData && <div className="resource-trend-empty">{empty}</div>}
    </div>
  );
}

export default function ResourceTrends({
  api,
  nodeID,
  value,
}: {
  api: API;
  nodeID: string;
  value?: ResourceMetrics | null;
}) {
  const [window, setWindow] = useState("1h"),
    [data, setData] = useState<History | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [device, setDevice] = useState("total"),
    [ioMetric, setIOMetric] = useState("rate");
  const group = `resources:${nodeID}`;
  useEffect(() => () => disconnect(group), [group]);
  useEffect(() => {
    let closed = false,
      running = false;
    setData(null);
    async function load() {
      if (running) return;
      running = true;
      setLoading(true);
      try {
        const value = await api.get<History>(
          `/api/v1/nodes/resource-history?node_id=${encodeURIComponent(nodeID)}&range=${window}`,
        );
        if (!closed) {
          setData(value);
          setError("");
        }
      } catch (e) {
        if (!closed)
          setError(e instanceof Error ? e.message : "历史指标读取失败");
      } finally {
        running = false;
        if (!closed) setLoading(false);
      }
    }
    void load();
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 15000);
    return () => {
      closed = true;
      clearInterval(timer);
    };
  }, [api, nodeID, window]);
  const samples = useMemo(
    () =>
      [...(data?.samples || [])].sort(
        (a, b) => Date.parse(a.at) - Date.parse(b.at),
      ),
    [data],
  );
  const devices = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of samples)
      for (const d of s.disks || [])
        map.set(deviceKey(d), d.device || deviceKey(d));
    for (const d of value?.disk_devices || [])
      map.set(deviceKey(d), d.device || deviceKey(d));
    return [...map].map(([value, label]) => ({ value, label }));
  }, [samples, value?.disk_devices]);
  const chosenDevice = devices.some((d) => d.value === device)
    ? device
    : devices[0]?.value || "total";
  const step = data?.step_seconds || 30;
  const from = data
      ? Date.parse(data.from)
      : Date.now() -
        ({ "15m": 900, "1h": 3600, "6h": 21600, "24h": 86400 }[window] ||
          3600) *
          1000,
    to = data ? Date.parse(data.to) : Date.now();
  const line = (
    name: string,
    read: (s: Sample) => number | null | undefined,
  ): Line => ({ name, data: points(samples, step, read) });
  const cpu = [
    line("总使用", (s) => s.cpu),
    line("用户态", (s) => s.cpu_user),
    line("内核态", (s) => s.cpu_system),
  ];
  const memory = [
    line("工作集", (s) => s.memory),
    line("RSS", (s) => s.rss),
    line("总使用", (s) => s.memory_usage),
  ];
  function disk(s: Sample, write: boolean) {
    if (s.disk_status !== "AVAILABLE") return null;
    if (chosenDevice === "total")
      return ioMetric === "rate"
        ? write
          ? s.disk_write
          : s.disk_read
        : write
          ? s.disk_write_iops
          : s.disk_read_iops;
    const d = s.disks?.find((d) => deviceKey(d) === chosenDevice);
    return ioMetric === "rate"
      ? write
        ? d?.write_bytes_per_second
        : d?.read_bytes_per_second
      : write
        ? d?.write_operations_per_second
        : d?.read_operations_per_second;
  }
  const io = [
    line("读取", (s) => disk(s, false)),
    line("写入", (s) => disk(s, true)),
  ];
  const network = [
    line("接收", (s) => (s.network_scope === "CONTAINER" ? s.rx : null)),
    line("发送", (s) => (s.network_scope === "CONTAINER" ? s.tx : null)),
  ];
  const current =
    value && !value.stale && value.status !== "UNAVAILABLE" ? value : undefined;
  const netShared = current?.network_scope === "HOST_SHARED";
  const empty = !samples.length ? "历史采样正在积累" : "该时段无有效采样";
  const ioFormat = (v: number) =>
    ioMetric === "rate" ? rate(v) : `${number(v)} IOPS`;
  return (
    <div className="resource-trends">
      <div className="resource-trends-toolbar">
        <Segmented
          aria-label="历史时间范围"
          size="small"
          options={[
            { value: "15m", label: "15 分钟" },
            { value: "1h", label: "1 小时" },
            { value: "6h", label: "6 小时" },
            { value: "24h", label: "24 小时" },
          ]}
          value={window}
          onChange={(v) => setWindow(String(v))}
        />
        <Typography.Text type="secondary">
          {loading
            ? "更新中…"
            : `显示间隔 ${step < 60 ? step + "秒" : step / 60 + "分钟"}`}
        </Typography.Text>
      </div>
      {error && (
        <Alert
          type="warning"
          message={error}
          showIcon
          style={{ marginBottom: 12 }}
        />
      )}
      <div className="resource-trend-grid">
        <Card
          size="small"
          title="CPU"
          extra={
            <Typography.Text type="secondary">
              当前 {cores(current?.cpu_usage_cores)}
            </Typography.Text>
          }
        >
          <TrendChart
            lines={cpu}
            from={from}
            to={to}
            group={group}
            format={cores}
            empty={empty}
          />
          <p className="resource-trend-note">1 核表示一个逻辑核</p>
        </Card>
        <Card
          size="small"
          title="内存"
          extra={
            <Typography.Text type="secondary">
              工作集 {bytes(current?.memory_working_set_bytes)}
            </Typography.Text>
          }
        >
          <TrendChart
            lines={memory}
            from={from}
            to={to}
            group={group}
            format={bytes}
            empty={empty}
          />
          <p className="resource-trend-note">工作集、RSS 和总使用分别展示</p>
        </Card>
        <Card size="small" title="磁盘 I/O">
          <div className="resource-trend-controls">
            <Select
              aria-label="I/O 指标"
              size="small"
              value={ioMetric}
              onChange={setIOMetric}
              options={[
                { value: "rate", label: "读写速率" },
                { value: "iops", label: "IOPS" },
              ]}
            />
            {!!devices.length && (
              <Select
                aria-label="磁盘设备"
                size="small"
                value={chosenDevice}
                onChange={setDevice}
                options={devices}
              />
            )}
          </div>
          <TrendChart
            lines={io}
            from={from}
            to={to}
            group={group}
            format={ioFormat}
            empty={
              value?.disk_io_status === "UNSUPPORTED"
                ? "当前环境不支持 I/O 采集"
                : empty
            }
          />
          <p className="resource-trend-note">
            按设备查看，逻辑盘和底层设备不重复累加
          </p>
        </Card>
        <Card
          size="small"
          title="网络"
          extra={
            netShared ? (
              <Typography.Text type="secondary">共享主机网络</Typography.Text>
            ) : null
          }
        >
          <TrendChart
            lines={network}
            from={from}
            to={to}
            group={group}
            format={rate}
            empty={netShared ? "共享网络无法归属到单容器" : empty}
          />
          <p className="resource-trend-note">共享网络时段不展示单容器流量</p>
        </Card>
      </div>
      <Typography.Paragraph
        type="secondary"
        className="resource-trend-footnote"
      >
        历史保留 {data?.retention_days || 7}{" "}
        天；缺失采样、容器重建和重启处断开曲线，缺失值不计为 0。
      </Typography.Paragraph>
    </div>
  );
}
