export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}
export function basic(id: string, secret: string) {
  const bytes = new TextEncoder().encode(`${id}:${secret}`);
  return (
    "Basic " + btoa(Array.from(bytes, (b) => String.fromCharCode(b)).join(""))
  );
}
export function client(authorization: string, onUnauthorized: () => void) {
  const request = async (
    path: string,
    init: RequestInit = {},
    auth = authorization,
  ) => {
    let response: Response;
    try {
      response = await fetch(path, {
        ...init,
        cache: "no-store",
        credentials: "omit",
        headers: { ...init.headers, Authorization: auth },
      });
    } catch {
      throw new APIError("暂时无法连接监控中心", 0, "NETWORK_ERROR");
    }
    if (!response.ok) {
      let data: { message?: string; code?: string } = {};
      try {
        data = await response.json();
      } catch {}
      if (response.status === 401 && auth === authorization) onUnauthorized();
      throw new APIError(
        data.message || `请求失败（${response.status}）`,
        response.status,
        data.code || "HTTP_ERROR",
      );
    }
    return response;
  };
  return {
    get: async <T>(path: string): Promise<T> => {
      return (await request(path)).json();
    },
    post: async <T>(path: string, body: unknown): Promise<T> => {
      return (
        await request(path, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-AVOPS-Request": "1",
          },
          body: JSON.stringify(body),
        })
      ).json();
    },
    accountSession: authorization.startsWith("Bearer "),
    postRestart: async <T>(
      path: string,
      body: unknown,
      keyID: string,
      secret: string,
    ): Promise<T> => {
      const account = authorization.startsWith("Bearer ");
      return (
        await request(
          path,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-AVOPS-Request": "1",
              ...(account
                ? {
                    "X-AVOPS-Restart-Key-ID": keyID,
                    "X-AVOPS-Restart-Key-Secret": secret,
                  }
                : {}),
            },
            body: JSON.stringify(body),
          },
          account ? authorization : basic(keyID, secret),
        )
      ).json();
    },
    download: async (path: string) => {
      const response = await request(path);
      const url = URL.createObjectURL(await response.blob());
      const a = document.createElement("a");
      a.href = url;
      a.download = "services.yaml";
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
  };
}
export type API = ReturnType<typeof client>;
