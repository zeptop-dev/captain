import { Alert, Badge, Button, Card, NumberInput, Select, SimpleGrid, Stack, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
interface Policy { mode: string; fixed_ms: number; deviation_pct: number; floor_ms: number; loss_pct: number; min_samples: number; window_minutes: number }
interface State { current: boolean; key: string; name: string; family: string; baseline_ms: number; learned: number; last: number; threshold_ms: number; ready: boolean; frozen: boolean; interval: number }
export function NetworkAlertCard({ nodeID }: { nodeID: number }) {
 const { t } = useTranslation(), qc = useQueryClient(), key = ['network-alerts', nodeID]
 const [clock, setClock] = useState(() => ({ mono: performance.now(), wall: Date.now() }))
 useEffect(() => { const tick = () => setClock({ mono: performance.now(), wall: Date.now() }); const timer = window.setInterval(tick, 1000); window.addEventListener('offline', tick); window.addEventListener('focus', tick); return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('focus', tick) } }, [])
 const q = useQuery({ queryKey: key, queryFn: async () => { const mono = performance.now(), wall = Date.now(); const value = await api.get<{ policy: Policy; states: State[]; now: number }>(`/api/admin/settings/nodes/${nodeID}/network-alerts`); return { ...value, mono, wall } }, refetchInterval: 30_000 })
 const [draft, setDraft] = useState<Policy | null>(null), p = draft ?? q.data?.policy
 useSettingsDirty(draft !== null && JSON.stringify(draft) !== JSON.stringify(q.data?.policy))
 const save = useMutation({ mutationFn: () => api.put(`/api/admin/settings/nodes/${nodeID}/network-alerts`, p), onSuccess: async () => { await qc.invalidateQueries({ queryKey: key }); setDraft(null); toast.ok(t('common.saved')) }, onError: toast.err })
 const patch = (x: Partial<Policy>) => !save.isPending && p && setDraft({ ...p, ...x })
 return <Card><Stack gap="sm"><Title order={5}>{t('insights.networkAlerts')}</Title><Text size="sm" c="dimmed">{t('insights.networkHint')}</Text>{q.isError && <Alert color="red">{q.error.message}</Alert>}{p && <>
 <Select label={t('insights.mode')} value={p.mode} onChange={mode => mode && patch({ mode })} allowDeselect={false} data={['off', 'fixed', 'adaptive'].map(value => ({ value, label: t(`insights.modes.${value}`) }))} />
 {p.mode !== 'off' && <SimpleGrid cols={{ base: 1, sm: 2 }}>{(['fixed_ms', 'deviation_pct', 'floor_ms', 'loss_pct', 'min_samples', 'window_minutes'] as const).filter(k => p.mode === 'fixed' ? !['deviation_pct', 'floor_ms'].includes(k) : k !== 'fixed_ms').map(k => <NumberInput key={k} label={t(`insights.${k}`)} value={p[k]} min={k === 'window_minutes' ? 2 : k === 'min_samples' ? 5 : k === 'deviation_pct' ? 10 : 1} max={k === 'window_minutes' ? 30 : k === 'min_samples' || k === 'loss_pct' ? 100 : k === 'deviation_pct' ? 1000 : 60000} onChange={v => patch({ [k]: Number(v) })} />)}</SimpleGrid>}
 <Button variant="light" disabled={!draft || q.isError} loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button>
 <Table.ScrollContainer minWidth={440}><Table><Table.Thead><Table.Tr>{['series', 'baseline', 'threshold', 'state'].map(k => <Table.Th key={k}>{t(`insights.${k}`)}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>{q.data?.states.map(v => <Table.Tr key={v.key}><Table.Td>{v.name} · {v.family}</Table.Td><Table.Td>{v.baseline_ms ? `${v.baseline_ms.toFixed(1)} ms` : '—'}</Table.Td><Table.Td>{v.threshold_ms.toFixed(1)} ms</Table.Td><Table.Td><Badge color={v.frozen ? 'orange' : 'gray'}>{t(`insights.${!v.current || q.isError || navigator.onLine === false || (q.data?.now ?? 0) + Math.max(0, clock.mono - (q.data?.mono ?? 0), clock.wall - (q.data?.wall ?? 0)) / 1000 - v.last > Math.max(120, 3 * v.interval) ? 'stale' : v.frozen ? 'frozen' : v.ready ? 'ready' : 'warming'}`)}</Badge></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
 </>}</Stack></Card>
}
