export interface AgentHTTPSSettings {
  enabled: boolean; url: string; listen: string; certificate_id: number; revision: string
}
export interface AgentHTTPSView {
  settings: AgentHTTPSSettings
  status: { state: 'disabled' | 'listening' | 'error'; active_url: string; active_listen: string; certificate_id: number; certificate_not_after?: string; last_refresh?: string; last_error?: string }
  ready: boolean; base_url: string; in_container: boolean
}
export type AgentHTTPSCheckStatus = 'ok' | 'disabled' | 'not_ready' | 'dns_error' | 'connect_error' | 'timeout' | 'tls_error' | 'redirect' | 'http_error' | 'unexpected_service'
export interface AgentHTTPSCheckResult {
  revision: string; url: string; checked_at: string; perspective: 'panel'; status: AgentHTTPSCheckStatus; http_status?: number; elapsed_ms: number
}
export interface AgentHTTPSObservation { result: AgentHTTPSCheckResult; start: { wall: number; mono: number } }

// Bind an observation to the saved configuration and the request start time.
export function agentHTTPSCheckFresh(observation: AgentHTTPSObservation, settings: AgentHTTPSSettings, wallNow: number, monoNow: number): boolean {
  return observation.result.perspective === 'panel' && observation.result.revision === settings.revision && observation.result.url === settings.url && settings.enabled && agentHTTPSFresh(observation.start.wall, observation.start.mono, wallNow, monoNow)
}

export interface AgentHTTPSChoice { enabled: boolean; url: string; ready: boolean; entries?: Array<{ id: string; name: string; url: string; ready: boolean; default: boolean }> }

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

// Use only the validated saved origin, shell-quoted even for unusual hostnames.
export function agentHTTPSDockerCommand(savedURL: string, readOnly = false): string | null {
  const url = agentHTTPSOrigin(savedURL)
  if (!url) return null
  const port = Number(url.port || 443)
  if (!Number.isInteger(port) || port < 1024 || port > 65535 || port === 8080) return null
  const origin = url.origin.replaceAll("'", "'\"'\"'")
  return `curl -q -fsSL --proto '=https' --proto-redir '=https' https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh -o captain-install.sh && sudo sh captain-install.sh ${readOnly ? 'check-agent-https' : 'enable-agent-https'} --agent-https-port ${port} --agent-https-url '${origin}'${readOnly ? '' : ' --agent-firewall auto'}`
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
  return `curl -q --fail --show-error --globoff --max-time 10 --proto '=https' -- '${target}'`
}
