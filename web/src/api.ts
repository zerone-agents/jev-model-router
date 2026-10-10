export type CallEnvelope = {
  input: unknown;
  expected_version?: number;
  idempotency_key?: string;
};
export type Capability = {
  id: string;
  availability: string;
  call?: { protocol?: { playground?: unknown } };
  input_schema: Record<string, unknown>;
};
export type Reply<T> = {
  ok: boolean;
  data: T;
  meta: { operation_id?: string };
  error?: { code: string };
};
export class APIError extends Error {
  constructor(
    public code: string,
    public status = 0,
    public operationId?: string,
  ) {
    super(code);
  }
}
export function secureOrigin(url: URL) {
  return (
    url.protocol === "https:" ||
    (url.protocol === "http:" &&
      ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname))
  );
}
export type SessionInfo = { expires_at: string; csrf_token: string };
async function request<T>(
  fetcher: typeof fetch,
  path: string,
  init: RequestInit,
): Promise<Reply<T>> {
  let response: Response;
  try {
    response = await fetcher(path, {
      ...init,
      credentials: "same-origin",
      redirect: "error",
      cache: "no-store",
    });
  } catch {
    throw new APIError("network_error");
  }
  let value: Reply<T>;
  try {
    value = await response.json();
  } catch {
    throw new APIError("invalid_response", response.status);
  }
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    typeof value.ok !== "boolean" ||
    value.ok !== response.ok ||
    (value.ok && (value.data === null || value.data === undefined)) ||
    (!value.ok && typeof value.error?.code !== "string")
  )
    throw new APIError("invalid_response", response.status);
  if (!value.ok)
    throw new APIError(
      value.error!.code,
      response.status,
      typeof value.meta?.operation_id === "string"
        ? value.meta.operation_id
        : undefined,
    );
  return value;
}
export function createSessionAPI(fetcher: typeof fetch = fetch) {
  async function auth(
    path: string,
    headers: Record<string, string>,
    post = false,
  ) {
    const reply = await request<SessionInfo>(fetcher, path, {
      method: post ? "POST" : "GET",
      headers,
      body: post ? "{}" : undefined,
      signal: AbortSignal.timeout(15000),
    });
    if (
      !reply.data ||
      typeof reply.data.csrf_token !== "string" ||
      !reply.data.csrf_token ||
      typeof reply.data.expires_at !== "string" ||
      !Number.isFinite(Date.parse(reply.data.expires_at))
    )
      throw new APIError("invalid_response");
    return reply.data;
  }
  return {
    login: (token: string) =>
      auth(
        "/admin/v1/session/login",
        {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        true,
      ),
    status: () => auth("/admin/v1/session", { "X-Jev-Session": "1" }),
    logout: (csrf: string) =>
      request<{ revoked: boolean }>(fetcher, "/admin/v1/session/logout", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: "{}",
        signal: AbortSignal.timeout(15000),
      }),
  };
}
export function createManagementClient(
  csrf: string,
  fetchImpl: typeof fetch = fetch,
) {
  const controller = new AbortController();
  const call = <T>(path: string, body?: CallEnvelope, signal?: AbortSignal) =>
    request<T>(fetchImpl, path, {
      method: body ? "POST" : "GET",
      headers: {
        "X-Jev-Session": "1",
        ...(body
          ? { "Content-Type": "application/json", "X-CSRF-Token": csrf }
          : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.any([controller.signal, ...(signal ? [signal] : [])]),
    });
  return {
    schema: (signal?: AbortSignal) =>
      call<Capability[]>(
        "/admin/v1/schema",
        undefined,
        AbortSignal.any([
          AbortSignal.timeout(15000),
          ...(signal ? [signal] : []),
        ]),
      ),
    call: <T>(
      capability: string,
      envelope: CallEnvelope,
      signal?: AbortSignal,
    ) =>
      call<T>(
        "/admin/v1/call/" + encodeURIComponent(capability),
        envelope,
        signal,
      ).catch((e: unknown) => {
        if (e instanceof APIError && e.status === 403)
          throw new APIError("session_changed", 403, e.operationId);
        throw e;
      }),
    playground: (input?: unknown, signal?: AbortSignal) =>
      fetchImpl(
        input === undefined
          ? "/admin/v1/playground"
          : "/admin/v1/playground/completions",
        {
          method: input === undefined ? "GET" : "POST",
          credentials: "same-origin",
          redirect: "error",
          cache: "no-store",
          headers: {
            "X-Jev-Session": "1",
            ...(input === undefined
              ? {}
              : { "Content-Type": "application/json", "X-CSRF-Token": csrf }),
          },
          body: input === undefined ? undefined : JSON.stringify(input),
          signal: AbortSignal.any([
            controller.signal,
            ...(signal ? [signal] : []),
          ]),
        },
      ),
    dispose: () => {
      csrf = "";
      controller.abort();
    },
  };
}
export type Client = ReturnType<typeof createManagementClient>;
