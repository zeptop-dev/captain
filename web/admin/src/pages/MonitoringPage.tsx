import { DNSHealthCard } from '../components/DNSHealthCard'
import { Alert, Badge, Button, Card, Checkbox, Group, Loader, MultiSelect, Select, SimpleGrid, Stack, Table, Tabs, Text, TextInput, Title } from '@mantine/core'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { bytes } from '../lib/format'
import { toast } from '../lib/notify'
import type { MonitorNode } from '../lib/monitoring'
import { MonitoringAlerts } from '../components/MonitoringAlerts'
import { ResourceHistoryCard } from '../components/ResourceHistoryCard'
import { useURLChoice } from '../lib/use-url-choice'

export default function MonitoringPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [tab, setTab] = useURLChoice('tab', ['nodes', 'alerts', 'dns'], 'nodes')
  const q = useQuery({ queryKey: ['monitoring'], queryFn: () => api.get<MonitorNode[]>('/api/admin/monitoring'), refetchInterval: 10_000 })
  const [search, setSearch] = useState(''), [group, setGroup] = useState<string | null>(null), [status, setStatus] = useState<string | null>(null)
  const [sort, setSort] = useState<string | null>('name'), [selected, setSelected] = useState<string[]>([])
  const [editing, setEditing] = useState<number | null>(null), [newGroup, setNewGroup] = useState('')
  const saveGroup = useMutation({ mutationFn: () => api.put(`/api/admin/nodes/${editing}/monitor-group`, { group: newGroup }), onSuccess: () => { setEditing(null); qc.invalidateQueries({ queryKey: ['monitoring'] }); qc.invalidateQueries({ queryKey: ['node-layout'] }); qc.invalidateQueries({ queryKey: ['nodes'] }); qc.invalidateQueries({ queryKey: ['node'] }); toast.ok(t('common.saved')) }, onError: toast.err })
  const nodes = q.data ?? []
  const val = (n: MonitorNode, metric: string): number | null => {
    const h = n.host
    if (!n.online || !h || n.stale) return null
    if (metric === 'cpu') return h.valid?.cpu === false ? null : h.cpu_percent ?? 0
    if (metric === 'memory') return h.valid?.memory === false || !h.mem_total ? null : 100 * (h.mem_used ?? 0) / h.mem_total
    return h.valid?.network === false ? null : (h.net_up ?? 0) + (h.net_down ?? 0)
  }
  const filtered = nodes.filter((n) => `${n.name} ${n.info.region ?? ''} ${n.info.provider ?? ''} ${n.group}`.toLowerCase().includes(search.toLowerCase()) && (!group || n.group === group) && (!status || (status === 'online' ? n.online : status === 'offline' ? n.paired && !n.online : !n.paired))).sort((a, b) => sort === 'name' ? a.name.localeCompare(b.name) : (val(b, sort ?? 'cpu') ?? -1) - (val(a, sort ?? 'cpu') ?? -1))
  const format = (n: MonitorNode, metric: string) => { const x = val(n, metric); return x == null ? '—' : metric === 'network' ? `${bytes(x)}/s` : `${x.toFixed(1)}%` }
  return <Stack>
    <Title order={2}>{t('monitoring.title')}</Title>
    <Text c="dimmed">{t('monitoring.hint')}</Text>
    <Tabs value={tab} onChange={value => value && setTab(value)} keepMounted={false}><Tabs.Panel value="nodes" pt="md"><Stack>
    {q.isLoading && <Loader />}{q.isError && <Alert color="red">{t('monitoring.loadError')}</Alert>}
    <SimpleGrid cols={{ base: 1, sm: 3 }}>{['online', 'offline', 'unpaired'].map((key) => <Card key={key}><Text size="sm" c="dimmed">{t(`monitoring.${key}`)}</Text><Text size="xl" fw={700}>{nodes.filter((n) => key === 'online' ? n.online : key === 'offline' ? n.paired && !n.online : !n.paired).length}</Text></Card>)}</SimpleGrid>
    <Card><Stack>
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
        <TextInput label={t('monitoring.search')} value={search} onChange={(e) => setSearch(e.currentTarget.value)} />
        <Select label={t('monitoring.group')} clearable searchable data={[...new Set(nodes.map((n) => n.group).filter(Boolean))].sort()} value={group} onChange={setGroup} />
        <Select label={t('monitoring.status')} clearable data={['online', 'offline', 'unpaired'].map((k) => ({ value: k, label: t(`monitoring.${k}`) }))} value={status} onChange={setStatus} />
        <Select label={t('monitoring.sort')} data={['name', 'cpu', 'memory', 'network'].map((k) => ({ value: k, label: t(`monitoring.sorts.${k}`) }))} value={sort} onChange={setSort} allowDeselect={false} />
      </SimpleGrid>
      <Table.ScrollContainer minWidth={780}><Table highlightOnHover><Table.Thead><Table.Tr>{['select', 'node', 'group', 'status', 'cpu', 'memory', 'network', 'sampled'].map((k) => <Table.Th key={k}>{t(`monitoring.columns.${k}`)}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>
        {filtered.map((n) => <Table.Tr key={n.id}>
          <Table.Td><Checkbox aria-label={`${t('monitoring.compare')} ${n.name}`} checked={selected.includes(String(n.id))} disabled={!selected.includes(String(n.id)) && selected.length >= 4} onChange={(e) => setSelected(e.currentTarget.checked ? [...selected, String(n.id)] : selected.filter((id) => id !== String(n.id)))} /></Table.Td>
          <Table.Td><Text component={Link} to={`/nodes/${n.id}`} fw={600}>{n.name}</Text><Text size="xs" c="dimmed">{[n.info.region, n.info.provider].filter(Boolean).join(' · ')}</Text></Table.Td>
          <Table.Td><Button size="compact-xs" variant="subtle" onClick={() => { setEditing(n.id); setNewGroup(n.group) }}>{n.group || t('monitoring.ungrouped')}</Button></Table.Td>
          <Table.Td><Badge color={!n.paired ? 'gray' : n.online ? 'teal' : 'red'}>{t(`monitoring.${!n.paired ? 'unpaired' : n.online ? 'online' : 'offline'}`)}</Badge></Table.Td>
          <Table.Td>{format(n, 'cpu')}</Table.Td><Table.Td>{format(n, 'memory')}</Table.Td><Table.Td>{format(n, 'network')}</Table.Td>
          <Table.Td><Text size="xs">{n.sampled_at ? new Date(n.sampled_at * 1000).toLocaleString() : '—'}</Text></Table.Td>
        </Table.Tr>)}
      </Table.Tbody></Table></Table.ScrollContainer>
      {filtered.length === 0 && !q.isLoading && <Text c="dimmed">{t('monitoring.noNodes')}</Text>}
      {editing != null && <Group align="flex-start"><TextInput label={`${t('monitoring.group')} · ${nodes.find((n) => n.id === editing)?.name ?? ''}`} description={t('monitoring.groupHint')} maxLength={128} value={newGroup} onChange={(e) => setNewGroup(e.currentTarget.value)} /><Stack gap="xs" pt={25}><Button size="xs" loading={saveGroup.isPending} onClick={() => saveGroup.mutate()}>{t('common.save')}</Button><Button size="xs" variant="subtle" onClick={() => setEditing(null)}>{t('common.cancel')}</Button></Stack></Group>}
      <MultiSelect label={t('monitoring.compare')} description={t('monitoring.compareHint')} maxValues={4} searchable clearable value={selected} onChange={setSelected} data={nodes.map((n) => ({ value: String(n.id), label: n.name }))} />
    </Stack></Card>
    {selected.length > 0 && <ResourceHistoryCard nodes={selected.map((id) => nodes.find((n) => String(n.id) === id)).filter((n): n is MonitorNode => !!n)} />}
  </Stack></Tabs.Panel><Tabs.Panel value="alerts" pt="md"><MonitoringAlerts nodes={nodes} /></Tabs.Panel><Tabs.Panel value="dns" pt="md"><DNSHealthCard /></Tabs.Panel></Tabs>
  </Stack>
}
