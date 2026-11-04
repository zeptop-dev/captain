export interface AgentHTTPSSettings {
  enabled: boolean; url: string; listen: string; certificate_id: number; revision: string
}
export interface AgentHTTPSView {
  settings: AgentHTTPSSettings
  status: { state: 'disabled' | 'listening' | 'error'; active_url: string; active_listen: string; certificate_id: number; certificate_not_after?: string; last_refresh?: string; last_error?: string }
  ready: boolean; base_url: string; in_container: boolean
}
export interface AgentHTTPSChoice { enabled: boolean; url: string; ready: boolean }

export function agentHTTPSOrigin(value: string): URL | null {
  try {
    const url = new URL(value)
    return Number(url.port || 443) > 0 && url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash && url.pathname === '/' ? url : null
  } catch { return null }
}

export function suggestedAgentHTTPSURL(baseURL: string): string {
  try {
    const url = new URL(baseURL)
    return `https://${url.hostname}:2096`
  } catch { return '' }
}

// Only a validated numeric port reaches shell text. Use the saved origin, never
// an unsaved hostname or listener string, to prepare host port publishing.
export function agentHTTPSDockerCommand(savedURL: string): string | null {
  const url = agentHTTPSOrigin(savedURL)
  if (!url) return null
  const port = Number(url.port || 443)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null
  return `curl -fsSL --proto '=https' --proto-redir '=https' https://raw.githubusercontent.com/zeptop-dev/captain/master/scripts/install.sh -o captain-install.sh && sudo sh captain-install.sh enable-agent-https --agent-https-port ${port}`
}

// A stale success cannot survive a failed poll, browser offline event or sleep.
export function agentHTTPSFresh(wallStart: number, monoStart: number, wallNow: number, monoNow: number): boolean {
  const wall = wallNow - wallStart, mono = monoNow - monoStart
  return wall >= 0 && mono >= 0 && wall < 30_000 && mono < 30_000
}

export function agentHTTPSCheckCommand(savedURL: string): string | null {
  const url = agentHTTPSOrigin(savedURL)
  if (!url) return null
  const target = `${url.origin}/api/agent/health`.replaceAll("'", "'\"'\"'")
  return `curl --fail --show-error --max-time 10 --proto '=https' -- '${target}'`
}
