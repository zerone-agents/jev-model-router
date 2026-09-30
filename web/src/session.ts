import {
  APIError,
  createSessionAPI,
  createManagementClient,
  type Client,
  type Capability,
  type SessionInfo,
} from "./api";
export class Session {
  client: Client | null = null;
  capabilities: Capability[] = [];
  private generation = 0;
  private pending: Client | null = null;
  private csrf = "";
  private busy = false;
  private uncertain = false;
  private auth;
  constructor(private fetcher: typeof fetch = fetch) {
    this.auth = createSessionAPI(fetcher);
  }
  private async exclusive<T>(work: () => Promise<T>) {
    if (this.busy) throw new APIError("auth_busy");
    this.busy = true;
    try {
      return await work();
    } finally {
      this.busy = false;
    }
  }
  private async attach(info: SessionInfo, generation: number) {
    if (generation !== this.generation) return false;
    const client = createManagementClient(info.csrf_token, this.fetcher);
    this.pending = client;
    try {
      const result = await client.schema();
      if (generation !== this.generation) {
        client.dispose();
        return false;
      }
      this.client = client;
      this.pending = null;
      this.csrf = info.csrf_token;
      this.capabilities = result.data.filter(
        (c) => c.availability === "available",
      );
      return true;
    } catch (e) {
      client.dispose();
      if (generation !== this.generation) return false;
      this.pending = null;
      throw e;
    }
  }
  async connect(token: string) {
    return this.exclusive(async () => {
      this.dispose();
      const generation = this.generation;
      // An uncertain exchange may already have installed a cookie. Resolve it first.
      if (this.uncertain) {
        try {
          const info = await this.auth.status();
          this.uncertain = false;
          return await this.attach(info, generation);
        } catch (e) {
          if (!(e instanceof APIError && e.status === 401)) throw e;
          this.uncertain = false;
        }
      }
      try {
        const info = await this.auth.login(token);
        token = "";
        return await this.attach(info, generation);
      } catch (e) {
        if (e instanceof APIError && (e.status === 0 || e.status >= 500))
          this.uncertain = true;
        if (generation !== this.generation) return false;
        throw e;
      } finally {
        token = "";
      }
    });
  }
  async restore() {
    return this.exclusive(async () => {
      this.dispose();
      const generation = this.generation;
      try {
        const info = await this.auth.status();
        this.uncertain = false;
        return await this.attach(info, generation);
      } catch (e) {
        if (generation !== this.generation) return false;
        if (e instanceof APIError && e.status === 401) {
          this.uncertain = false;
          return false;
        }
        throw e;
      }
    });
  }
  async logout() {
    return this.exclusive(async () => {
      const generation = this.generation;
      try {
        await this.auth.logout(this.csrf);
      } catch (e) {
        if (e instanceof APIError && e.status === 403)
          throw new APIError("session_changed", 403);
        if (!(e instanceof APIError && e.status === 401)) throw e;
      }
      if (generation === this.generation) this.dispose();
    });
  }
  dispose() {
    this.generation++;
    this.client?.dispose();
    this.pending?.dispose();
    this.client = null;
    this.pending = null;
    this.capabilities = [];
    this.csrf = "";
  }
  expire(client: Client) {
    if (client === this.client) this.dispose();
  }
}
