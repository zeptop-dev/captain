import { Alert, Badge, Button, Card, Group, Loader, NumberInput, Select, SimpleGrid, Stack, Switch, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'
import type { MonitorNode } from '../lib/monitoring'
import { AvailabilityCard } from './AvailabilityCard'
import { useURLChoice } from '../lib/use-url-choice'
interface Incident { silenced?: boolean; silence_until?: number | null; id: number; node_id: number; node: string; kind: string; started_at: number; updated_at: number; ended_at: number | null; resolution: string; value: number; threshold: number; acknowledged_at: number; acknowledged_by: string }
interface MonitorWindow { id: number; node_id: number | null; kind: string; starts_at: number; ends_at: number; note: string; canceled_at: number | null }
const date = (v: number) => new Date(v * 1000).toLocaleString()
export function MonitoringAlerts({ nodes }: { nodes: MonitorNode[] }) {
 const { t } = useTranslation(), qc = useQueryClient(), { me } = useAuth()
 const admin = me?.role === 'admin'
 const [nodeChoice, setNodeChoice] = useURLChoice('node', ['all', ...nodes.map(n => String(n.id))], 'all')
 const node = nodeChoice === 'all' ? null : nodeChoice
 const setNode = (value: string | null) => setNodeChoice(value ?? 'all')
 const [state, setState] = useState<string | null>('open'), [before, setBefore] = useState(0)
 const [scope, setScope] = useState<string | null>('all'), [kind, setKind] = useState<string | null>('maintenance')
 const [immediate, setImmediate] = useState(true), [start, setStart] = useState(''), [minutes, setMinutes] = useState<number | string>(60), [note, setNote] = useState('')
 const q = useQuery({ queryKey: ['monitor-incidents', node, state, before], queryFn: () => api.get<{ items: Incident[]; next: number }>(`/api/admin/monitoring/incidents?state=${state ?? ''}&before=${before || ''}&node_id=${node ?? ''}`), refetchInterval: 30_000 })
 const windows = useQuery({ queryKey: ['monitor-windows'], queryFn: () => api.get<{ items: MonitorWindow[]; truncated: boolean; now: number }>('/api/admin/monitoring/windows'), refetchInterval: 30_000 })
 const refresh = () => { qc.invalidateQueries({ queryKey: ['monitor-incidents'] }); qc.invalidateQueries({ queryKey: ['monitor-windows'] }); qc.invalidateQueries({ queryKey: ['availability'] }) }
 const silence = useMutation({ mutationFn: ({ id, seconds }: { id: number; seconds: number }) => api.post(`/api/admin/settings/monitor-incidents/${id}/silence`, { seconds }), onSuccess: () => { refresh(); toast.ok(t('common.saved')) }, onError: toast.err })
 const ack = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/monitoring/incidents/${id}/ack`), onSuccess: refresh, onError: toast.err })
 const cancel = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/settings/monitor-windows/${id}`), onSuccess: refresh, onError: toast.err })
 const create = useMutation({ mutationFn: () => {
  const starts = immediate ? Math.floor(Date.now() / 1000) : Math.floor(new Date(start).getTime() / 1000)
  if (!Number.isFinite(starts) || !Number(minutes)) throw new Error(t('alerts.invalidDate'))
  return api.post('/api/admin/settings/monitor-windows', { node_id: scope === 'all' ? null : Number(scope), kind, starts_at: immediate ? 0 : starts, ends_at: starts + Number(minutes) * 60, note })
 }, onSuccess: () => { refresh(); setNote(''); toast.ok(t('common.saved')) }, onError: toast.err })
 const options = nodes.map((n) => ({ value: String(n.id), label: n.name }))
 return <Stack>
  <Card><Stack gap="sm"><Title order={4}>{t('alerts.title')}</Title><Text size="sm" c="dimmed">{t('alerts.hint')}</Text>
   <Group grow align="flex-start"><Select label={t('alerts.node')} placeholder={t('alerts.allNodes')} clearable searchable data={options} value={node} onChange={(v) => { setNode(v); setBefore(0) }} /><Select label={t('alerts.state')} value={state} onChange={(v) => { setState(v); setBefore(0) }} allowDeselect={false} data={['open', 'acknowledged', 'resolved', ''].map((value) => ({ value, label: t(`alerts.states.${value || 'all'}`) }))} /></Group>
   {q.isLoading && <Loader size="sm" />}{q.isError && <Alert color="red">{t('alerts.error')}</Alert>}
   <Table.ScrollContainer minWidth={850}><Table striped><Table.Thead><Table.Tr>{['node', 'kind', 'state', 'started', 'ended', 'value', 'ack'].map((k) => <Table.Th key={k}>{t(`alerts.${k}`)}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>{q.data?.items.map((i) => <Table.Tr key={i.id}>
    <Table.Td><Text component={Link} to={`/nodes/${i.node_id}`} size="sm">{i.node}</Text>{admin && <Button component={Link} to={`/nodes/${i.node_id}#diagnostics`} size="compact-xs" variant="subtle">{t('diagnostics.title')}</Button>}</Table.Td><Table.Td>{i.kind.startsWith('network_') ? `${t(i.kind.startsWith('network_loss') ? 'insights.packetLoss' : 'insights.latency')} · ${i.kind.split(':').slice(1, 3).join(' / ')}` : i.kind.startsWith('route_') ? `${t('insights.routeWatch')} · ${i.kind.slice(6).toUpperCase()}` : t(`alerts.kinds.${i.kind}`)}</Table.Td>
    <Table.Td>{i.silenced && <Badge variant="outline" mb={4} color="gray">{t('insights.silence')}{i.silence_until ? ` · ${date(i.silence_until)}` : ''}</Badge>}<Badge color={i.ended_at ? 'gray' : i.acknowledged_at ? 'orange' : 'red'}>{t(`alerts.states.${i.ended_at ? 'resolved' : i.acknowledged_at ? 'acknowledged' : 'open'}`)}</Badge>{i.resolution && <Text size="xs" c="dimmed">{t(`alerts.resolutions.${i.resolution}`)}</Text>}</Table.Td>
    <Table.Td><Text size="xs">{date(i.started_at)}</Text></Table.Td><Table.Td><Text size="xs">{i.ended_at ? date(i.ended_at) : '—'}</Text></Table.Td><Table.Td><Text size="xs">{i.value.toFixed(1)} / {i.threshold.toFixed(1)} {i.kind.startsWith('network_latency') ? 'ms' : i.kind.startsWith('route_') ? '' : i.kind === 'offline' ? 's' : i.kind === 'dns' ? t('dnsHealth.checks') : '%'}</Text></Table.Td>
    <Table.Td>{admin && <Select w={150} mb={4} aria-label={t('insights.silence')} placeholder={t('insights.silence')} value={null} disabled={silence.isPending} data={[86400, 259200, 604800, 0, -1].map(seconds => ({ value: String(seconds), label: t(`insights.silenceModes.${seconds}`) }))} onChange={v => v !== null && silence.mutate({ id: i.id, seconds: Number(v) })} />}{i.acknowledged_at ? <><Text size="xs">{i.acknowledged_by}</Text><Text size="xs" c="dimmed">{date(i.acknowledged_at)}</Text></> : !i.ended_at ? <Button size="compact-xs" variant="light" loading={ack.isPending && ack.variables === i.id} onClick={() => ack.mutate(i.id)}>{t('alerts.ack')}</Button> : '—'}</Table.Td>
   </Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
   {!q.isLoading && q.data?.items.length === 0 && <Text c="dimmed">{t('alerts.empty')}</Text>}
   <Group justify="flex-end">{before > 0 && <Button variant="subtle" onClick={() => setBefore(0)}>{t('alerts.newest')}</Button>}{!!q.data?.next && <Button variant="light" onClick={() => setBefore(q.data!.next)}>{t('alerts.older')}</Button>}</Group>
  </Stack></Card>
  {node && <AvailabilityCard nodeID={Number(node)} />}
  <Card><Stack gap="sm"><Title order={4}>{t('alerts.windows')}</Title><Text size="sm" c="dimmed">{t('alerts.windowHint')}</Text>
   {admin && <form onSubmit={(e) => { e.preventDefault(); create.mutate() }}><Stack gap="sm"><SimpleGrid cols={{ base: 1, sm: 2 }}><Select label={t('alerts.scope')} value={scope} onChange={setScope} allowDeselect={false} searchable data={[{ value: 'all', label: t('alerts.allNodes') }, ...options]} /><Select label={t('alerts.windowType')} value={kind} onChange={setKind} allowDeselect={false} data={['maintenance', 'silence'].map((value) => ({ value, label: t(`alerts.windowKinds.${value}`) }))} /></SimpleGrid>
    <Switch label={t('alerts.startNow')} checked={immediate} onChange={(e) => setImmediate(e.currentTarget.checked)} />
    <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput type="datetime-local" label={t('alerts.starts')} description={t('alerts.localTime')} disabled={immediate} required={!immediate} value={start} onChange={(e) => setStart(e.currentTarget.value)} /><NumberInput label={t('alerts.duration')} description={t('alerts.durationHint')} min={1} max={43200} required value={minutes} onChange={setMinutes} /></SimpleGrid>
    <TextInput label={t('alerts.note')} maxLength={512} value={note} onChange={(e) => setNote(e.currentTarget.value)} /><Group justify="flex-end"><Button type="submit" loading={create.isPending}>{t('alerts.schedule')}</Button></Group>
   </Stack></form>}
   {windows.isLoading && <Loader size="sm" />}{windows.isError && <Alert color="red">{t('alerts.error')}</Alert>}{windows.data?.truncated && <Alert color="yellow">{t('alerts.truncated')}</Alert>}
   <Table.ScrollContainer minWidth={700}><Table striped><Table.Thead><Table.Tr>{['scope', 'windowType', 'starts', 'ended', 'state', 'note'].map((k) => <Table.Th key={k}>{t(`alerts.${k}`)}</Table.Th>)}<Table.Th /></Table.Tr></Table.Thead><Table.Tbody>{windows.data?.items.map((w) => {
    const status = w.canceled_at ? 'canceled' : (windows.data!.now < w.starts_at ? 'scheduled' : windows.data!.now >= w.ends_at ? 'expired' : 'active')
    return <Table.Tr key={w.id}><Table.Td>{w.node_id == null ? t('alerts.allNodes') : nodes.find((n) => n.id === w.node_id)?.name ?? `#${w.node_id}`}</Table.Td><Table.Td>{t(`alerts.windowKinds.${w.kind}`)}</Table.Td><Table.Td><Text size="xs">{date(w.starts_at)}</Text></Table.Td><Table.Td><Text size="xs">{date(w.canceled_at ? Math.min(w.ends_at, w.canceled_at) : w.ends_at)}</Text></Table.Td><Table.Td><Badge color={status === 'active' ? 'blue' : 'gray'}>{t(`alerts.windowsStates.${status}`)}</Badge></Table.Td><Table.Td maw={240}><Text size="xs" style={{ overflowWrap: 'anywhere' }}>{w.note}</Text></Table.Td><Table.Td>{admin && (status === 'scheduled' || status === 'active') && <Button size="compact-xs" color="red" variant="subtle" loading={cancel.isPending && cancel.variables === w.id} onClick={() => cancel.mutate(w.id)}>{t('alerts.cancel')}</Button>}</Table.Td></Table.Tr>
   })}</Table.Tbody></Table></Table.ScrollContainer>
  </Stack></Card>
 </Stack>
}
