import { Badge, Button, Card, Group, Table, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlayerPlay } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { ago } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Tcping { min_ms: number; avg_ms: number; at: number }
interface Entry { id: number; name: string; host: string; port: number; enabled: boolean; tcping?: Tcping }
interface Ping { task_id: number; name: string; latency_ms: number; loss?: number; mbps?: number; at?: number }
interface Node { id: number; name: string; pings?: Ping[]; at?: string }

// Speed test workbench. Left: TCP connect latency from the panel to each
// entry's public address. Right: what the nodes measure themselves
// (carrier latency, tasks, download throughput) via the probe.
export default function SpeedtestPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['speedtest'], queryFn: () => api.get<{ entries: Entry[]; nodes: Node[]; probe_enabled: boolean }>('/api/admin/speedtest'), refetchInterval: 15_000 })
  const ping = useMutation({ mutationFn: (e: Entry) => api.post<Tcping>('/api/admin/speedtest/tcping', { Host: e.host, Port: e.port }), onSuccess: () => qc.invalidateQueries({ queryKey: ['speedtest'] }), onError: toast.err })
  const all = useMutation({ mutationFn: async () => { for (const e of q.data?.entries ?? []) await api.post('/api/admin/speedtest/tcping', { Host: e.host, Port: e.port }) }, onSuccess: () => qc.invalidateQueries({ queryKey: ['speedtest'] }), onError: toast.err })
  const ms = (v: number) => (v < 0 ? <Badge color="red" size="sm">{t('speedtest.unreachable')}</Badge> : <Text span c={v > 300 ? 'orange' : undefined}>{Math.round(v)} ms</Text>)
  return (
    <>
      <PageHeader title={t('speedtest.title')} subtitle={t('speedtest.subtitle')} actions={<Button leftSection={<IconPlayerPlay size={16} />} loading={all.isPending} onClick={() => all.mutate()}>{t('speedtest.pingAll')}</Button>} />
      <Card p={0} mb="lg"><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('speedtest.entry')}</Table.Th><Table.Th>{t('speedtest.address')}</Table.Th><Table.Th>{t('speedtest.tcping')}</Table.Th><Table.Th>{t('speedtest.when')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data?.entries ?? []).map((e) => (
            <Table.Tr key={e.id} style={{ opacity: e.enabled ? 1 : 0.5 }}>
              <Table.Td><Text size="sm" fw={600}>{e.name}</Text></Table.Td>
              <Table.Td><Text size="sm" ff="monospace">{e.host}:{e.port}</Text></Table.Td>
              <Table.Td>{e.tcping ? <Text size="sm">{ms(e.tcping.min_ms)}{e.tcping.min_ms >= 0 && <Text span size="xs" c="dimmed"> · avg {Math.round(e.tcping.avg_ms)}</Text>}</Text> : <Text size="sm" c="dimmed">—</Text>}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{e.tcping ? ago(new Date(e.tcping.at * 1000).toISOString()) : ''}</Text></Table.Td>
              <Table.Td><Button size="compact-xs" variant="light" loading={ping.isPending && ping.variables?.id === e.id} onClick={() => ping.mutate(e)}>{t('speedtest.ping')}</Button></Table.Td>
            </Table.Tr>
          ))}
          {(q.data?.entries ?? []).length === 0 && <Table.Tr><Table.Td colSpan={5}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Text size="sm" fw={600} mb="xs">{t('speedtest.nodeSide')}</Text>
      <Text size="xs" c="dimmed" mb="sm">{q.data?.probe_enabled ? t('speedtest.nodeSideHint') : t('speedtest.probeOff')}</Text>
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('speedtest.node')}</Table.Th><Table.Th>{t('speedtest.results')}</Table.Th><Table.Th>{t('speedtest.when')}</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data?.nodes ?? []).map((n) => (
            <Table.Tr key={n.id}>
              <Table.Td><Text size="sm" fw={600}>{n.name}</Text></Table.Td>
              <Table.Td><Group gap="sm">{(n.pings ?? []).map((p) => <Text key={`${p.task_id}-${p.name}`} size="sm">{p.name}: {p.mbps ? <b>{p.mbps.toFixed(1)} Mbps</b> : ms(p.latency_ms)}{p.loss ? <Text span size="xs" c="dimmed"> · {p.loss.toFixed(0)}% loss</Text> : null}</Text>)}{(n.pings ?? []).length === 0 && <Text size="sm" c="dimmed">—</Text>}</Group></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{n.at ? ago(n.at) : ''}</Text></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table></Card>
    </>
  )
}
