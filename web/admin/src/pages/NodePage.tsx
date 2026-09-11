import { ActionIcon, Badge, Button, Card, Code, Group, Modal, Progress, SimpleGrid, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type Group as UGroup, type Inbound, type Node } from '../lib/api'
import { ago, bytes } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { InboundForm, toPayload, toValues, type InboundValues } from '../components/InboundForm'
import { NodeStatus, PairCodeBox } from './NodesPage'

interface Detail { node: Node; inbounds: Inbound[]; status: { host: Record<string, number> | null; cores: Record<string, { running: boolean }> | null } | null }

export default function NodePage() {
  const { id } = useParams()
  const { t } = useTranslation()
  const qc = useQueryClient()
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['node', id], queryFn: () => api.get<Detail>(`/api/admin/nodes/${id}`), refetchInterval: 10_000 })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const [editing, setEditing] = useState<Inbound | 'new' | null>(null)
  const [editNode, setEditNode] = useState(false)
  const [pair, setPair] = useState<string | null>(null)
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['node', id] }); qc.invalidateQueries({ queryKey: ['nodes'] }) }
  const save = useMutation({
    mutationFn: (v: InboundValues) => editing === 'new' ? api.post(`/api/admin/nodes/${id}/inbounds`, toPayload(v)) : api.patch(`/api/admin/inbounds/${(editing as Inbound).ID}`, toPayload(v)),
    onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); invalidate() }, onError: toast.err,
  })
  const del = useMutation({ mutationFn: (ibID: number) => api.del(`/api/admin/inbounds/${ibID}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const repair = useMutation({ mutationFn: () => api.post<{ pair_code: string }>(`/api/admin/nodes/${id}/repair`), onSuccess: (r) => { setPair(r.pair_code); invalidate() }, onError: toast.err })
  const delNode = useMutation({ mutationFn: () => api.del(`/api/admin/nodes/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['nodes'] }); nav('/nodes') }, onError: toast.err })
  const nodeForm = useForm({ initialValues: { Name: '', PublicAddr: '', InternalAddr: '', V6Addr: '', MonitorURL: '' } })
  const saveNode = useMutation({ mutationFn: (v: typeof nodeForm.values) => api.patch(`/api/admin/nodes/${id}`, v), onSuccess: () => { toast.ok(t('common.saved')); setEditNode(false); invalidate() }, onError: toast.err })

  const d = q.data
  if (!d) return null
  const n = d.node
  const host = d.status?.host
  const pct = (used?: number, total?: number) => (used && total ? Math.round((used / total) * 100) : 0)
  return (
    <>
      <PageHeader title={n.name} subtitle={`${n.hostname || ''} ${n.platform || ''} ${n.version || ''}`.trim()} actions={<>
        <NodeStatus n={n} />
        <Button variant="default" size="xs" leftSection={<IconPencil size={14} />} onClick={() => { nodeForm.setValues({ Name: n.name, PublicAddr: n.public_addr, InternalAddr: n.internal_addr, V6Addr: n.v6_addr, MonitorURL: n.monitor_url }); setEditNode(true) }}>{t('common.edit')}</Button>
        <Button variant="default" size="xs" onClick={() => modals.openConfirmModal({ title: t('nodes.repair'), children: <Text size="sm">{t('nodes.repairHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => repair.mutate() })}>{t('nodes.repair')}</Button>
        <Button color="red" variant="light" size="xs" leftSection={<IconTrash size={14} />} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('nodes.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => delNode.mutate() })}>{t('common.delete')}</Button>
      </>} />

      {!n.paired && n.pair_code && <Card mb="lg"><Title order={5} mb="sm">{t('nodes.pairTitle')}</Title><PairCodeBox code={n.pair_code} /></Card>}

      <SimpleGrid cols={{ base: 1, md: 3 }} mb="lg">
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={600} mb="xs">{t('nodes.host')}</Text>
          {host && host.mem_total ? (
            <Stack gap="xs">
              <div><Group justify="space-between"><Text size="sm">{t('nodes.cpu')}</Text><Text size="sm">{Math.round(host.cpu_percent ?? 0)}%</Text></Group><Progress value={host.cpu_percent ?? 0} size="sm" /></div>
              <div><Group justify="space-between"><Text size="sm">{t('nodes.mem')}</Text><Text size="sm">{bytes(host.mem_used)} / {bytes(host.mem_total)}</Text></Group><Progress value={pct(host.mem_used, host.mem_total)} size="sm" color="violet" /></div>
              <div><Group justify="space-between"><Text size="sm">{t('nodes.disk')}</Text><Text size="sm">{bytes(host.disk_used)} / {bytes(host.disk_total)}</Text></Group><Progress value={pct(host.disk_used, host.disk_total)} size="sm" color="teal" /></div>
            </Stack>
          ) : <Text size="sm" c="dimmed">{t('nodes.noStatus')}</Text>}
        </Card>
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={600} mb="xs">{t('nodes.cores')}</Text>
          <Group gap="xs">{Object.entries(d.status?.cores ?? {}).map(([name, c]) => <Badge key={name} color={c.running ? 'teal' : 'gray'}>{name}</Badge>)}{!d.status?.cores || Object.keys(d.status.cores).length === 0 ? <Text size="sm" c="dimmed">—</Text> : null}</Group>
        </Card>
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={600} mb="xs">{t('nodes.publicAddr')}</Text>
          <Code>{n.public_addr || '—'}</Code>
          <Text size="xs" c="dimmed" mt="sm">{t('nodes.lastSeen')}: {ago(n.last_seen_at)} · {t('nodes.trafficToday')}: {bytes(n.traffic_today_bytes)}</Text>
          {n.monitor_url && <Text size="xs" mt={4}><a href={n.monitor_url} target="_blank" rel="noreferrer">{t('nodes.monitorUrl')}</a></Text>}
        </Card>
      </SimpleGrid>

      <Card p={0}>
        <Group justify="space-between" p="md" pb="xs">
          <div><Text fw={600}>{t('inbounds.title')}</Text><Text size="xs" c="dimmed">{t('inbounds.subtitle')}</Text></div>
          <Button size="xs" leftSection={<IconPlus size={14} />} onClick={() => setEditing('new')}>{t('inbounds.create')}</Button>
        </Group>
        <Table>
          <Table.Thead><Table.Tr><Table.Th>{t('inbounds.tag')}</Table.Th><Table.Th>{t('inbounds.protocol')}</Table.Th><Table.Th>{t('inbounds.port')}</Table.Th><Table.Th>{t('inbounds.core')}</Table.Th><Table.Th>{t('inbounds.group')}</Table.Th><Table.Th>{t('inbounds.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {d.inbounds.map((ib) => (
              <Table.Tr key={ib.ID}>
                <Table.Td><Text fw={600}>{ib.Tag}</Text></Table.Td>
                <Table.Td><Badge>{ib.Protocol}</Badge></Table.Td>
                <Table.Td><Code>{ib.Listen || '::'}:{ib.Port}</Code></Table.Td>
                <Table.Td>{ib.Core || t('inbounds.coreAuto')}</Table.Td>
                <Table.Td>{ib.GroupID ? (groups.data?.find((g) => g.ID === ib.GroupID)?.Name ?? ib.GroupID) : t('inbounds.groupAll')}</Table.Td>
                <Table.Td>{ib.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
                <Table.Td><Group gap={4} justify="flex-end">
                  <ActionIcon variant="subtle" onClick={() => setEditing(ib)}><IconPencil size={16} /></ActionIcon>
                  <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(ib.ID) })}><IconTrash size={16} /></ActionIcon>
                </Group></Table.Td>
              </Table.Tr>
            ))}
            {d.inbounds.length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
          </Table.Tbody>
        </Table>
      </Card>

      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('inbounds.create') : t('common.edit')} size="xl">
        {editing !== null && <InboundForm initial={toValues(editing === 'new' ? undefined : editing)} groups={groups.data ?? []} busy={save.isPending} onSubmit={(v) => save.mutate(v)} onCancel={() => setEditing(null)} />}
      </Modal>
      <Modal opened={editNode} onClose={() => setEditNode(false)} title={t('common.edit')}>
        <form onSubmit={nodeForm.onSubmit((v) => saveNode.mutate(v))}><Stack>
          <TextInput label={t('nodes.name')} required {...nodeForm.getInputProps('Name')} />
          <TextInput label={t('nodes.publicAddr')} {...nodeForm.getInputProps('PublicAddr')} />
          <Group grow><TextInput label={t('nodes.internalAddr')} {...nodeForm.getInputProps('InternalAddr')} /><TextInput label={t('nodes.v6Addr')} {...nodeForm.getInputProps('V6Addr')} /></Group>
          <TextInput label={t('nodes.monitorUrl')} {...nodeForm.getInputProps('MonitorURL')} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditNode(false)}>{t('common.cancel')}</Button><Button type="submit" loading={saveNode.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
      <Modal opened={pair !== null} onClose={() => setPair(null)} title={t('nodes.pairTitle')} size="lg">{pair && <PairCodeBox code={pair} />}</Modal>
    </>
  )
}
