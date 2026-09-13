import { Badge, Box, Card, Container, Group, Progress, SimpleGrid, Stack, Text, Title, Tooltip } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { IconServer, IconArrowUp, IconArrowDown } from '@tabler/icons-react'
import { useState } from 'react'
import { bytes, flag, getJSON, pct, rate, uptime, type Node, type Snapshot } from './lib/api'
import { Spark } from './components/Spark'
import { NodeDetail } from './components/NodeDetail'

// The public status page: one card per node, live values, sparklines and
// the monthly traffic bar. Polls the snapshot at the beat interval.
export default function App() {
  const snap = useQuery({ queryKey: ['probe'], queryFn: () => getJSON<Snapshot>('/api/probe'), refetchInterval: (q) => Math.max(3, q.state.data?.beat_seconds ?? 10) * 1000, retry: false })
  const [open, setOpen] = useState<Node | null>(null)
  if (snap.isError) {
    const code = (snap.error as Error).message
    return <Container size="sm" py={80}><Card className="glass" bg="transparent" withBorder={false}><Title order={3}>{code === '401' ? 'Sign in to view the status page' : code === '403' ? 'Staff only' : 'Status page is off'}</Title>{code === '401' && <Text mt="sm"><a href="/portal/login">Sign in</a></Text>}</Card></Container>
  }
  const s = snap.data
  if (!s) return null
  const online = s.nodes.filter((n) => n.online).length
  const totalUp = s.nodes.reduce((a, n) => a + (n.host?.net_up ?? 0), 0)
  const totalDown = s.nodes.reduce((a, n) => a + (n.host?.net_down ?? 0), 0)
  document.title = s.title
  return (
    <Box pos="relative" style={{ zIndex: 1 }}>
      <div className="site-bg" />
      <Container size="lg" py="md">
        <Group justify="space-between">
          <Group gap="xs">{s.logo && <img src={s.logo} alt="" style={{ height: 28 }} />}<Text fw={800} fz="xl" className="gradient-text">{s.title}</Text></Group>
          <Group gap="md">
            <Badge variant="light" color="teal" size="lg" leftSection={<span className="live-dot" />}>{online} / {s.nodes.length} online</Badge>
            <Text size="sm" c="dimmed"><IconArrowUp size={14} /> {rate(totalUp)} <IconArrowDown size={14} /> {rate(totalDown)}</Text>
          </Group>
        </Group>
      </Container>
      <Container size="lg" pb={60}>
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
          {s.nodes.map((n) => <NodeCard key={n.id} n={n} onOpen={() => setOpen(n)} />)}
        </SimpleGrid>
        {s.nodes.length === 0 && <Text c="dimmed" ta="center" py={60}>No servers to show yet.</Text>}
      </Container>
      <NodeDetail node={open} onClose={() => setOpen(null)} />
    </Box>
  )
}

function NodeCard({ n, onOpen }: { n: Node; onOpen: () => void }) {
  const h = n.host
  const cpu = h?.cpu_percent ?? 0
  const mem = pct(h?.mem_used, h?.mem_total)
  const disk = pct(h?.disk_used, h?.disk_total)
  const tr = n.traffic
  const trafficPct = tr.limit > 0 ? Math.min(100, Math.round((tr.used / tr.limit) * 100)) : 0
  const carriers = (h?.pings ?? []).filter((p) => p.task_id === 0)
  return (
    <Card className="glass" bg="transparent" withBorder={false} style={{ cursor: 'pointer', opacity: n.online ? 1 : 0.6 }} onClick={onOpen}>
      <Group justify="space-between" wrap="nowrap">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
          <Text fz="lg">{flag(n.info.region) || <IconServer size={18} />}</Text>
          <div style={{ minWidth: 0 }}>
            <Text fw={700} truncate>{n.name}</Text>
            <Text size="xs" c="dimmed" truncate>{[n.info.provider, h?.info?.os, h?.uptime ? uptime(h.uptime) : ''].filter(Boolean).join(' · ')}</Text>
          </div>
        </Group>
        <Badge size="sm" color={n.online ? 'teal' : 'red'} variant={n.online ? 'light' : 'filled'}>{n.online ? 'UP' : 'DOWN'}</Badge>
      </Group>
      <SimpleGrid cols={3} spacing="xs" mt="sm">
        <Meter label="CPU" value={cpu} />
        <Meter label="MEM" value={mem} sub={bytes(h?.mem_total ?? 0)} />
        <Meter label="DISK" value={disk} sub={bytes(h?.disk_total ?? 0)} />
      </SimpleGrid>
      <Group justify="space-between" mt="sm" gap="xs">
        <Text size="xs" c="dimmed"><IconArrowUp size={12} /> {rate(h?.net_up ?? 0)} <IconArrowDown size={12} /> {rate(h?.net_down ?? 0)}</Text>
        <Text size="xs" c="dimmed">load {h?.load1?.toFixed(2) ?? '—'}</Text>
      </Group>
      <Spark values={n.recent.map((r) => r.down)} color="#f59e0b" height={28} />
      {carriers.length > 0 && (
        <Group gap="sm" mt="xs">
          {carriers.map((p) => <Tooltip key={p.name} label={`${p.name} loss ${p.loss?.toFixed(0) ?? 0}%`}><Text size="xs" c={p.latency_ms < 0 ? 'red' : p.latency_ms > 200 ? 'orange' : 'dimmed'}>{p.name} {p.latency_ms < 0 ? '×' : `${Math.round(p.latency_ms)}ms`}</Text></Tooltip>)}
        </Group>
      )}
      {tr.limit > 0 ? (
        <Stack gap={2} mt="xs">
          <Group justify="space-between"><Text size="xs" c="dimmed">Traffic</Text><Text size="xs" c="dimmed">{bytes(tr.used)} / {bytes(tr.limit)}</Text></Group>
          <Progress value={trafficPct} size="sm" color={trafficPct > 90 ? 'red' : trafficPct > 70 ? 'orange' : 'teal'} />
        </Stack>
      ) : <Text size="xs" c="dimmed" mt="xs">Traffic this period {bytes(tr.used)}</Text>}
      {n.info.expires_at && <Text size="xs" c="dimmed" mt={4}>Expires {n.info.expires_at}{n.info.price ? ` · ${n.info.price}` : ''}</Text>}
    </Card>
  )
}

function Meter({ label, value, sub }: { label: string; value: number; sub?: string }) {
  return (
    <div>
      <Group justify="space-between" gap={4}><Text size="xs" c="dimmed">{label}</Text><Text size="xs" fw={600}>{Math.round(value)}%</Text></Group>
      <Progress value={value} size="xs" color={value > 90 ? 'red' : value > 70 ? 'orange' : 'cyan'} />
      {sub && <Text size="xs" c="dimmed" mt={2}>{sub}</Text>}
    </div>
  )
}
