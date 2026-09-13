import { Badge, Group, Modal, SegmentedControl, SimpleGrid, Stack, Text } from '@mantine/core'
import { AreaChart, LineChart } from '@mantine/charts'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { bytes, getJSON, type Node, type PingPoint, type StatPoint, type Sample } from '../lib/api'

const ranges = ['1h', '24h', '7d', '30d']

function fmtTs(ts: number, range: string) {
  const d = new Date(ts * 1000)
  return range === '1h' || range === '24h' ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString([], { month: 'numeric', day: 'numeric' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit' })
}

// History charts for one node: CPU/memory, network rate, latency per
// probe target. 1h comes from the in-memory ring, longer ranges from the
// aggregated buckets.
export function NodeDetail({ node, onClose }: { node: Node | null; onClose: () => void }) {
  const [range, setRange] = useState('24h')
  const hist = useQuery({ queryKey: ['history', node?.id, range], queryFn: () => getJSON<{ res: string; points: (StatPoint | Sample)[] }>(`/api/probe/nodes/${node!.id}/history?range=${range}`), enabled: !!node, refetchInterval: 30_000 })
  const pings = useQuery({ queryKey: ['pings', node?.id, range], queryFn: () => getJSON<{ points: PingPoint[] }>(`/api/probe/nodes/${node!.id}/pings?range=${range === '1h' ? '24h' : range}`), enabled: !!node, refetchInterval: 60_000 })
  const raw = hist.data?.res === 'raw'
  const sys = (hist.data?.points ?? []).map((p) => raw
    ? { t: fmtTs((p as Sample).t, range), CPU: +(p as Sample).cpu.toFixed(1), Mem: +(p as Sample).mem.toFixed(1), up: (p as Sample).up, down: (p as Sample).down }
    : { t: fmtTs((p as StatPoint).ts, range), CPU: +(p as StatPoint).cpu.toFixed(1), Mem: (p as StatPoint).mem_total ? +((p as StatPoint).mem_used * 100 / (p as StatPoint).mem_total).toFixed(1) : 0, up: (p as StatPoint).net_up, down: (p as StatPoint).net_down })
  const net = sys.map((p) => ({ t: p.t, '↑ Mbps': +((p.up * 8) / 1e6).toFixed(2), '↓ Mbps': +((p.down * 8) / 1e6).toFixed(2) }))
  // Pivot ping points into one row per timestamp with a column per target.
  const names = Array.from(new Set((pings.data?.points ?? []).map((p) => p.name)))
  const byTs = new Map<number, Record<string, number | string | null>>()
  for (const p of pings.data?.points ?? []) {
    const row = byTs.get(p.ts) ?? { t: fmtTs(p.ts, range === '1h' ? '24h' : range) }
    row[p.name] = p.avg_ms < 0 ? null : +p.avg_ms.toFixed(1)
    byTs.set(p.ts, row)
  }
  const latency = Array.from(byTs.entries()).sort((a, b) => a[0] - b[0]).map(([, r]) => r)
  const colors = ['cyan.5', 'violet.5', 'orange.5', 'teal.5', 'pink.5', 'yellow.5']
  return (
    <Modal opened={!!node} onClose={onClose} title={node?.name} size="xl" centered>
      {node && (
        <Stack>
          <Group justify="space-between">
            <Group gap="xs">
              <Badge color={node.online ? 'teal' : 'red'} variant="light">{node.online ? 'online' : 'offline'}</Badge>
              {node.host?.info?.os && <Text size="sm" c="dimmed">{node.host.info.os}</Text>}
              {node.host?.info?.cpu_model && <Text size="sm" c="dimmed">{node.host.info.cpu_model} × {node.host.info.cpu_cores}</Text>}
            </Group>
            <SegmentedControl size="xs" value={range} onChange={setRange} data={ranges} />
          </Group>
          <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="xs">
            <Text size="xs" c="dimmed">Memory {bytes(node.host?.mem_used ?? 0)} / {bytes(node.host?.mem_total ?? 0)}</Text>
            <Text size="xs" c="dimmed">Disk {bytes(node.host?.disk_used ?? 0)} / {bytes(node.host?.disk_total ?? 0)}</Text>
            <Text size="xs" c="dimmed">Load {node.host?.load1?.toFixed(2)} / {node.host?.load5?.toFixed(2)} / {node.host?.load15?.toFixed(2)}</Text>
            <Text size="xs" c="dimmed">TCP {node.host?.tcp} · UDP {node.host?.udp} · Proc {node.host?.processes}</Text>
          </SimpleGrid>
          <Text size="sm" fw={600}>CPU / Memory %</Text>
          <AreaChart h={180} data={sys} dataKey="t" series={[{ name: 'CPU', color: 'cyan.5' }, { name: 'Mem', color: 'violet.5' }]} curveType="monotone" withDots={false} yAxisProps={{ domain: [0, 100] }} />
          <Text size="sm" fw={600}>Network</Text>
          <AreaChart h={160} data={net} dataKey="t" series={[{ name: '↑ Mbps', color: 'teal.5' }, { name: '↓ Mbps', color: 'orange.5' }]} curveType="monotone" withDots={false} />
          {names.length > 0 && <>
            <Text size="sm" fw={600}>Latency (ms)</Text>
            <LineChart h={180} data={latency} dataKey="t" series={names.map((n, i) => ({ name: n, color: colors[i % colors.length] }))} curveType="monotone" withDots={false} connectNulls={false} />
          </>}
        </Stack>
      )}
    </Modal>
  )
}
