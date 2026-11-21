export const formatTimestamp = (value?: string | null) =>
  value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "暂无";
export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : "请求未完成";
