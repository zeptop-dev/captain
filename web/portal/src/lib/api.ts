export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) { super(message); this.status = status }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, { method, headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined, body: body !== undefined ? JSON.stringify(body) : undefined, credentials: 'same-origin' })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data as T
}

export const api = {
  get: <T>(url: string) => request<T>('GET', url),
  post: <T>(url: string, body?: unknown) => request<T>('POST', url, body ?? {}),
  put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body ?? {}),
  del: <T>(url: string) => request<T>('DELETE', url),
}

export interface Sub { id: number; plan_id: number; plan_name: string; status: 'active' | 'queued'; starts_at: string; expires_at: string | null; reset_at: string | null; quota_bytes: number; used_bytes: number; usable: boolean; period_days: number }
export interface Me {
  id: number; email: string; balance_cents: number; subscription_url: string; gateways: string[]
  subscription: { plan_id: number; starts_at: string; expires_at: string | null; reset_at: string | null; quota_bytes: number; used_bytes: number; usable: boolean; online_devices: number } | null
  subscriptions: Sub[]; online_devices: number; single_plan: boolean
  hwid_enabled: boolean; hwid_limit: number; hwid_devices: { hwid: string; platform?: string; os_version?: string; device_model?: string; user_agent?: string; last_seen_at: string }[]
}
export interface Plan { ID: number; Name: string; PriceCents: number; PeriodDays: number; QuotaBytes: number; DeviceLimit: number; SpeedLimitMbps: number; Prices: { period_days: number; price_cents: number }[] | null }
export interface Quote { plan_id: number; period_days: number; list_cents: number; discount_cents: number; surplus_cents: number; amount_cents: number; coupon?: string }
export interface Invite { code: string; url: string; enabled: boolean; percent: number; first_order_only: boolean; levels: number[]; payout: string; commission_cents: number; min_withdraw_cents: number; withdraw_methods: string[]; invited: number; earned_cents: number; invited_by: boolean }
export interface Withdrawal { id: number; amount_cents: number; method: string; account: string; status: string; note: string; created_at: string }
export interface Order { ID: number; No: string; PlanID: number; AmountCents: number; Gateway: string; Status: string; CreatedAt: string; PaidAt: string | null }
export interface Server { name: string; host: string; port: number; protocol: string; uri: string; tags?: string[] }
