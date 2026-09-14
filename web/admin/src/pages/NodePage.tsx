import { Accordion, ActionIcon, Badge, Button, Card, Code, Group, Modal, Progress, SimpleGrid, Stack, Table, Text, TextInput, Title, Autocomplete } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type DoctorReport, type Ingress, type CertStatus, type Group as UGroup, type Inbound, type Node } from '../lib/api'
import { ago, bytes, when } from '../lib/format'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { InboundForm, toPayload, toValues, type InboundValues } from '../components/InboundForm'
import { NodeStatus, PairCodeBox } from './NodesPage'
import { NodeProbeCard } from '../components/NodeProbeCard'
import { RoutingCard } from '../components/RoutingCard'
import { ForwardsCard } from '../components/ForwardsCard'
import { IngressesCard, ingressPayload } from '../components/IngressesCard'

interface Detail { node: Node; inbounds: Inbound[]; ingresses?: Ingress[]; status: { host: Record<string, number> | null; cores: Record<string, { running: boolean }> | null; certs: CertStatus[] | null; doctor?: DoctorReport | null } | null }

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
    mutationFn: async (v: InboundValues) => {
      // An inline line ingress from the IPLC recipe is created first, then referenced.
      if (v.NewIngress) { const r = await api.post<{ ingress: Ingress; dns?: DNSResult[] }>(`/api/admin/nodes/${id}/ingresses`, ingressPayload(v.NewIngress)); dnsToast(r.dns); v = { ...v, IngressID: String(r.ingress.id), NewIngress: undefined } }
      return editing === 'new' ? api.post(`/api/admin/nodes/${id}/inbounds`, toPayload(v)) : api.patch(`/api/admin/inbounds/${(editing as Inbound).ID}`, toPayload(v))
    },
    onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); invalidate() }, onError: toast.err,
  })
  const del = useMutation({ mutationFn: (ibID: number) => api.del(`/api/admin/inbounds/${ibID}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const repair = useMutation({ mutationFn: () => api.post<{ pair_code: string }>(`/api/admin/nodes/${id}/repair`), onSuccess: (r) => { setPair(r.pair_code); invalidate() }, onError: toast.err })
  const delNode = useMutation({ mutationFn: () => api.del(`/api/admin/nodes/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['nodes'] }); nav('/nodes') }, onError: toast.err })
  const nodeForm = useForm({ initialValues: { Name: '', PublicAddr: '', InternalAddr: '', V6Addr: '', Domain: '', MonitorURL: '' } })
  const domainList = useQuery({ queryKey: ['domains'], queryFn: () => api.get<{ domains: { name: string }[] }>('/api/admin/domains') })
  const saveNode = useMutation({ mutationFn: (v: typeof nodeForm.values) => api.patch<{ ok: boolean; dns?: DNSResult[] }>(`/api/admin/nodes/${id}`, v), onSuccess: (r) => { toast.ok(t('common.saved')); setEditNode(false); invalidate(); dnsToast(r.dns) }, onError: toast.err })

  const d = q.data
  if (!d) return null
  const n = d.node
  const host = d.status?.host
  const pct = (used?: number, total?: number) => (used && total ? Math.round((used / total) * 100) : 0)
  return (
    <>
      <PageHeader title={n.name} subtitle={`${n.hostname || ''} ${n.platform || ''} ${n.version || ''}`.trim()} actions={<>
        <NodeStatus n={n} />
        <Button variant="default" size="xs" leftSection={<IconPencil size={14} />} onClick={() => { nodeForm.setValues({ Name: n.name, PublicAddr: n.public_addr, InternalAddr: n.internal_addr, V6Addr: n.v6_addr, Domain: n.domain ?? '', MonitorURL: n.monitor_url }); setEditNode(true) }}>{t('common.edit')}</Button>
        <Button variant="default" size="xs" onClick={() => modals.openConfirmModal({ title: t('nodes.repair'), children: <Text size="sm">{t('nodes.repairHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => repair.mutate() })}>{t('nodes.repair')}</Button>
        <Button color="red" variant="light" size="xs" leftSection={<IconTrash size={14} />} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('nodes.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => delNode.mutate() })}>{t('common.delete')}</Button>
      </>} />

      {!n.paired && n.pair_code && <Card mb="lg"><Title order={5} mb="sm">{t('nodes.pairTitle')}</Title><PairCodeBox code={n.pair_code} /></Card>}
      {n.paired && <NodeProbeCard nodeID={n.id} />}
      {d.status?.doctor && <DoctorCard report={d.status.doctor} />}
      {d.status?.certs && d.status.certs.length > 0 && (
        <Card mb="lg">
          <Text size="xs" tt="uppercase" c="dimmed" fw={600} mb="xs">{t('nodes.certs')}</Text>
          <Table>
            <Table.Tbody>
              {d.status.certs.map((c) => (
                <Table.Tr key={c.domain}>
                  <Table.Td><Code>{c.domain}</Code></Table.Td>
                  <Table.Td><Badge variant="outline" color="gray">{c.method}</Badge></Table.Td>
                  <Table.Td><Text size="sm">{c.not_after && !c.not_after.startsWith('0001') ? t('nodes.certExpires', { date: when(c.not_after).split(',')[0] }) : '—'}</Text></Table.Td>
                  <Table.Td>{c.error ? <Text size="xs" c="red">{c.error}</Text> : <Badge color="teal">{t('nodes.certOk')}</Badge>}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Card>
      )}

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

      <Card mt="lg" p={0}>
        <Accordion multiple chevronPosition="right" variant="default">
          <Accordion.Item value="ingress"><Accordion.Control><Text size="sm" fw={600}>{t('ingress.title')}</Text><Text size="xs" c="dimmed">{(d.ingresses ?? []).length > 0 ? t('nodes.advIngressCount', { count: (d.ingresses ?? []).length }) : t('nodes.advIngressHint')}</Text></Accordion.Control><Accordion.Panel><IngressesCard embedded nodeID={n.id} ingresses={d.ingresses ?? []} inbounds={d.inbounds} /></Accordion.Panel></Accordion.Item>
          <Accordion.Item value="routing"><Accordion.Control><Text size="sm" fw={600}>{t('routing.title')}</Text><Text size="xs" c="dimmed">{t('nodes.advRoutingHint')}</Text></Accordion.Control><Accordion.Panel><RoutingCard embedded nodeID={n.id} inboundTags={d.inbounds.map((ib) => ib.Tag)} /></Accordion.Panel></Accordion.Item>
          <Accordion.Item value="forwards"><Accordion.Control><Text size="sm" fw={600}>{t('forwards.title')}</Text><Text size="xs" c="dimmed">{t('nodes.advForwardsHint')}</Text></Accordion.Control><Accordion.Panel><ForwardsCard embedded node={n} /></Accordion.Panel></Accordion.Item>
        </Accordion>
      </Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('inbounds.create') : t('common.edit')} size="xl">
        {editing !== null && <InboundForm domain={n.domain} lineOnly={!n.public_addr && !n.domain} ingresses={d.ingresses ?? []} usedPorts={d.inbounds.filter((ib) => editing === 'new' || ib.ID !== (editing as Inbound).ID).map((ib) => ib.Port)} initial={toValues(editing === 'new' ? undefined : editing)} groups={groups.data ?? []} busy={save.isPending} onSubmit={(v) => save.mutate(v)} onCancel={() => setEditing(null)} />}
      </Modal>
      <Modal opened={editNode} onClose={() => setEditNode(false)} title={t('common.edit')}>
        <form onSubmit={nodeForm.onSubmit((v) => saveNode.mutate(v))}><Stack>
          <TextInput label={t('nodes.name')} required {...nodeForm.getInputProps('Name')} />
          <TextInput label={t('nodes.publicAddr')} {...nodeForm.getInputProps('PublicAddr')} />
          <Autocomplete label={t('nodes.domain')} description={t('nodes.domainHint')} placeholder="jp1.example.com" data={(domainList.data?.domains ?? []).map((d) => (nodeForm.values.Domain.includes('.') && !nodeForm.values.Domain.endsWith('.' + d.name) ? `${nodeForm.values.Domain.split('.')[0]}.${d.name}` : d.name))} {...nodeForm.getInputProps('Domain')} />
          <Group grow><TextInput label={t('nodes.internalAddr')} {...nodeForm.getInputProps('InternalAddr')} /><TextInput label={t('nodes.v6Addr')} {...nodeForm.getInputProps('V6Addr')} /></Group>
          <TextInput label={t('nodes.monitorUrl')} description={t('nodes.monitorUrlHint')} placeholder="https://komari.example.com/..." {...nodeForm.getInputProps('MonitorURL')} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditNode(false)}>{t('common.cancel')}</Button><Button type="submit" loading={saveNode.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
      <Modal opened={pair !== null} onClose={() => setPair(null)} title={t('nodes.pairTitle')} size="lg">{pair && <PairCodeBox code={pair} />}</Modal>
    </>
  )
}

// The node's last self-check (bosun >= 0.18 sends one on change or every 30 min).
function DoctorCard({ report }: { report: DoctorReport }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(report.summary.fail > 0 || report.summary.warn > 0)
  const color = (s: string) => (s === 'ok' ? 'teal' : s === 'warn' ? 'orange' : s === 'fail' ? 'red' : 'gray')
  return (
    <Card mb="lg">
      <Group justify="space-between" mb={open ? 'xs' : 0} style={{ cursor: 'pointer' }} onClick={() => setOpen((o) => !o)}>
        <Group gap="sm"><Title order={5}>{t('nodes.doctor')}</Title>
          <Badge color="teal" variant="light" size="xs">{t('nodes.doctorOk')} {report.summary.ok}</Badge>
          {report.summary.warn > 0 && <Badge color="orange" variant="light" size="xs">{t('nodes.doctorWarn')} {report.summary.warn}</Badge>}
          {report.summary.fail > 0 && <Badge color="red" variant="light" size="xs">{t('nodes.doctorFail')} {report.summary.fail}</Badge>}
        </Group>
        <Text size="xs" c="dimmed">{when(report.at)}</Text>
      </Group>
      {open && (
        <Table fz="sm"><Table.Tbody>
          {report.checks.filter((c) => c.status !== 'skip').map((c) => (
            <Table.Tr key={c.id}><Table.Td w={70}><Badge size="xs" color={color(c.status)} variant="light">{c.status}</Badge></Table.Td><Table.Td><Text size="sm">{c.name}</Text></Table.Td><Table.Td><Text size="xs" c="dimmed">{c.detail}</Text></Table.Td></Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Card>
  )
}
