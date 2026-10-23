import { Badge, Card, Group, Progress, SimpleGrid, Stack, Table, Tabs, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { useState, type ReactNode } from 'react'
import { GPUResources } from './GPUResources'
import { bytes } from '../lib/format'
import type { ResourceHost } from '../lib/resources'

const dash = '—'
const amount = (value: number | null | undefined) => value == null ? dash : bytes(value)
const rate = (value: number | null | undefined) => value == null ? dash : `${bytes(value)}/s`
const percent = (value: number | null | undefined) => value == null ? dash : `${value.toFixed(1)}%`
const usage = (used: number | null, total: number | null) => used == null || total == null ? dash : `${bytes(used)} / ${bytes(total)}`

function Rows({ headers, children }: { headers: string[]; children: ReactNode }) {
  return <Table.ScrollContainer minWidth={540}><Table striped highlightOnHover><Table.Thead><Table.Tr>{headers.map((h) => <Table.Th key={h}>{h}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>{children}</Table.Tbody></Table></Table.ScrollContainer>
}

// Kept in sync with Captain's resource card. Detail is visible only to admins.
export function ResourcesCard({ host }: { host?: ResourceHost | null }) {
  const { t } = useTranslation()
  // One clock read per mount keeps render pure; data refetches refresh it.
  const [renderedAt] = useState(() => Date.now())
  const r = host?.resources
  const valid = (key: keyof NonNullable<ResourceHost['valid']>) => !!host && (!host.valid || host.valid[key])
  const empty = <Text c="dimmed" size="sm">{t('resources.unavailable')}</Text>
  const summary = [
    { label: 'CPU', value: valid('cpu') ? percent(host?.cpu_percent ?? 0) : dash },
    { label: t('resources.memory'), value: valid('memory') ? usage(host?.mem_used ?? 0, host?.mem_total ?? 0) : dash },
    { label: 'Swap', value: valid('swap') ? usage(host?.swap_used ?? 0, host?.swap_total ?? 0) : dash },
    { label: t('resources.rootDisk'), value: valid('disk') ? usage(host?.disk_used ?? 0, host?.disk_total ?? 0) : dash },
    { label: t('resources.network'), value: valid('network') ? `↑ ${rate(host?.net_up ?? 0)} · ↓ ${rate(host?.net_down ?? 0)}` : dash },
    { label: t('resources.load'), value: valid('load') ? [host?.load1, host?.load5, host?.load15].map((v) => (v ?? 0).toFixed(2)).join(' / ') : dash },
  ]
  return <Card mb="lg">
    <Group justify="space-between" align="flex-start" mb="sm">
      <div><Title order={5}>{t('resources.title')}</Title><Text c="dimmed" size="xs">{t('resources.hint')}</Text></div>
      {r && <Text c="dimmed" size="xs">{t('resources.sampled')}: {new Date(r.at).toLocaleString()}{renderedAt - r.at > 90_000 && <Badge component="span" ml="xs" color="orange">{t('resources.stale')}</Badge>}</Text>}
    </Group>
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} mb="md">{summary.map((s) => <div key={s.label}><Text size="xs" c="dimmed">{s.label}</Text><Text size="sm" fw={500}>{s.value}</Text></div>)}</SimpleGrid>
    {!r ? <Text size="sm" c="dimmed">{t('resources.legacy')}</Text> : <Tabs defaultValue="network" keepMounted={false}>
      <Tabs.List mb="sm">
        <Tabs.Tab value="network">{t('resources.network')}</Tabs.Tab><Tabs.Tab value="filesystems">{t('resources.filesystems')}</Tabs.Tab><Tabs.Tab value="io">{t('resources.diskIO')}</Tabs.Tab><Tabs.Tab value="cpu">{t('resources.cpuCores')}</Tabs.Tab><Tabs.Tab value="processes">{t('resources.processes')}</Tabs.Tab><Tabs.Tab value="gpu">GPU</Tabs.Tab>
      </Tabs.List>
      <Tabs.Panel value="network">{r.networks == null ? empty : <Rows headers={[t('resources.device'), t('resources.counted'), t('resources.upload'), t('resources.download'), t('resources.totalUp'), t('resources.totalDown')]}>{r.networks.map((n) => <Table.Tr key={n.name}><Table.Td>{n.name}</Table.Td><Table.Td><Badge size="xs" color={n.included ? 'teal' : 'gray'}>{n.included ? t('resources.included') : t('resources.excluded')}</Badge></Table.Td><Table.Td>{rate(n.up_rate)}</Table.Td><Table.Td>{rate(n.down_rate)}</Table.Td><Table.Td>{amount(n.up)}</Table.Td><Table.Td>{amount(n.down)}</Table.Td></Table.Tr>)}</Rows>}<Text size="xs" c="dimmed" mt="xs">{t('resources.networkHint')}</Text></Tabs.Panel>
      <Tabs.Panel value="filesystems">{r.filesystems == null ? empty : <Rows headers={[t('resources.mount'), t('resources.device'), t('resources.space'), t('resources.inodes')]}>{r.filesystems.map((f) => <Table.Tr key={f.mount}><Table.Td>{f.mount}<Text size="xs" c="dimmed">{f.type}</Text></Table.Td><Table.Td>{f.device || dash}</Table.Td><Table.Td>{usage(f.used, f.total)}</Table.Td><Table.Td>{f.inodes_used == null || !f.inodes_total ? dash : `${f.inodes_used.toLocaleString()} / ${f.inodes_total.toLocaleString()} (${(100 * f.inodes_used / f.inodes_total).toFixed(1)}%)`}</Table.Td></Table.Tr>)}</Rows>}</Tabs.Panel>
      <Tabs.Panel value="io">{r.disks == null ? empty : <Rows headers={[t('resources.device'), t('resources.read'), t('resources.write'), t('resources.readIOPS'), t('resources.writeIOPS')]}>{r.disks.map((d) => <Table.Tr key={d.name}><Table.Td>{d.name}</Table.Td><Table.Td>{rate(d.read_rate)}</Table.Td><Table.Td>{rate(d.write_rate)}</Table.Td><Table.Td>{d.read_iops?.toFixed(1) ?? dash}</Table.Td><Table.Td>{d.write_iops?.toFixed(1) ?? dash}</Table.Td></Table.Tr>)}</Rows>}</Tabs.Panel>
      <Tabs.Panel value="cpu">{r.cpus == null ? empty : <SimpleGrid cols={{ base: 2, sm: 4, lg: 6 }}>{r.cpus.map((c) => <Stack key={c.name} gap={4}><Group justify="space-between"><Text size="xs">{c.name}</Text><Text size="xs">{percent(c.percent)}</Text></Group>{c.percent != null && <Progress value={c.percent} size="sm" />}</Stack>)}</SimpleGrid>}</Tabs.Panel>
      <Tabs.Panel value="processes">{r.processes == null ? empty : <Rows headers={[t('resources.process'), 'PID', 'CPU', 'RSS']}>{r.processes.map((p) => <Table.Tr key={p.pid}><Table.Td>{p.name}</Table.Td><Table.Td>{p.pid}</Table.Td><Table.Td>{percent(p.cpu)}</Table.Td><Table.Td>{amount(p.rss)}</Table.Td></Table.Tr>)}</Rows>}<Text size="xs" c="dimmed" mt="xs">{t('resources.processHint')}</Text></Tabs.Panel>
      <Tabs.Panel value="gpu"><GPUResources value={r.gpu} /></Tabs.Panel>
    </Tabs>}
  </Card>
}
