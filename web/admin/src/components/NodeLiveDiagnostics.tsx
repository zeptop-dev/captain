import { LineChart } from '@mantine/charts'
import { Alert, Badge, Button, Card, Group, Loader, SimpleGrid, Stack, Table, Text, Title } from '@mantine/core'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { bytes } from '../lib/format'
import { DiagnosticSession, diagnosticFresh, type DiagnosticLease, type DiagnosticSessionState } from '../lib/diagnostic-session'
import { finiteMetric, liveGraphPoints, LiveSampleWindow, type LiveDiagnostics, type LiveLease } from '../lib/live-diagnostics'

const clock = () => ({ monotonic: performance.now(), wall: Date.now() })
export function NodeLiveDiagnostics({ nodeID }: { nodeID: number }) {
  const { t, i18n } = useTranslation()
  const [state, setState] = useState<DiagnosticSessionState<LiveDiagnostics>>({ phase: 'idle' })
  const [, setNow] = useState(clock)
  const [available, setAvailable] = useState(() => navigator.onLine && document.visibilityState === 'visible')
  const session = useRef<DiagnosticSession<DiagnosticLease, LiveDiagnostics> | null>(null)
  const wanted = useRef(true), failed = useRef(false)
  useEffect(() => {
    let mounted = true
    const sampleWindow = new LiveSampleWindow()
    const base = `/api/admin/node-diagnostics/${nodeID}`
    const normalize = (value: LiveLease): DiagnosticLease => ({ id: value.lease_id, fresh_for_ms: value.ttl_ms })
    const control = new DiagnosticSession<DiagnosticLease, LiveDiagnostics>({
      newID: () => crypto.randomUUID(),
      create: async (id, signal) => {
        sampleWindow.open(id)
        return normalize(await api.post<LiveLease>(`${base}/leases`, { request_id: id }, signal))
      },
      renew: async (id, signal) => normalize(await api.put<LiveLease>(`${base}/leases/${encodeURIComponent(id)}`, {}, signal)),
      // The preallocated request ID also works before POST finishes. Tombstones
      // prevent a late POST from recreating a lease after this DELETE.
      release: id => {
        sampleWindow.close(id)
        // A pagehide cleanup must be allowed to finish after navigation.
        return fetch(`${base}/leases/${encodeURIComponent(id)}`, { method: 'DELETE', credentials: 'same-origin', keepalive: true }).then(response => {
          if (!response.ok) throw new Error('diagnostics release failed')
        })
      },
      read: async (id, signal) => {
        const view = await api.get<LiveDiagnostics>(`${base}?lease_id=${encodeURIComponent(id)}&after=${sampleWindow.after}`, signal)
        if (view.node_id !== nodeID || !Array.isArray(view.samples)) throw new Error('invalid diagnostics response')
        if (view.status === 'expired') throw new Error('lease_expired')
        if (view.status === 'unsupported') throw new ApiError(409, 'diagnostics_unavailable')
        return sampleWindow.accept(id, view)
      },
      clock, changed: value => { if (mounted) { if (value.phase === 'error') failed.current = true; setState(value) } },
      schedule: (fn, ms) => window.setTimeout(fn, ms), cancel: timer => window.clearTimeout(timer as number),
    })
    session.current = control; wanted.current = true; failed.current = false
    const sync = () => {
      const visible = navigator.onLine && document.visibilityState === 'visible'
      setAvailable(visible); setNow(clock())
      if (!visible) control.stop()
      else if (wanted.current && !failed.current) void control.start()
    }
    const leaving = () => control.stop()
    sync()
    document.addEventListener('visibilitychange', sync); window.addEventListener('offline', sync); window.addEventListener('online', sync); window.addEventListener('pagehide', leaving); window.addEventListener('pageshow', sync)
    const timer = window.setInterval(() => setNow(clock()), 500)
    return () => {
      mounted = false; control.stop(); session.current = null; window.clearInterval(timer)
      document.removeEventListener('visibilitychange', sync); window.removeEventListener('offline', sync); window.removeEventListener('online', sync); window.removeEventListener('pagehide', leaving); window.removeEventListener('pageshow', sync)
    }
  }, [nodeID])
  const view = state.data, latest = view?.samples.at(-1)
  const fresh = available && state.phase === 'active' && view?.status === 'live' && !!latest
    && diagnosticFresh(view.fresh_for_ms, state.requestedAt, clock(), true)
  const status = !available ? 'inactive' : state.phase === 'error' ? 'error' : state.phase === 'idle' ? 'stopped' : state.phase === 'starting' ? 'starting' : fresh ? 'live' : view?.status === 'waiting' || !view ? 'waiting' : 'stale'
  const rate = (value?: number | null) => finiteMetric(value) === null ? '—' : `${bytes(value!)}/s`
  const percent = (value?: number | null) => finiteMetric(value) === null ? '—' : `${value!.toLocaleString(i18n.language, { maximumFractionDigits: 1 })}%`
  const memory = latest && finiteMetric(latest.memory_used) !== null && finiteMetric(latest.memory_total) !== null && latest.memory_total! > 0 ? 100 * latest.memory_used! / latest.memory_total! : null
  const errorKey = state.error instanceof ApiError && state.error.status === 409 ? 'unsupported' : state.error instanceof ApiError && state.error.status === 429 ? 'budget' : 'loadError'
  return <Stack gap="lg">
    <Card withBorder><Stack gap="sm">
      <Group justify="space-between"><Title order={4}>{t('observability.live')}</Title><Badge color={fresh ? 'teal' : 'gray'}>{t(`liveDiagnostics.status.${status}`)}</Badge></Group>
      <Text size="sm" c="dimmed">{t('liveDiagnostics.scope')}</Text>
      <Group><Button size="xs" variant="default" disabled={!available || state.phase === 'active' || state.phase === 'starting'} onClick={() => { wanted.current = true; failed.current = false; void session.current?.start() }}>{t('liveDiagnostics.start')}</Button><Button size="xs" variant="subtle" disabled={state.phase !== 'active' && state.phase !== 'starting'} onClick={() => { wanted.current = false; session.current?.stop() }}>{t('liveDiagnostics.stop')}</Button></Group>
      {(state.phase === 'starting' || status === 'waiting') && <Loader size="sm" />}
      {state.phase === 'error' && <Alert color="red">{t(`liveDiagnostics.${errorKey}`)}</Alert>}
      {!available && <Alert color="gray">{t('liveDiagnostics.inactive')}</Alert>}
      <Text size="xs" c="dimmed">{t('liveDiagnostics.baseline')}</Text>
    </Stack></Card>
    <Card withBorder><Stack gap="md">
      <SimpleGrid cols={{ base: 2, sm: 4 }}>
        {[['cpu', fresh ? percent(latest?.cpu_percent) : '—'], ['memory', fresh ? percent(memory) : '—'], ['upload', fresh ? rate(latest?.net_up_bytes_per_second) : '—'], ['download', fresh ? rate(latest?.net_down_bytes_per_second) : '—']].map(([key, value]) => <div key={key}><Text size="xs" c="dimmed">{t(`liveDiagnostics.${key}`)}</Text><Text fw={600} size="lg">{value}</Text></div>)}
      </SimpleGrid>
      <Text size="xs" c="dimmed">{t('liveDiagnostics.sampled')}: {latest ? new Date(latest.sampled_at).toLocaleString(i18n.language) : t('observability.unknown')}</Text>
      {!fresh && <Text size="sm" c="dimmed">{t('liveDiagnostics.noCurrent')}</Text>}
      {!!view?.samples.length && <>
        <LineChart h={180} data={liveGraphPoints(view.samples)} dataKey="index" series={[{ name: 'up', color: 'blue.6', label: t('liveDiagnostics.upload') }, { name: 'down', color: 'teal.6', label: t('liveDiagnostics.download') }]} curveType="linear" connectNulls={false} withXAxis={false} withLegend withTooltip={false} withDots={false} valueFormatter={value => rate(value)} />
        <Text size="xs" c="dimmed">{t('liveDiagnostics.graph')}</Text>
      </>}
    </Stack></Card>
    <Card withBorder><Stack gap="sm">
      <Title order={5}>{t('liveDiagnostics.interfaces')}</Title>
      {fresh && latest?.interfaces.length ? <Table.ScrollContainer minWidth={280}><Table><Table.Thead><Table.Tr><Table.Th>{t('liveDiagnostics.interface')}</Table.Th><Table.Th>{t('liveDiagnostics.download')}</Table.Th><Table.Th>{t('liveDiagnostics.upload')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{latest.interfaces.map(nic => <Table.Tr key={nic.name}><Table.Td style={{ overflowWrap: 'anywhere' }}>{nic.name}</Table.Td><Table.Td>{rate(nic.rx_bytes_per_second)}</Table.Td><Table.Td>{rate(nic.tx_bytes_per_second)}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <Text size="sm" c="dimmed">{t('liveDiagnostics.noCurrent')}</Text>}
      {latest?.truncated && <Text size="xs" c="dimmed">{t('liveDiagnostics.truncated')}</Text>}
    </Stack></Card>
  </Stack>
}
