export interface Location { name: string; lat: number; lng: number; tag?: string }
export interface Feature { title: string; text: string; icon?: string }
export interface FAQ { q: string; a: string }
export interface Site {
  name: string; tagline: string; description: string
  features: Feature[]; locations: Location[]; hub: { name: string; lat: number; lng: number } | null
  faq: FAQ[]; links: { telegram?: string; tos?: string; download?: string; github?: string }
  show_plans: boolean; registration: boolean
  stats: { nodes: number; online: number; users: number; locations: number }
}
export interface Plan { ID: number; Name: string; PriceCents: number; PeriodDays: number; QuotaBytes: number; DeviceLimit: number; SpeedLimitMbps: number }

export async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin' })
  if (!r.ok) throw new Error(r.statusText)
  return r.json() as Promise<T>
}
