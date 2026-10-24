// arc-managed: opencode plugin shim. 'arc init --opencode' overwrites this file; remove this line to keep local edits.
//
// Forwards opencode tool calls to 'arc hook opencode' (SPEC.md, "arc hook").
// The contract with opencode is pinned in internal/hook/opencode.go; the JSON
// exchanged with arc is documented there too. Any arc failure (not on PATH,
// crash, timeout, bad reply) allows the call: a broken arc never blocks an
// agent. Only a deny reply blocks, by throwing from execute.before.

import { spawn } from "node:child_process"

const TIMEOUT_MS = 10_000
// Tools arc judges before they run (file writes) and after (shell commands).
const BEFORE = new Set(["write", "edit", "multiedit", "patch", "apply_patch"])
const AFTER = new Set(["shell", "bash"])

// ask runs 'arc hook opencode' with req as JSON on stdin and resolves to
// arc's reply, or null on any failure.
function ask(req) {
  return new Promise((resolve) => {
    let done = false
    let out = ""
    let timer
    const finish = (v) => {
      if (!done) {
        done = true
        clearTimeout(timer)
        resolve(v)
      }
    }
    let child
    try {
      child = spawn("arc", ["hook", "opencode"], { cwd: req.directory, stdio: ["pipe", "pipe", "ignore"] })
    } catch {
      return finish(null)
    }
    timer = setTimeout(() => {
      try { child.kill() } catch {}
      finish(null)
    }, TIMEOUT_MS)
    child.on("error", () => finish(null))
    child.stdout.on("data", (b) => { out += b })
    child.on("close", () => {
      if (!out.trim()) return finish(null)
      try {
        const v = JSON.parse(out)
        finish(v && typeof v === "object" ? v : null)
      } catch {
        finish(null)
      }
    })
    child.stdin.on("error", () => {})
    child.stdin.end(JSON.stringify(req))
  })
}

// withContext returns result with text appended to the content the model sees.
function withContext(result, text) {
  const content = result?.content
  const parts = typeof content === "string" ? [{ type: "text", text: content }] : Array.isArray(content) ? [...content] : []
  parts.push({ type: "text", text })
  return { ...result, content: parts }
}

export default {
  id: "arc",
  async setup(ctx) {
    const directory = ctx.location?.directory ?? process.cwd()
    const pending = new Map() // call id -> context to attach once the call completes
    const request = (hook, event, extra) => ({
      hook,
      tool: event.tool,
      session_id: event.sessionID,
      call_id: event.id,
      directory,
      input: event.input,
      ...extra,
    })

    await ctx.tool.hook("execute.before", async (event) => {
      let reply = null
      try {
        if (BEFORE.has(event.tool)) reply = await ask(request("execute.before", event))
      } catch {}
      if (reply?.action === "deny") throw new Error(String(reply.reason || "arc: denied"))
      if (typeof reply?.context === "string" && reply.context) pending.set(event.id, reply.context)
    })

    await ctx.tool.hook("execute.after", async (event) => {
      try {
        const texts = []
        if (pending.has(event.id)) {
          texts.push(pending.get(event.id))
          pending.delete(event.id)
        }
        if (event.status !== "completed") return
        if (AFTER.has(event.tool)) {
          const reply = await ask(request("execute.after", event, { result: event.result }))
          if (typeof reply?.context === "string" && reply.context) texts.push(reply.context)
        }
        for (const t of texts) event.result = withContext(event.result, t)
      } catch {}
    })
  },
}
