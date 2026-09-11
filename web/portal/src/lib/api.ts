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
}

export interface Me {
  id: number; email: string; balance_cents: number; subscription_url: string; gateways: string[]
  subscription: { plan_id: number; starts_at: string; expires_at: string | null; reset_at: string | null; quota_bytes: number; used_bytes: number; usable: boolean; online_devices: number } | null
}
export interface Plan { ID: number; Name: string; PriceCents: number; PeriodDays: number; QuotaBytes: number; DeviceLimit: number; SpeedLimitMbps: number }
export interface Order { ID: number; No: string; PlanID: number; AmountCents: number; Gateway: string; Status: string; CreatedAt: string; PaidAt: string | null }
export interface Server { name: string; host: string; port: number; protocol: string; uri: string }
