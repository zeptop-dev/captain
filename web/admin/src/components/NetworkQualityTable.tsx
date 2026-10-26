import { Badge, Group, Stack, Table, Text, Tooltip } from '@mantine/core'
import { Fragment, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { networkIdentity, networkLabel, type NetworkSample } from '../lib/network'
const ms = (v: number | null | undefined) => v == null || v < 0 ? '—' : `${v.toFixed(1)} ms`

// Kept identical across the management, standalone and public probe apps.
export function NetworkQualityTable({ samples }: { samples: NetworkSample[] }) {
 const { t } = useTranslation()
 const [now, setNow] = useState(Date.now)
 useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 10_000); return () => window.clearInterval(timer) }, [])
 return <Stack gap="xs">
  <Text size="xs" c="dimmed">{t('networkQuality.liveHint')}</Text>
  {samples.length === 0 ? <Text size="sm" c="dimmed">{t('networkQuality.empty')}</Text> : <Table.ScrollContainer minWidth={850}><Table fz="xs" striped>
   <Table.Thead><Table.Tr>{['target', 'type', 'outcome', 'latency', 'failure', 'p50', 'p95', 'jitter'].map((key) => <Table.Th key={key}>{t(`networkQuality.${key}`)}</Table.Th>)}</Table.Tr></Table.Thead>
   <Table.Tbody>{samples.map((p) => {
    const q = p.quality, w = q?.window
    const stale = !!p.at && now / 1000 - p.at > Math.max(60, (q?.interval_seconds ?? 30) * 2)
    const detail = q?.timings
    return <Fragment key={networkIdentity(p)}>
     <Table.Tr><Table.Td><Text fw={600} size="xs">{networkLabel(p)}</Text><Text size="xs" c="dimmed">{p.at ? new Date(p.at * 1000).toLocaleString() : '—'}</Text></Table.Td>
      <Table.Td>{t(`networkQuality.types.${q?.type ?? 'legacy'}`)}</Table.Td>
      <Table.Td><Badge size="xs" color={stale ? 'gray' : p.latency_ms < 0 ? 'red' : q?.outcome === 'refused' ? 'orange' : 'teal'}>{stale ? t('networkQuality.stale') : q ? t(`networkQuality.outcomes.${q.outcome}`) : p.latency_ms < 0 ? t('networkQuality.failed') : t('networkQuality.outcomes.ok')}</Badge>{q?.http_status ? <Text size="xs" mt={2}>HTTP {q.http_status}</Text> : null}</Table.Td>
      <Table.Td>{ms(p.latency_ms)}{p.mbps != null && p.mbps > 0 && <Text size="xs">{p.mbps.toFixed(1)} Mbps</Text>}</Table.Td>
      <Table.Td>{w?.attempts ? `${(100 * w.failed / w.attempts).toFixed(1)}% (${w.failed}/${w.attempts})` : !q && p.loss != null ? `${p.loss.toFixed(1)}%` : '—'}</Table.Td>
      <Table.Td>{ms(w?.p50_ms)}</Table.Td><Table.Td><Tooltip label={`${t('networkQuality.min')}: ${ms(w?.min_ms)} · ${t('networkQuality.max')}: ${ms(w?.max_ms)}`}><span>{ms(w?.p95_ms)}</span></Tooltip></Table.Td><Table.Td>{ms(w?.jitter_ms)}</Table.Td>
     </Table.Tr>
     {q && <Table.Tr><Table.Td colSpan={8}><Group gap="md">
      {(['dns_ms', 'connect_ms', 'tls_ms', 'response_ms'] as const).map((phase) => <Text key={phase} size="xs" c="dimmed">{t(`networkQuality.phases.${phase}`)}: {ms(detail?.[phase])}</Text>)}
      <Text size="xs" c="dimmed">{t('networkQuality.duration')}: {ms(q.duration_ms)}</Text>
     </Group></Table.Td></Table.Tr>}
    </Fragment>
   })}</Table.Tbody>
  </Table></Table.ScrollContainer>}
  <Text size="xs" c="dimmed">{t('networkQuality.semantics')}</Text>
 </Stack>
}
