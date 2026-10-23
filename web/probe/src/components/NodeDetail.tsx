import { Alert, Badge, Group, Modal, SegmentedControl, SimpleGrid, Stack, Text } from '@mantine/core'
import { StorageVolumes } from './StorageSummary'
import { AvailabilityView } from './AvailabilityView'
import type { Availability } from '../lib/availability'
import { NetworkQualityTable } from './NetworkQualityTable'
import { NetworkHistoryChart } from './NetworkHistoryChart'
import type { NetworkHistory } from '../lib/network'
import { AreaChart } from '@mantine/charts'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { bytes, getJSON, publicShows, type Node, type StatPoint, type Sample, type Snapshot } from '../lib/api'

const ranges = ['1h', '24h', '7d', '30d']
interface History { res: string; points: (StatPoint | Sample)[]; from: number; to: number; step: number }
function fmtTs(ts: number, range: string) {
  const d = new Date(ts * 1000)
  return range === '1h' || range === '24h' ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString([], { month: 'numeric', day: 'numeric' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit' })
}

export function NodeDetail({ node, snapshot, onClose }: { node: Node | null; snapshot: Snapshot; onClose: () => void }) {
  const { t } = useTranslation()
  const [range, setRange] = useState('24h')
  const show = (section: string) => publicShows(snapshot, section)
  const hist = useQuery({ queryKey: ['history', node?.id, range, snapshot.public_sections], queryFn: () => getJSON<History>(`/api/probe/nodes/${node!.id}/history?range=${range}`), enabled: !!node && show('history'), refetchInterval: 30_000 })
  const pings = useQuery({ queryKey: ['pings', node?.id, range, snapshot.public_sections], queryFn: () => getJSON<Omit<NetworkHistory, 'current'>>(`/api/probe/nodes/${node!.id}/pings?range=${range}`), enabled: !!node && show('history') && show('latency'), refetchInterval: 60_000 })
  const availability = useQuery({ queryKey: ['availability', node?.id, range, snapshot.public_sections], queryFn: () => getJSON<Availability>(`/api/probe/nodes/${node!.id}/availability?range=${range}`), enabled: !!node && show('history') && show('availability'), refetchInterval: 30_000 })
  const raw = hist.data?.res === 'raw'
  const byBucket = new Map<number, StatPoint | Sample>()
  const step = hist.data?.step || 60
  for (const p of hist.data?.points ?? []) byBucket.set(Math.floor(('t' in p ? p.t : p.ts) / step) * step, p)
  const sys = []
  if (hist.data) for (let ts = Math.floor(hist.data.from / step) * step; ts <= hist.data.to; ts += step) {
    const p = byBucket.get(ts), point = p as StatPoint | undefined, sample = p as Sample | undefined
    const v = p?.valid
    sys.push({ t: fmtTs(ts, range), CPU: !p || v?.cpu === false ? null : raw ? sample!.cpu : point!.cpu,
      Mem: !p || v?.memory === false ? null : raw ? sample!.mem : point!.mem_total ? point!.mem_used * 100 / point!.mem_total : null,
      up: !p || v?.network === false ? null : raw ? sample!.up : point!.net_up,
      down: !p || v?.network === false ? null : raw ? sample!.down : point!.net_down })
  }
  const net = sys.map((p) => ({ t: p.t, '↑ Mbps': p.up == null ? null : +((p.up * 8) / 1e6).toFixed(2), '↓ Mbps': p.down == null ? null : +((p.down * 8) / 1e6).toFixed(2) }))
  return <Modal opened={!!node} onClose={onClose} title={node?.name} size="xl" centered>{node && <Stack>
    <Group justify="space-between">
      <Group gap="xs"><Badge color={node.online ? 'teal' : 'red'} variant="light">{t(node.online ? 'page.online' : 'page.offline')}</Badge>
        {node.host?.info?.os && <Text size="sm" c="dimmed">{node.host.info.os}</Text>}
        {node.host?.info?.cpu_model && <Text size="sm" c="dimmed">{node.host.info.cpu_model} × {node.host.info.cpu_cores}</Text>}
      </Group>
      {show('history') && <SegmentedControl size="xs" value={range} onChange={setRange} data={ranges} />}
    </Group>
    <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="xs">
      {show('memory') && <Text size="xs" c="dimmed">{t('page.memory')} {node.host && node.host.valid?.memory !== false ? `${bytes(node.host.mem_used ?? 0)} / ${bytes(node.host.mem_total ?? 0)}` : '—'}</Text>}
      {show('system') && <Text size="xs" c="dimmed">{t('page.load')} {node.host && node.host.valid?.load !== false ? [node.host.load1, node.host.load5, node.host.load15].map((v) => (v ?? 0).toFixed(2)).join(' / ') : '—'}</Text>}
    </SimpleGrid>
    {show('disk') && <><Text size="sm" fw={600}>{t('storage.title')}</Text><StorageVolumes node={node} /></>}
    {show('history') && show('availability') && <><Text size="sm" fw={600}>{t('availability.title')}</Text>{availability.isError && <Alert color="red">{t('availability.error')}</Alert>}{availability.data && <AvailabilityView data={availability.data} />}</>}
    {show('latency') && <NetworkQualityTable samples={node.host?.pings ?? []} />}
    {show('history') && <>
      {(hist.isError || (show('latency') && pings.isError)) && <Alert color="red">{t('page.error')}</Alert>}
      <Text size="xs" c="dimmed">{t('page.gaps')}</Text>
      {(show('cpu') || show('memory')) && <><Text size="sm" fw={600}>{t('page.history')} (%)</Text><AreaChart connectNulls={false} h={180} data={sys} dataKey="t" series={[...(show('cpu') ? [{ name: 'CPU', color: 'cyan.5' }] : []), ...(show('memory') ? [{ name: 'Mem', label: t('page.memory'), color: 'violet.5' }] : [])]} curveType="linear" withDots={false} yAxisProps={{ domain: [0, 100] }} /></>}
      {show('network') && <><Text size="sm" fw={600}>{t('page.network')}</Text><AreaChart connectNulls={false} h={160} data={net} dataKey="t" series={[{ name: '↑ Mbps', color: 'teal.5' }, { name: '↓ Mbps', color: 'orange.5' }]} curveType="linear" withDots={false} /></>}
      {show('latency') && <NetworkHistoryChart history={pings.data} />}
    </>}
  </Stack>}</Modal>
}
