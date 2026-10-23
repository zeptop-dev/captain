import { Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { HostTraffic } from '../lib/api'
import { bytes } from '../lib/format'

// These are NIC counter deltas for the server's reset cycle, not proxy billing.
// Keep the totals' own update time: a recent heartbeat cannot refresh old totals.
export function HostTrafficSummary({ traffic }: { traffic?: HostTraffic }) {
  const { t, i18n } = useTranslation()
  const date = (seconds: number | null | undefined, time = false) => {
    if (seconds == null || !Number.isFinite(seconds)) return '—'
    return new Date(seconds * 1000).toLocaleString(i18n.language, {
      timeZone: 'UTC', year: 'numeric', month: '2-digit', day: '2-digit',
      ...(time ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } as const : {}),
    })
  }
  return <span>
    {traffic?.valid
      ? <Text component="span" size="sm" inherit style={{ display: 'flex', flexWrap: 'wrap', gap: '0 0.75em' }}>
        <span style={{ whiteSpace: 'nowrap' }} title={t('resources.upload')}>↑ {bytes(traffic.used_up)}</span>
        <span style={{ whiteSpace: 'nowrap' }} title={t('resources.download')}>↓ {bytes(traffic.used_down)}</span>
      </Text>
      : <Text component="span" inherit>—</Text>}
    <Text component="span" size="xs" c="dimmed" fw={400} display="block" title={t('nodes.hostTrafficPeriod', { start: date(traffic?.cycle_start, true), end: date(traffic?.cycle_end, true) })}>{t('nodes.hostTrafficPeriod', { start: date(traffic?.cycle_start), end: date(traffic?.cycle_end) })}</Text>
    <Text component="span" size="xs" c="dimmed" fw={400} display="block">{t('nodes.hostTrafficUpdated', { date: date(traffic?.updated_at, true) })}</Text>
  </span>
}
