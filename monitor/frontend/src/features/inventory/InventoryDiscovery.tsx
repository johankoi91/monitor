import {
  CheckCircleOutlined,
  CloudServerOutlined,
  FilterOutlined,
  SearchOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Card from "antd/es/card";
import Form from "antd/es/form";
import Input from "antd/es/input";
import Pagination from "antd/es/pagination";
import Select from "antd/es/select";
import Table from "antd/es/table";
import Tag from "antd/es/tag";
import type { Dispatch, SetStateAction } from "react";
import { formatTimestamp as stamp } from "../../shared/format";
import type { Addition, Agent, Container } from "../../shared/types";
import Badge from "../../shared/ui/StatusBadge";
import { InventoryEmpty, InventoryIdentity } from "./InventoryVisual";
const key = (c: Container) => `${c.agent_id}/${c.container_id}`;

interface InventoryFilters {
  agent_id: string;
  runtime_status: string;
  name: string;
}

interface InventoryDiscoveryProps {
  setPage: Dispatch<SetStateAction<number>>;
  setQuery: Dispatch<SetStateAction<InventoryFilters>>;
  filters: InventoryFilters;
  setFilters: Dispatch<SetStateAction<InventoryFilters>>;
  agents: Agent[];
  inventoryPane: string;
  chosen: Map<string, Container>;
  saving: boolean;
  setChosen: Dispatch<SetStateAction<Map<string, Container>>>;
  can: (p: string) => boolean;
  setAssignmentOpen: Dispatch<SetStateAction<boolean>>;
  additions: Map<string, Addition>;
  removals: Set<string>;
  containers: Container[];
  eligible: (c: Container) => boolean;
  select: (c: Container, checked: boolean) => void;
  detailPending: string;
  showDetail: (c: Container) => Promise<void>;
  page: number;
  total: number;
  notice: string;
  noticeType: "info" | "error";
  assignmentOpen: boolean;
}

export default function InventoryDiscovery({
  setPage,
  setQuery,
  filters,
  setFilters,
  agents,
  inventoryPane,
  chosen,
  saving,
  setChosen,
  can,
  setAssignmentOpen,
  additions,
  removals,
  containers,
  eligible,
  select,
  detailPending,
  showDetail,
  page,
  total,
  notice,
  noticeType,
  assignmentOpen,
}: InventoryDiscoveryProps) {
  return (
    <section
      className="inventory-screen inventory-suite"
      aria-label="部署服务容器发现工作区"
    >
      <div className="inventory-layout inventory-contextual-layout">
        <Card className="inventory-main">
          <>
            <div className="inventory-filter-caption">
              <FilterOutlined aria-hidden />
              <span>筛选发现范围</span>
            </div>
            <Form
              layout="vertical"
              className="filters inventory-filters"
              onSubmitCapture={(e) => {
                e.preventDefault();
                setPage(1);
                setQuery({
                  ...filters,
                  name: filters.name.trim(),
                });
              }}
            >
              <Form.Item
                label="容器名关键词"
                htmlFor="inventory-container-name"
              >
                <Input
                  id="inventory-container-name"
                  placeholder="输入容器名关键词，如 media_edge"
                  prefix={<SearchOutlined aria-hidden="true" />}
                  allowClear
                  value={filters.name}
                  onChange={(event) =>
                    setFilters({
                      ...filters,
                      name: event.target.value,
                    })
                  }
                />
              </Form.Item>
              <Form.Item label="主机 IP" htmlFor="app-field-3">
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
                  <Select.Option value="">全部机器</Select.Option>
                  {agents.map((a) => (
                    <Select.Option key={a.agent_id} value={a.agent_id}>
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
              <Button htmlType="submit" icon={<FilterOutlined aria-hidden />}>
                筛选
              </Button>
            </Form>
            {inventoryPane === "discovery" &&
              (chosen.size > 0 ? (
                <div className="inventory-selection-bar">
                  <div
                    className="inventory-selection-summary"
                    role="status"
                    aria-live="polite"
                  >
                    <span className="inventory-selection-check" aria-hidden>
                      <CheckCircleOutlined />
                    </span>
                    <div>
                      <strong>已选 {chosen.size} 个容器</strong>
                      <span>跨筛选与分页保留，统一设置后加入变更</span>
                    </div>
                  </div>
                  <div className="inventory-selection-actions">
                    <Button
                      type="text"
                      disabled={saving}
                      onClick={() => setChosen(new Map())}
                    >
                      取消勾选
                    </Button>
                    <Button
                      type="primary"
                      icon={<SettingOutlined aria-hidden />}
                      disabled={!can("baseline.write") || saving}
                      onClick={() => setAssignmentOpen(true)}
                    >
                      设置纳管规则
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="inventory-selection-idle">
                  <CheckCircleOutlined aria-hidden />
                  <span>
                    {additions.size + removals.size > 0
                      ? "可继续选择容器，变更将在下方统一核对和保存。"
                      : "勾选容器后，可批量设置分组与检查规则。"}
                  </span>
                </div>
              ))}
            <Table<Container>
              className="inventory-table inventory-discovery-table"
              rowClassName={(c) =>
                chosen.has(key(c)) ? "inventory-row-picked" : ""
              }
              rowKey={key}
              dataSource={containers}
              locale={{
                emptyText: (
                  <InventoryEmpty
                    title="暂无符合筛选条件的未纳管容器"
                    description="可以调整容器名关键词、主机或运行态后重新筛选，已勾选的容器会继续保留。"
                  />
                ),
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
                  disabled: saving || (!eligible(c) && !chosen.has(key(c))),
                }),
                onSelect: (c, checked) => select(c, checked),
                onSelectAll: (checked, _selected, changed) =>
                  changed.forEach((c) => select(c, checked)),
              }}
              columns={[
                {
                  title: "容器 / 镜像",
                  key: "container",
                  width: 260,
                  ellipsis: true,
                  render: (_, c) => (
                    <InventoryIdentity
                      name={c.container_name}
                      image={c.image}
                    />
                  ),
                },
                {
                  title: "主机 IP",
                  dataIndex: "host_address",
                  width: 150,
                  render: (value) => (
                    <span className="inventory-host">
                      <CloudServerOutlined aria-hidden />
                      <code>{value}</code>
                    </span>
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
                          )[c.runtime_status] || c.runtime_status
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
                      loading={detailPending === key(c)}
                      onClick={() => void showDetail(c)}
                    >
                      详情
                    </Button>
                  ),
                },
              ]}
            />
            <div className="table-footer inventory-discovery-footer">
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
        </Card>
        {notice && noticeType === "error" && !assignmentOpen && (
          <Alert
            className="inventory-inline-error"
            type="error"
            message={notice}
            showIcon
          />
        )}
      </div>
    </section>
  );
}
