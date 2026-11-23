import { CloudServerOutlined, ReloadOutlined } from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Empty from "antd/es/empty";
import Form from "antd/es/form";
import Select from "antd/es/select";
import { useCallback, useEffect, useRef, useState } from "react";
import type { API } from "../../shared/api";
import type { Agent } from "../../shared/types";
import LogCollectionPanel from "./LogCollectionPanel";
import "./LogCollection.css";

export default function LogCollection({ api }: { api: API }) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [agentID, setAgentID] = useState(
    () => new URLSearchParams(window.location.search).get("agent") || "",
  );
  const live = useRef(true);
  const inFlight = useRef(false);
  const load = useCallback(
    async (showLoading = false) => {
      if (inFlight.current) return;
      inFlight.current = true;
      if (showLoading) setLoading(true);
      try {
        const value = await api.get<{ agents: Agent[] }>("/api/v1/agents");
        if (!live.current) return;
        const hosts = value.agents.sort((a, b) =>
          a.host_address.localeCompare(b.host_address),
        );
        setAgents(hosts);
        setAgentID((previous) =>
          hosts.some((a) => a.agent_id === previous)
            ? previous
            : (hosts.find((a) => a.agent_online && !a.stale) || hosts[0])
                ?.agent_id || "",
        );
        setError("");
      } catch (e) {
        if (live.current) setError((e as Error).message);
      } finally {
        inFlight.current = false;
        if (live.current && showLoading) setLoading(false);
      }
    },
    [api],
  );
  useEffect(() => {
    live.current = true;
    void load(true);
    const timer = setInterval(() => {
      if (!document.hidden) void load();
    }, 15000);
    const restore = () =>
      setAgentID(
        new URLSearchParams(window.location.search).get("agent") || "",
      );
    window.addEventListener("popstate", restore);
    return () => {
      live.current = false;
      clearInterval(timer);
      window.removeEventListener("popstate", restore);
    };
  }, [load]);
  function select(id: string) {
    setAgentID(id);
    const url = new URL(window.location.href);
    url.searchParams.set("agent", id);
    window.history.pushState(
      null,
      "",
      `${url.pathname}${url.search}${url.hash}`,
    );
  }
  const selected = agents.find((a) => a.agent_id === agentID);
  const online = !!selected?.agent_online && !selected.stale && !error;
  const hostSelection = (
    <Form layout="vertical" component={false}>
      <Form.Item label="主机 IP" htmlFor="log-host" style={{ marginBottom: 0 }}>
        <Select
          id="log-host"
          showSearch
          optionFilterProp="children"
          value={selected?.agent_id}
          placeholder={loading ? "正在读取主机…" : "选择需要拉取日志的主机"}
          loading={loading}
          onChange={select}
          notFoundContent={
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="暂无 Agent 主机"
            />
          }
        >
          {agents.map((a) => (
            <Select.Option key={a.agent_id} value={a.agent_id}>
              {a.host_address}
            </Select.Option>
          ))}
        </Select>
      </Form.Item>
    </Form>
  );
  return (
    <section className="log-collection-page">
      <div className="log-page-toolbar">
        <span className="log-host-count">
          <CloudServerOutlined aria-hidden />
          {error
            ? "主机状态待确认"
            : loading && !agents.length
              ? "正在读取主机"
              : `${agents.length} 台主机 · ${agents.filter((a) => a.agent_online && !a.stale).length} 台在线`}
        </span>
        <Button
          icon={<ReloadOutlined aria-hidden />}
          loading={loading}
          onClick={() => void load(true)}
        >
          刷新主机
        </Button>
      </div>
      {error && (
        <Alert
          type="error"
          showIcon
          message={error}
          description="主机状态暂不可确认，请刷新后重试。"
        />
      )}
      {!selected && (
        <Card
          title={
            <>
              <CloudServerOutlined aria-hidden /> 选择主机
            </>
          }
        >
          {hostSelection}
          {!loading && !error && !agents.length && (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="暂无 Agent 主机，请先接入需要采集日志的机器。"
            />
          )}
        </Card>
      )}
      {selected && (
        <LogCollectionPanel
          key={selected.agent_id}
          api={api}
          agentID={selected.agent_id}
          hostAddress={selected.host_address}
          agentOnline={online}
          hostSelection={hostSelection}
        />
      )}
    </section>
  );
}
