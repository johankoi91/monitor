import type { API } from "../../shared/api";

type Entry = { value: unknown; at: number; bytes: number };
class MonitoringCache {
  private entries = new Map<string, Entry>();
  private pending = new Map<string, Promise<unknown>>();
  private bytes = 0;
  constructor(private api: API) {}

  read<T>(key: string, maxAge = Infinity): T | undefined {
    const entry = this.entries.get(key);
    if (!entry) return;
    if (Date.now() - entry.at > maxAge) {
      this.remove(key);
      return;
    }
    this.entries.delete(key);
    this.entries.set(key, entry);
    return entry.value as T;
  }
  private remove(key: string) {
    const entry = this.entries.get(key);
    if (entry) this.bytes -= entry.bytes;
    this.entries.delete(key);
  }
  load<T>(path: string): Promise<T> {
    const existing = this.pending.get(path);
    if (existing) return existing as Promise<T>;
    const request = this.api
      .get<T>(path)
      .then((value) => {
        const bytes = JSON.stringify(value).length * 2;
        this.remove(path);
        if (bytes <= 8 * 1048576) {
          this.entries.set(path, { value, at: Date.now(), bytes });
          this.bytes += bytes;
          while (this.entries.size > 32 || this.bytes > 8 * 1048576)
            this.remove(this.entries.keys().next().value!);
        }
        return value;
      })
      .catch((error) => {
        if ([401, 403, 404, 409].includes(error.status)) this.remove(path);
        throw error;
      })
      .finally(() => this.pending.delete(path));
    this.pending.set(path, request);
    return request;
  }
}

// API clients are account/session specific. Weak keys isolate cache contents
// and allow discarded clients and their cached samples to be collected.
const caches = new WeakMap<API, MonitoringCache>();
export function monitoringCache(api: API) {
  let cache = caches.get(api);
  if (!cache) {
    cache = new MonitoringCache(api);
    caches.set(api, cache);
  }
  return cache;
}
