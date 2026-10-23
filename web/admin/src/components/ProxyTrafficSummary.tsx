import { Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { ProxyTraffic } from '../lib/api'
import { bytes } from '../lib/format'

// User billing stays with the collector. This view attributes managed reverse
// traffic to its exit, using the immutable attribution stored with the report.
export function ProxyTrafficSummary({ traffic, collectedBytes }: { traffic?: ProxyTraffic; collectedBytes: number }) {
  const { t, i18n } = useTranslation()
  const date = (seconds: number) => new Date(seconds * 1000).toLocaleDateString(i18n.language, { timeZone: 'UTC' })
  return <span>
    <Text component="span" inherit style={{ whiteSpace: 'nowrap' }}>{bytes(traffic?.today_bytes ?? collectedBytes)}</Text>
    {!traffic && <Text component="span" display="block" size="xs" c="dimmed" fw={400}>{t('nodes.proxyTrafficLegacy')}</Text>}
    {traffic && <>
      <Text component="span" display="block" size="xs" c="dimmed" fw={400}>{t('nodes.proxyTrafficDay', { date: date(traffic.day_start) })}</Text>
      {traffic.forwarded_today_bytes > 0 && <Text component="span" display="block" size="xs" c="dimmed" fw={400}>{t('nodes.proxyTrafficCollected', { bytes: bytes(traffic.collected_today_bytes) })}</Text>}
      {traffic.forwarded_today_bytes > 0 && <Text component="span" display="block" size="xs" c="dimmed" fw={400}>{t('nodes.proxyTrafficForwarded', { bytes: bytes(traffic.forwarded_today_bytes) })}</Text>}
      {traffic.received_today_bytes > 0 && <Text component="span" display="block" size="xs" c="dimmed" fw={400}>{t('nodes.proxyTrafficReceived', { bytes: bytes(traffic.received_today_bytes) })}</Text>}
      {traffic.unattributed_today_bytes > 0 && <Text component="span" display="block" size="xs" c="orange" fw={400}>{t('nodes.proxyTrafficUnattributed', { bytes: bytes(traffic.unattributed_today_bytes) })}</Text>}
      {!traffic.history_complete && <Text component="span" display="block" size="xs" c="orange" fw={400}>{t('nodes.proxyTrafficPartial', { date: new Date(traffic.attribution_started_at * 1000).toLocaleString(i18n.language, { timeZone: 'UTC' }) })}</Text>}
    </>}
  </span>
}
