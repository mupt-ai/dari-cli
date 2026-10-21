// The Dari provider for Pi. It mirrors each response's routing headers into
// Pi's footer: the model that served the turn, its thinking level, how many
// turns remain on the current lease, and whether the last finished response
// read the provider's prompt cache. The launcher supplies the status renderer
// and cache TTL through the environment, along with the models to offer and
// the router's base URL.
export default function (pi: any) {
  const executable = process.env.DARI_STATUS_EXECUTABLE
  const cacheTTL = Number(process.env.DARI_CACHE_TTL_MS ?? 300000)
  // The launcher lists the models this organization can use, default first.
  const models: string[] = JSON.parse(process.env.DARI_MODELS ?? '["dari/routing"]')
  const routingBaseURL = process.env.DARI_ROUTING_BASE_URL ?? "https://routing.dari.dev"
  pi.registerProvider("dari", {
    name: "Dari",
    baseUrl: `${routingBaseURL}/v1`,
    apiKey: "$DARI_ROUTING_API_KEY",
    api: "openai-completions",
    compat: { sendSessionAffinityHeaders: true },
    models: models.map((id: string) => ({
      id,
      name: id === "dari/routing" ? "Dari Router" : id.replace("dari/model/", ""),
      reasoning: false,
      input: ["text", "image"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 1000000,
      maxTokens: 128000
    }))
  });

  let headers: Record<string, string> | undefined;
  let cache: string[] = [];
  let revision = 0;
  let expiry: ReturnType<typeof setTimeout> | undefined;
  const render = async (ctx: any, expectedRevision: number) => {
    if (!executable) return
    const currentHeaders = headers;
    const currentCache = cache;
    const result = await pi.exec(executable, [
      "agent-status", "--headers", JSON.stringify(currentHeaders), ...currentCache
    ], { timeout: 2000 });
    if (result.code === 0 && revision === expectedRevision) {
      const parts = result.stdout.trim().split(" · ");
      ctx.ui.setStatus("dari", parts.map((part: string, index: number) =>
        ctx.ui.theme.fg(index === 0 ? "accent" : "dim", part)
      ).join(" · "));
    }
  };

  pi.on("after_provider_response", async (event: any, ctx: any) => {
    if (!ctx.hasUI || !event.headers?.["x-dari-selected-model"]) return;
    const nextHeaders = Object.fromEntries([
      "x-dari-selected-model", "x-dari-thinking-level", "x-dari-lease-turns-remaining"
    ].filter(name => event.headers[name] !== undefined).map(name => [name, event.headers[name]]));
    if (headers?.["x-dari-selected-model"] !== nextHeaders["x-dari-selected-model"]) cache = [];
    headers = nextHeaders;
    const currentRevision = ++revision;
    await render(ctx, currentRevision);
  });

  // Usage arrives with the finished message. Re-render once the cache TTL has
  // passed so an idle session shows "cache cold".
  pi.on("message_end", async (event: any, ctx: any) => {
    const message = event.message;
    if (!ctx.hasUI || !headers || message?.role !== "assistant" || message.provider !== "dari" || !message.usage) return;
    cache = ["--cached-tokens", String(message.usage.cacheRead ?? 0), "--responded-at", String(Date.now())];
    clearTimeout(expiry);
    const currentCache = cache;
    const currentRevision = ++revision;
    expiry = setTimeout(() => {
      // A new revision drops any in-flight render that computed "cache warm".
      if (cache === currentCache) render(ctx, ++revision).catch(() => {});
    }, cacheTTL);
    await render(ctx, currentRevision);
  });
}
