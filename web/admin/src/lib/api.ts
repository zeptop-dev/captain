// Minimal typed fetch wrapper. Sessions are cookies, so nothing to attach.
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, url: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
    signal,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data as T
}

export const api = {
  get: <T>(url: string, signal?: AbortSignal) => request<T>('GET', url, undefined, signal),
  post: <T>(url: string, body?: unknown, signal?: AbortSignal) => request<T>('POST', url, body ?? {}, signal),
  patch: <T>(url: string, body: unknown) => request<T>('PATCH', url, body),
  put: <T>(url: string, body: unknown) => request<T>('PUT', url, body),
  del: <T>(url: string, body?: unknown) => request<T>('DELETE', url, body),
}

export interface Me { id: number; email: string; role: string; version?: string; totp?: boolean }
export interface Node {
  id: number; name: string; public_addr: string; internal_addr: string; v6_addr: string; domain: string; domain_shared?: boolean; domain_conflict?: boolean; monitor_url: string; dstatus_sid?: string; decoy_enabled?: boolean; decoy_upstream?: string; user_speed_limit_mbps?: number
  version: string; platform: string; hostname: string; last_seen_at: string | null; online: boolean; paired: boolean
  pair_code?: string; traffic_today_bytes: number; inbounds: number; upgrade_to?: string; outdated: boolean; cert_problem: boolean; doctor_fail?: boolean; mita_quotas?: boolean; egress_by_ingress?: boolean }
// runNodeJob queues a one-off job on the node and polls until it answers.
export async function runNodeJob<T>(nodeID: number, kind: string, params: unknown, timeoutMs = 150_000, signal?: AbortSignal): Promise<T> {
  signal?.throwIfAborted()
  const { id } = await api.post<{ id: string }>(`/api/admin/nodes/${nodeID}/jobs`, { kind, params }, signal)
  const started = Date.now()
  while (Date.now() - started < timeoutMs) {
    signal?.throwIfAborted()
    await new Promise<void>((resolve, reject) => {
      const abort = () => { clearTimeout(timer); reject(signal?.reason) }
      const timer = setTimeout(() => { signal?.removeEventListener('abort', abort); resolve() }, 2000)
      signal?.addEventListener('abort', abort, { once: true })
    })
    signal?.throwIfAborted()
    const j = await api.get<NodeJob>(`/api/admin/nodes/${nodeID}/jobs/${id}`, signal)
    signal?.throwIfAborted()
    if (j.done_at) { if (j.error) throw new Error(j.error); return j.result as T }
  }
  throw new Error('node did not answer in time')
}
export interface NodeJob { id: string; node_id: number; kind: string; params: unknown; result?: unknown; error?: string; created_at: string; done_at?: string }
export interface CertStatus { domain: string; method: string; not_after: string; error?: string }
export interface DoctorCheck { id: string; name: string; status: 'ok' | 'warn' | 'fail' | 'skip'; detail?: string }
export interface DoctorReport { at: string; checks: DoctorCheck[]; summary: { ok: number; warn: number; fail: number; skip: number } }
export interface SiteSettings {
  name: string; tagline: string; description: string
  features: { title: string; text: string; icon?: string }[]
  locations: { name: string; lat: number; lng: number; tag?: string }[]
  hub: { name: string; lat: number; lng: number } | null
  faq: { q: string; a: string }[]
  links: { telegram?: string; tos?: string; download?: string; github?: string }
  theme?: { primary?: string; radius?: string; scheme?: string; site_scheme?: string; font_family?: string; portal_title?: string }
  inject_head?: string; inject_body?: string
  show_plans: boolean
}
export interface OIDCProvider { id: string; name: string; issuer: string; client_id: string; client_secret?: string; scopes?: string[]; trust_email: boolean; auto_register: boolean; has_secret?: boolean }
export interface OIDCSettings { providers: OIDCProvider[]; password_login: boolean }
export interface MailSettings {
  provider: string; from_name: string; from_address: string
  smtp: { host: string; port: number; username: string; password: string; security: string }
  resend: { api_key: string }
  verify_registration: boolean; reminders: boolean; traffic_thresholds?: number[]
  language?: string
}
export interface SubscriptionSettings { urls: string[]; short_links?: boolean; auto_flags?: boolean; single_plan?: boolean; info_lines?: string[]; hwid?: { enabled: boolean; require: boolean; fallback_limit: number; announce: string } }
export interface ACMESettings { email: string; has_cloudflare_token: boolean }
export interface Inbound {
  ID: number; NodeID: number; Tag: string; Protocol: string; Listen: string; Port: number; Core: string
  Settings: Record<string, unknown>; GroupID: number | null; Enabled: boolean; Sort: number; IngressID: number | null
}
export interface PortMapping { local_from: number; local_to: number; public_from: number }
export interface Ingress { port_mappings?: PortMapping[]; require_ingress?: boolean; id: number; node_id: number; name: string; kind: string; bind_ip: string; line_ip: string; entry_host: string; entry_domain?: string; port_from: number; port_to: number; port_offset: number; reserved_ports?: number[] }
export interface Group { ID: number; Name: string }
export interface Plan {
  ID: number; Name: string; PriceCents: number; PeriodDays: number; ResetDays: number; ResetMode: string; Prices: { period_days: number; price_cents: number }[] | null; QuotaBytes: number; DeviceLimit: number
  SpeedLimitMbps: number; GroupID: number | null; Sort: number; Enabled: boolean
}
export interface Coupon {
  ID: number; Code: string; Name: string; Kind: string; Value: number; PlanIDs: number[] | null; MaxUses: number; Used: number; PerUser: number
  StartsAt: string | null; ExpiresAt: string | null; Enabled: boolean; CreatedAt: string
}
export interface RegistrationStatus { settings: RegistrationSettings; has_captcha_secret: boolean; enabled: boolean; password_open: boolean; oidc_open: boolean }
export interface RegistrationSettings { enabled?: boolean; email_suffixes: string[]; invite_only: boolean; ip_limit: number; ip_window_hours: number; captcha: { provider: string; site_key: string; secret_key: string } }
export interface InviteSettings { enabled: boolean; percent: number; first_order_only: boolean; multi_level: boolean; level2: number; level3: number; payout: string; min_withdraw_cents: number; withdraw_methods: string[] }
export interface NoticeSettings { enabled: boolean; title: string; body: string }
export interface UserRow {
  id: number; email: string; uuid: string; sub_token: string; sub_url: string; group_id: number | null; balance_cents: number; status: string
  created_at: string; plan_name: string; expires_at: string | null; quota_bytes: number; used_bytes: number; sub_usable: boolean; sub_count: number
}
export interface OnlineDevice { via_relay?: boolean; ip: string; node_id: number; last_seen_at: string }
export interface HwidDevice { hwid: string; platform?: string; os_version?: string; device_model?: string; user_agent?: string; request_ip?: string; first_seen_at: string; last_seen_at: string }
export interface SubRequest { id: number; at: string; request_ip: string; user_agent: string; hwid?: string; rule?: string; response: string }

