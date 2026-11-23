import Alert from "antd/es/alert";
import Button from "antd/es/button";
import Skeleton from "antd/es/skeleton";
import { useEffect, useState } from "react";
import type { API } from "../../shared/api";
import type { StartupRecord, StartupReference } from "../../shared/types";
import { monitoringCache } from "./monitoringCache";
import StartupParameters from "./StartupParameters";

export default function ContainerStartupParameters({
  api,
  nodeID,
  reference,
}: {
  api: API;
  nodeID: string;
  reference?: StartupReference;
}) {
  const cache = monitoringCache(api);
  const path =
    reference?.container_id && reference.snapshot_id
      ? `/api/v1/nodes/startup?${new URLSearchParams({ node_id: nodeID, container_id: reference.container_id, snapshot_id: reference.snapshot_id })}`
      : "";
  const [result, setResult] = useState<{
    path: string;
    value: StartupRecord;
  }>();
  const [error, setError] = useState<{ path: string; message: string }>();
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!path || cache.read(path)) return;
    let live = true;
    setError(undefined);
    void cache
      .load<StartupRecord>(path)
      .then((value) => {
        if (live) setResult({ path, value });
      })
      .catch((e) => {
        if (live) setError({ path, message: e.message });
      });
    return () => {
      live = false;
    };
  }, [cache, path, retry]);
  const value =
    cache.read<StartupRecord>(path) ||
    (result?.path === path ? result.value : undefined);
  if (!path) return <StartupParameters data={{ startup: null, stale: true }} />;
  if (error?.path === path)
    return (
      <Alert
        type="warning"
        showIcon
        message={error.message}
        action={<Button onClick={() => setRetry((n) => n + 1)}>重试</Button>}
      />
    );
  if (!value) return <Skeleton active paragraph={{ rows: 5 }} />;
  return (
    <StartupParameters
      data={{
        ...value,
        stale: reference?.stale ?? value.stale,
        reported_at: reference?.reported_at || value.reported_at,
      }}
    />
  );
}
