import {
  CloseOutlined,
  ContainerOutlined,
  DownOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Checkbox from "antd/es/checkbox";
import Drawer from "antd/es/drawer";
import Empty from "antd/es/empty";
import Input, { type InputRef } from "antd/es/input";
import { useRef, useState } from "react";

export type LogService = {
  container_id: string;
  container_name: string;
  processes: { pid: number; name: string }[];
  available: boolean;
  reason?: string;
};

const groupName = (name: string) =>
  name.includes("udp_media_edge")
    ? "UDP Edge"
    : name.includes("aut_media_edge")
      ? "AUT Edge"
      : name.includes("web_media_edge")
        ? "Web Edge"
        : name.includes("local_ap")
          ? "AP 接入"
          : "其他服务";
const groups = ["AP 接入", "UDP Edge", "AUT Edge", "Web Edge", "其他服务"];

export default function LogServicePicker({
  services,
  value,
  onChange,
  disabled,
}: {
  services: LogService[];
  value: string[];
  onChange: (ids: string[]) => void;
  disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [selectedOnly, setSelectedOnly] = useState(false);
  const input = useRef<InputRef>(null);
  const available = services.filter((service) => service.available);
  const selected = new Set(draft);
  const invalid = draft.filter(
    (id) => !available.some((service) => service.container_id === id),
  );
  const visible = available.filter(
    (service) =>
      service.container_name
        .toLowerCase()
        .includes(search.trim().toLowerCase()) &&
      (!selectedOnly || selected.has(service.container_id)),
  );

  function show(only = false) {
    setDraft([...value]);
    setSearch("");
    setSelectedOnly(only);
    setOpen(true);
  }
  function toggle(ids: string[], checked: boolean) {
    setDraft((previous) =>
      checked
        ? [...new Set([...previous, ...ids])]
        : previous.filter((id) => !ids.includes(id)),
    );
  }
  function selection(ids: string[]) {
    const count = ids.filter((id) => selected.has(id)).length;
    return {
      checked: ids.length > 0 && count === ids.length,
      indeterminate: count > 0 && count < ids.length,
    };
  }

  return (
    <div className="log-service-picker">
      <Button
        id="log-service"
        className="log-picker-trigger"
        disabled={disabled}
        onClick={() => show()}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <ContainerOutlined aria-hidden />
        <span>
          {value.length ? `已选 ${value.length} 个服务容器` : "选择服务容器"}
        </span>
        <DownOutlined aria-hidden />
      </Button>
      {value.length > 0 && (
        <div className="log-selected-preview" aria-label="已选服务容器">
          {value.slice(0, 3).map((id) => {
            const service = services.find((item) => item.container_id === id);
            const name = service?.container_name || "已失效容器";
            return (
              <span
                className={`log-selected-tag${service?.available ? "" : " is-invalid"}`}
                key={id}
              >
                <span>{name}</span>
                <Button
                  type="text"
                  size="small"
                  disabled={disabled}
                  icon={<CloseOutlined aria-hidden />}
                  aria-label={`移除 ${name}`}
                  onClick={() => onChange(value.filter((item) => item !== id))}
                />
              </span>
            );
          })}
          {value.length > 3 && (
            <Button
              type="link"
              size="small"
              onClick={() => show(true)}
              disabled={disabled}
            >
              查看全部 {value.length} 个
            </Button>
          )}
        </div>
      )}
      <Drawer
        title="选择服务容器"
        open={open}
        onClose={() => setOpen(false)}
        width={540}
        rootClassName="log-service-drawer"
        destroyOnHidden
        afterOpenChange={(isOpen) => {
          if (isOpen) input.current?.focus();
        }}
        footer={
          <div className="log-picker-footer">
            <span role="status" aria-live="polite" aria-atomic="true">
              已选 <strong>{draft.length}</strong> 个容器
            </span>
            <Button onClick={() => setOpen(false)}>取消</Button>
            <Button
              type="primary"
              disabled={disabled || draft.length === 0 || invalid.length > 0}
              onClick={() => {
                onChange(draft);
                setOpen(false);
              }}
            >
              确认选择
            </Button>
          </div>
        }
      >
        <div className="log-picker-tools">
          <Input
            ref={input}
            prefix={<SearchOutlined aria-hidden />}
            aria-label="搜索服务容器"
            placeholder="搜索容器名称"
            allowClear
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <div className="log-picker-filter">
            <Checkbox
              checked={selectedOnly}
              onChange={(e) => setSelectedOnly(e.target.checked)}
            >
              只看已选
            </Checkbox>
            <span>{visible.length} 个匹配容器</span>
          </div>
          {invalid.length > 0 && (
            <Alert
              showIcon
              type="warning"
              message={`${invalid.length} 个已选容器已失效`}
              action={
                <Button
                  size="small"
                  onClick={() =>
                    setDraft((previous) =>
                      previous.filter((id) => !invalid.includes(id)),
                    )
                  }
                >
                  移除失效项
                </Button>
              }
            />
          )}
          <div className="log-picker-bulk">
            <Checkbox
              {...selection(visible.map((service) => service.container_id))}
              disabled={disabled || visible.length === 0}
              onChange={(e) =>
                toggle(
                  visible.map((service) => service.container_id),
                  e.target.checked,
                )
              }
            >
              全选当前结果
            </Checkbox>
            <Button
              type="text"
              size="small"
              disabled={disabled || draft.length === 0}
              onClick={() => setDraft([])}
            >
              清空已选
            </Button>
          </div>
        </div>
        <div className="log-picker-list">
          {visible.length === 0 && (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                selectedOnly
                  ? "当前筛选下没有已选容器"
                  : search
                    ? "没有匹配的容器，试试其他名称"
                    : "暂无可拉取日志的服务容器"
              }
            />
          )}
          {groups.map((group) => {
            const members = visible.filter(
              (service) => groupName(service.container_name) === group,
            );
            if (!members.length) return null;
            const ids = members.map((service) => service.container_id);
            return (
              <section
                className="log-picker-group"
                key={group}
                aria-label={group}
              >
                <div className="log-picker-group-heading">
                  <Checkbox
                    {...selection(ids)}
                    disabled={disabled}
                    onChange={(e) => toggle(ids, e.target.checked)}
                  >
                    {group}
                  </Checkbox>
                  <span>{members.length}</span>
                </div>
                {members.map((service) => (
                  <Checkbox
                    key={service.container_id}
                    className={`log-picker-row${selected.has(service.container_id) ? " is-selected" : ""}`}
                    checked={selected.has(service.container_id)}
                    disabled={disabled}
                    onChange={(e) =>
                      toggle([service.container_id], e.target.checked)
                    }
                  >
                    {service.container_name}
                  </Checkbox>
                ))}
              </section>
            );
          })}
        </div>
      </Drawer>
    </div>
  );
}