export interface Entry {
  ID: number; Name: string; InboundID: number; ChainID: number | null; DisplayHost: string; DisplayPort: number
  Rate: number; Sort: number; Enabled: boolean; Tags?: string[]; Region?: string; ClientExtra?: Record<string, unknown> | null }
export interface Order {
  ID: number; No: string; UserID: number; PlanID: number; AmountCents: number; Gateway: string; GatewayRef: string
  Status: string; CreatedAt: string; PaidAt: string | null; PeriodDays: number; DiscountCents: number; email: string; plan_name: string
}
export interface Dashboard {
  stats: {
    users: number; active_subs: number; nodes: number; nodes_online: number; revenue_today_cents: number
    revenue_month_cents: number; orders_pending: number; traffic_today_bytes: number; online_devices: number
  }
  traffic: { day: number; up: number; down: number }[]
  open_tickets?: number
  attention?: { nodes?: { offline: number; unpaired: number; doctor_fail: number }; open_incidents?: number; assets_due?: number }
}
export interface UpdateInfo {
  current: string; latest: string; has_update: boolean; release_build: boolean; in_container: boolean
  notes?: string; published_at?: string; url?: string; checked_at: string; cached: boolean; warning?: string
  has_backup: boolean; backup_version?: string
  host_update?: { available: boolean; job?: { id: string; target: string; previous?: string; phase: string; error?: string; updated_at: string } }
}
export interface SystemUpdate { captain?: UpdateInfo; bosun_latest: string }
export interface Page<T> { items: T[]; total: number; page: number; per_page: number }

export interface IngressInput { Name: string; Kind: string; BindIP: string; LineIP: string; EntryHost: string; EntryDomain: string; PortFrom: number; PortTo: number; PortOffset: number; ReservedPorts: number[]; PortMappings: PortMapping[]; RequireIngress: boolean }
