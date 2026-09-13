import { notifications } from '@mantine/notifications'

export interface DNSResult { name: string; ip: string; action: string; error?: string }
// Auto DNS outcome after saving a node or ingress: errors as a warning, changes as info.
export function dnsToast(res?: DNSResult[]) {
  if (!res || !res.length) return
  const errs = res.filter((r) => r.error)
  const changed = res.filter((r) => !r.error && (r.action === 'created' || r.action === 'updated'))
  if (errs.length) toast.err(new Error('DNS: ' + errs.map((r) => `${r.name}: ${r.error}`).join('; ')))
  else if (changed.length) toast.ok('DNS: ' + changed.map((r) => `${r.name} → ${r.ip}`).join(', '))
}

export const toast = {
  ok: (message: string) => notifications.show({ message, color: 'teal' }),
  err: (e: unknown) => notifications.show({ message: e instanceof Error ? e.message : String(e), color: 'red' }),
}
