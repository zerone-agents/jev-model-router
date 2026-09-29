import { APIError, type CallEnvelope, type Client } from "./api";
export function descriptionInput<T extends { description: string }>(
  resource: T,
  description: string,
): T {
  return { ...resource, description };
}
export function prepareWrite(
  capability: string,
  input: unknown,
  version: number,
  key: string,
) {
  return {
    capability,
    serialized: JSON.stringify({
      input,
      expected_version: version,
      idempotency_key: key,
    } satisfies CallEnvelope),
  };
}
export type PreparedWrite = ReturnType<typeof prepareWrite>;
export class WriteAttempt {
  state: "idle" | "saving" | "unknown" | "conflict" | "saved" | "failed" =
    "idle";
  constructor(
    readonly write: PreparedWrite,
    readonly created = Date.now(),
  ) {}
  async send<T>(client: Client, now = Date.now()) {
    if (this.state === "saving") throw new APIError("write_busy");
    if (
      this.state === "saved" ||
      this.state === "conflict" ||
      this.state === "failed"
    )
      throw new APIError("write_finished");
    if (now - this.created >= 86400000) throw new APIError("retry_expired");
    this.state = "saving";
    try {
      const result = await client.call<T>(
        this.write.capability,
        JSON.parse(this.write.serialized),
      );
      this.state = "saved";
      return result;
    } catch (e) {
      this.state =
        e instanceof APIError && e.code === "config_conflict"
          ? "conflict"
          : e instanceof APIError &&
              (e.status === 0 ||
                e.status >= 500 ||
                e.code === "invalid_response")
            ? "unknown"
            : "failed";
      throw e;
    }
  }
}
