export type ServiceState = 'healthy' | 'degraded' | 'down' | 'maintenance' | 'unknown'
export interface ServiceMaintenance { entry_id: number; starts_at: number; ends_at: number; note: string }
export interface ServiceStatusSettings {
  revision: number
  public_enabled: boolean
  public_entry_ids: number[]
  maintenance: ServiceMaintenance[]
}
export interface ServiceStatusEntry {
  id: number; name: string; rate: number; region: string
  category: { id: number; name: string; description: string; sort: number } | null
  state: ServiceState; reason: string; evidence: string[]; observed_at: number | null; end_to_end: false
  maintenance: Omit<ServiceMaintenance, 'entry_id'> | null
}
export interface ServiceStatusSnapshot {
  generated_at: number; refresh_seconds: number
  summary: Record<ServiceState | 'total', number>
  entries: ServiceStatusEntry[]
}

export const serviceStates: ServiceState[] = ['healthy', 'degraded', 'down', 'maintenance', 'unknown']
export const serviceStateColor: Record<ServiceState, string> = { healthy: 'teal', degraded: 'orange', down: 'red', maintenance: 'yellow', unknown: 'gray' }

// Retain disabled and removed selections until an administrator explicitly
// removes them. Loading a partial catalog must not silently publish a new list.
export function missingStatusEntryIDs(settings: ServiceStatusSettings, entries: { ID: number }[]): number[] {
  const known = new Set(entries.map(entry => entry.ID))
  return [...new Set([...settings.public_entry_ids, ...settings.maintenance.map(row => row.entry_id)])].filter(id => !known.has(id))
}

export function maintenanceError(row: ServiceMaintenance): 'entry' | 'time' | 'duration' | 'note' | null {
  if (!Number.isSafeInteger(row.entry_id) || row.entry_id <= 0) return 'entry'
  if (!Number.isSafeInteger(row.starts_at) || !Number.isSafeInteger(row.ends_at) || row.starts_at <= 0 || row.ends_at <= row.starts_at) return 'time'
  if (row.ends_at - row.starts_at > 30 * 86400) return 'duration'
  // eslint-disable-next-line no-control-regex -- validation intentionally rejects control characters
  if (Array.from(row.note).length > 512 || /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/.test(row.note)) return 'note'
  return null
}

// Values remain Unix seconds in the draft. Formatting a local input must not
// rewrite an untouched timestamp during a DST transition or drop its seconds.
export function maintenanceLocalTime(unix: number): string {
  if (!unix) return ''
  const date = new Date(unix * 1000)
  if (!Number.isFinite(date.getTime())) return ''
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
export function parseMaintenanceLocalTime(value: string): number {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return 0
  const timestamp = new Date(value).getTime() / 1000
  // Invalid dates and times skipped by daylight saving must stay invalid.
  if (!Number.isSafeInteger(timestamp) || maintenanceLocalTime(timestamp) !== (value.length === 16 ? `${value}:00` : value)) return 0
  return timestamp
}
