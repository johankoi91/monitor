export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
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
      let data: { message?: string; error?: string; code?: string } = {};
      try {
        data = await response.json();
      } catch {}
      if (response.status === 401 && auth === authorization) {
        onUnauthorized();
        if (data.code === "SESSION_REPLACED")
          window.dispatchEvent(new CustomEvent("session-replaced"));
      }
      throw new APIError(
        data.message || data.error || `请求失败（${response.status}）`,
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
                    "X-AVOPS-Restart-Token": secret,
                  }
                : {}),
            },
            body: JSON.stringify(body),
          },
          account ? authorization : "Bearer " + secret,
        )
      ).json();
    },
    download: async (path: string, filename = "services.yaml") => {
      const response = await request(path);
      const url = URL.createObjectURL(await response.blob());
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
  };
}
export type API = ReturnType<typeof client>;
