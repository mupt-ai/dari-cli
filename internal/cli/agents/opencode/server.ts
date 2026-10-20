import { renameSync, writeFileSync } from "node:fs"

const statusFile = new URL("../status.json", import.meta.url)
const headerNames = ["x-dari-selected-model", "x-dari-thinking-level", "x-dari-lease-turns-remaining"]

// The last usage object in a JSON or SSE body; cache reads default to zero.
function cachedTokens(body: string): number | undefined {
  let cached: number | undefined
  for (const line of [body, ...body.split("\n").map(line => line.startsWith("data:") ? line.slice(5) : "")]) {
    try {
      const value = JSON.parse(line)
      const usage = value?.usage ?? value?.response?.usage
      if (usage) cached = usage.prompt_tokens_details?.cached_tokens ?? usage.input_tokens_details?.cached_tokens ?? 0
    } catch {}
  }
  return cached
}

// The server half of OpenCode's Dari status. It tags each request with its
// kind and agent, reads each Dari response's routing headers and usage, and
// writes the rendered status per session next to the plugin. The file lives
// outside the plugin directory so status writes do not trigger OpenCode's
// plugin reloads. The launcher supplies the status renderer and cache TTL
// through the environment.
export default {
  id: "dari.status",
  setup: async (ctx: any) => {
    const executable = process.env.DARI_STATUS_EXECUTABLE
    const cacheTTL = Number(process.env.DARI_CACHE_TTL_MS ?? 300000)
    // OpenCode names the session but not the kind of request or the agent;
    // Dari uses both to split Activity into main, subagent, and background work.
    await ctx.session.hook("model.request", (event: any) => {
      event.headers["x-opencode-request-kind"] = event.kind
      event.headers["x-opencode-agent"] = event.agent
    }, { providerID: "dari" })
    const statuses: Record<string, string> = {}
    const routes: Record<string, { headers: Record<string, string>, cachedTokens?: number, respondedAt?: number }> = {}
    const expiries: Record<string, ReturnType<typeof setTimeout>> = {}
    const generations: Record<string, number> = {}
    // Only the most recently started render per session may write; each one
    // reads the current state, so an older result (including a warm render
    // that finishes after the TTL re-render) is stale. The write and rename
    // run synchronously, so they never interleave.
    const render = async (sessionID: string) => {
      if (!executable) return
      const generation = generations[sessionID] = (generations[sessionID] ?? 0) + 1
      const route = routes[sessionID]
      const args = ["agent-status", "--headers", JSON.stringify(route.headers)]
      if (route.cachedTokens !== undefined) args.push("--cached-tokens", String(route.cachedTokens), "--responded-at", String(route.respondedAt))
      const child = Bun.spawn([executable, ...args], { stdout: "pipe", stderr: "ignore" })
      const status = (await new Response(child.stdout).text()).trim()
      if ((await child.exited) !== 0 || !status || generations[sessionID] !== generation) return
      statuses[sessionID] = status
      const temporary = new URL("../status.json.tmp", import.meta.url)
      writeFileSync(temporary, JSON.stringify(statuses))
      renameSync(temporary, statusFile)
    }
    await ctx.session.hook("http.response", async (event: any) => {
      const response = event.response.headers
      if (event.kind !== "primary" || !response.get(headerNames[0])) return
      const headers = Object.fromEntries(headerNames.filter(name => response.has(name)).map(name => [name, response.get(name)]))
      const previous = routes[event.sessionID]
      // The same model keeps the last response's cache state; a switch starts cold.
      const route = previous?.headers[headerNames[0]] === headers[headerNames[0]]
        ? { headers, cachedTokens: previous.cachedTokens, respondedAt: previous.respondedAt }
        : { headers }
      routes[event.sessionID] = route
      // Usage arrives at the end of the stream. Copy the body before anything
      // can consume it, and render without holding the client's response.
      const copy = event.response.clone()
      render(event.sessionID).catch(() => {})
      // Re-render once the cache TTL has passed.
      copy.text().then(async (body: string) => {
        const cached = cachedTokens(body)
        if (cached === undefined || routes[event.sessionID] !== route) return
        route.cachedTokens = cached
        route.respondedAt = Date.now()
        clearTimeout(expiries[event.sessionID])
        expiries[event.sessionID] = setTimeout(() => render(event.sessionID).catch(() => {}), cacheTTL)
        await render(event.sessionID)
      }).catch(() => {})
    }, { providerID: "dari" })
  },
}
