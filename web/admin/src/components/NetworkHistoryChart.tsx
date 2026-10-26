import { Alert, Group, Select, Stack, Text } from '@mantine/core'
import { LineChart } from '@mantine/charts'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { networkIdentity, networkLabel, type NetworkHistory, type NetworkPoint } from '../lib/network'

export function NetworkHistoryChart({ history }: { history?: Pick<NetworkHistory, 'points' | 'from' | 'to' | 'step'> }) {
 const { t } = useTranslation()
 const [chosen, setChosen] = useState<string | null>(null)
 const [metric, setMetric] = useState('avg_ms')
 const targets = new Map<string, string>()
 for (const p of history?.points ?? []) targets.set(networkIdentity(p), networkLabel(p))
 const selected = chosen && targets.has(chosen) ? chosen : targets.keys().next().value
 const points = (history?.points ?? []).filter((p) => networkIdentity(p) === selected)
 const byTs = new Map(points.map((p) => [p.ts, p]))
 const value = (p?: NetworkPoint): number | null => {
  if (!p) return null
  if (metric === 'failure') return p.n > 0 ? 100 * p.lost / p.n : null
  if (metric === 'avg_ms') return p.avg_ms < 0 ? null : p.avg_ms
  if (metric === 'p50_ms' || metric === 'p95_ms' || metric === 'jitter_ms') return p.quality?.[metric] ?? null
  return p.quality?.timings?.[metric as keyof NonNullable<NetworkPoint['quality']>['timings']] ?? null
 }
 const data: { time: string; value: number | null }[] = []
 if (history?.step && history.step > 0) for (let ts = Math.floor(history.from / history.step) * history.step; ts <= history.to; ts += history.step) data.push({ time: new Date(ts * 1000).toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }), value: value(byTs.get(ts)) })
 const failures = new Map<string, number>()
 for (const p of points) for (const [key, count] of Object.entries(p.quality?.outcomes ?? {})) if (key !== 'ok') failures.set(key, (failures.get(key) ?? 0) + count)
 const skipped = points.reduce((n, p) => n + (p.quality?.skipped ?? 0), 0)
 return <Stack gap="sm">
  <Group grow align="flex-start"><Select label={t('networkQuality.target')} searchable value={selected ?? null} onChange={setChosen} data={[...targets].map(([value, label]) => ({ value, label }))} allowDeselect={false} /><Select label={t('networkQuality.metric')} value={metric} onChange={(v) => v && setMetric(v)} data={['avg_ms', 'failure', 'p50_ms', 'p95_ms', 'jitter_ms', 'dns_ms', 'connect_ms', 'tls_ms', 'response_ms'].map((value) => ({ value, label: t(`networkQuality.metrics.${value}`) }))} allowDeselect={false} /></Group>
  <Text size="xs" c="dimmed">{t('networkQuality.historyHint')}</Text>
  {data.some((p) => p.value != null) ? <LineChart h={220} data={data} dataKey="time" series={[{ name: 'value', label: t(`networkQuality.metrics.${metric}`), color: 'indigo.6' }]} withDots={false} connectNulls={false} curveType="linear" valueFormatter={(v) => `${v.toFixed(1)} ${metric === 'failure' ? '%' : 'ms'}`} /> : <Text size="sm" c="dimmed" py="md">{t('networkQuality.noHistory')}</Text>}
  {skipped > 0 && <Alert color="yellow">{t('networkQuality.skipped', { count: skipped })}</Alert>}
  <Group gap="sm">{[...failures].map(([key, count]) => <Text size="xs" key={key}>{t(`networkQuality.outcomes.${key}`)}: {count}</Text>)}</Group>
 </Stack>
}
