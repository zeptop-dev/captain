import { NodeLayoutEditor } from '../components/NodeLayoutEditor'
import { HostTrafficSummary } from '../components/HostTrafficSummary'
import { nodeLayoutQuery } from '../lib/node-layout'
import { Badge, Button, Card, Code, Group, Modal, Stack, Table, Text, TextInput, Anchor, ActionIcon, Tooltip, Select, Switch } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconArrowUp, IconArrowBackUp } from '@tabler/icons-react'
import { modals } from '@mantine/modals'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Node, type SystemUpdate } from '../lib/api'
import { ago, bytes } from '../lib/format'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'
import { useURLChoice } from '../lib/use-url-choice'
import { NodeDomainFields } from '../components/NodeDomainFields'
import { useNodeDomainCheck } from '../lib/use-node-domain-check'
import { useAuth } from '../lib/auth'

export function PairCodeBox({ code, mode = 'full' }: { code: string; mode?: 'full' | 'monitor' }) {
  const { t } = useTranslation()
  const origin = window.location.origin
  const install = `curl -fsSL "${origin}/api/agent/install.sh?pair=${code}" | sh`
  const docker = `${install} -s -- --mode docker --web-upgrade`
  const snippet = `${mode === 'monitor' ? 'node_mode: monitor\n' : ''}panel:\n  driver: captain\n  captain:\n    url: ${origin}\n    pair_code: ${code}`
  return (
    <Stack gap="xs">
      <Text size="sm" c="dimmed">{t('nodes.pairHint')}</Text>
      <Text size="xs" fw={600}>{t('nodes.installCmd')}</Text>
      <Group align="flex-start" gap="xs"><Code block style={{ flex: 1, wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{install}</Code><Copy value={install} /></Group>
      <Text size="xs" fw={600}>{t('nodes.dockerCmd')}</Text>
      <Group align="flex-start" gap="xs"><Code block style={{ flex: 1, wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{docker}</Code><Copy value={docker} /></Group>
      <Text size="xs" c="dimmed" mt="xs">{t('nodes.manualHint')}</Text>
      <Group gap="xs"><Code px="md" py={4}>{code}</Code><Copy value={code} /><Text size="xs" c="dimmed" ml="sm">{t('nodes.configSnippet')}</Text><Copy value={snippet} /></Group>
    </Stack>
  )
}

export function NodeStatus({ n }: { n: Node }) {
  const { t } = useTranslation()
  if (!n.paired) return <Badge color="gray">{t('nodes.unpaired')}</Badge>
  return n.online ? <Badge color="teal">{t('nodes.online')}</Badge> : <Badge color="red">{t('nodes.offline')}</Badge>
}

export default function NodesPage() {
  const { t } = useTranslation()
  const isAdmin = useAuth().me?.role === 'admin'
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes'), refetchInterval: 15_000 })
  const layout = useQuery(nodeLayoutQuery)
  const [organizing, setOrganizing] = useState(false)
  const [nodeGroup, setNodeGroup] = useState<string | null>(null)
  const [status, setStatus] = useURLChoice('status', ['all', 'offline', 'unpaired', 'doctor', 'monitor'], 'all')
  const filtered = (q.data ?? []).filter(n => nodeGroup === null || (n.monitor_group ?? '') === nodeGroup).filter(n => status === 'all' || (status === 'offline' && n.paired && !n.online) || (status === 'unpaired' && !n.paired) || (status === 'doctor' && n.paired && n.doctor_fail) || (status === 'monitor' && n.mode === 'monitor'))
  const [opened, { open, close }] = useDisclosure()
  const [created, setCreated] = useState<Node | null>(null)
  const form = useForm({ initialValues: { Name: '', AutoIPv4: true, AutoIPv6: true, PublicAddr: '', InternalAddr: '', V6Addr: '', Domain: '', DomainShared: false, MonitorURL: '', Mode: 'full' } })
  const domainCheck = useNodeDomainCheck(form.values.Domain, form.values.DomainShared, undefined, opened && !created)
  const sys = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), enabled: isAdmin, staleTime: 10 * 60_000, retry: false })
  const rollback = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/nodes/${id}/jobs`, { kind: 'rollback', params: {} }), onSuccess: () => { toast.ok(t('nodes.rollbackQueued')); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  const upgrade = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/nodes/${id}/upgrade`, {}), onSuccess: () => { toast.ok(t('nodes.upgradeQueued')); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  const upgradeAll = useMutation({ mutationFn: () => api.post<{ nodes: number; upgrade_to: string }>('/api/admin/nodes/upgrade-all', {}), onSuccess: (r) => { toast.ok(t('nodes.upgradeAllQueued', { count: r.nodes, version: r.upgrade_to })); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  const outdated = (q.data ?? []).filter((n) => n.outdated && n.paired).length
  const create = useMutation({
    mutationFn: (v: typeof form.values) => api.post<Node & { dns?: DNSResult[] }>('/api/admin/nodes', { ...v, PublicAddr: v.AutoIPv4 ? '' : v.PublicAddr, V6Addr: v.AutoIPv6 ? '' : v.V6Addr }),
    onSuccess: (n) => { setCreated(n); form.reset(); qc.invalidateQueries({ queryKey: ['nodes'] }); qc.invalidateQueries({ queryKey: ['dns-health'] }); qc.invalidateQueries({ queryKey: ['dns-history'] }); dnsToast(n.dns) },
    onError: (e) => { toast.err(e); qc.invalidateQueries({ queryKey: ['node-domain-check'] }) },
  })
  return (
    <>
      <PageHeader title={t('nodes.title')} subtitle={t('nodes.subtitle')} actions={<>
        {isAdmin && outdated > 0 && <Button variant="light" color="orange" leftSection={<IconArrowUp size={16} />} loading={upgradeAll.isPending} onClick={() => modals.openConfirmModal({ title: t('nodes.upgradeAll', { count: outdated }), children: <Text size="sm">{t('nodes.upgradeAllConfirm', { count: outdated, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgradeAll', { count: outdated }), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgradeAll.mutate() })}>{t('nodes.upgradeAll', { count: outdated })}</Button>}
        <Button variant="light" disabled={!layout.data || !q.data || layout.isError || q.isError} loading={layout.isFetching} onClick={() => { void layout.refetch().then(r => { if (r.data && !r.isError) setOrganizing(true); else toast.err(r.error) }) }}>{t('nodeLayout.title')}</Button>
        <Button leftSection={<IconPlus size={16} />} onClick={open}>{t('nodes.create')}</Button>
      </>} />
      <Group align="flex-end" mb="md"><Select style={{ flex: '1 1 200px', minWidth: 0 }} label={t('monitoring.group')} placeholder={t('common.all')} clearable value={nodeGroup} onChange={setNodeGroup} data={[{ value: '', label: t('nodeLayout.ungrouped') }, ...(layout.data?.groups ?? []).map(g => ({ value: g, label: g }))]} />
      <Select style={{ flex: '1 1 200px', minWidth: 0 }} maw={320} label={t('monitoring.status')} value={status} onChange={value => setStatus(value ?? 'all')} allowDeselect={false} data={[{ value: 'all', label: t('common.all') }, { value: 'offline', label: t('nodes.offline') }, { value: 'unpaired', label: t('nodes.unpaired') }, { value: 'doctor', label: t('dashboard.lastCheckFailed') }, { value: 'monitor', label: t('nodeMode.monitor') }]} /></Group>
      <Card p={0}>
        <Table.ScrollContainer minWidth={1150}>
          <Table>
            <Table.Thead style={{ whiteSpace: 'nowrap' }}><Table.Tr>
              <Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('monitoring.group')}</Table.Th><Table.Th>{t('nodes.status')}</Table.Th><Table.Th>{t('nodes.publicAddr')}</Table.Th>
              <Table.Th>{t('nodes.inbounds')}</Table.Th><Table.Th>{t('nodes.hostTraffic')}</Table.Th><Table.Th>{t('nodes.trafficToday')}</Table.Th><Table.Th>{t('nodes.version')}</Table.Th><Table.Th>{t('nodes.lastSeen')}</Table.Th>
            </Table.Tr></Table.Thead>
            <Table.Tbody>
              {filtered.map((n) => (
                <Table.Tr key={n.id}>
                  <Table.Td><Anchor component={Link} to={`/nodes/${n.id}`} fw={600}>{n.name}</Anchor>{n.mode === 'monitor' && <Badge ml="xs" size="xs" color="cyan">{t('nodeMode.monitor')}</Badge>}<Text size="xs" c="dimmed">{n.hostname}</Text></Table.Td>
                  <Table.Td><Text size="sm">{n.monitor_group || t('nodeLayout.ungrouped')}</Text></Table.Td>
                  <Table.Td miw={84}><Group gap={4}><NodeStatus n={n} />{n.cert_problem && <Badge color="red" size="xs" title={t('nodes.certProblem')}>TLS</Badge>}{n.doctor_fail && <Badge color="red" size="xs" variant="light" title={t('nodes.doctorFailHint')}>{t('nodes.doctor')}</Badge>}</Group></Table.Td>
                  <Table.Td><Code>{n.public_addr || '—'}</Code><Text size="xs" c="dimmed">{n.domain}</Text>{(n.domain_shared || n.domain_conflict) && <Tooltip label={t(n.domain_shared ? 'nodes.domainSharedHint' : 'nodes.domainLegacyHint')}><Badge size="xs" color={n.domain_shared ? 'blue' : 'orange'}>{t(n.domain_shared ? 'nodes.domainExternal' : 'nodes.domainDuplicate')}</Badge></Tooltip>}</Table.Td>
                  <Table.Td>{n.inbounds}</Table.Td>
                  <Table.Td><HostTrafficSummary traffic={n.host_traffic} /></Table.Td>
                  <Table.Td>{bytes(n.traffic_today_bytes)}</Table.Td>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Text size="sm">{n.version || '—'}</Text>
                      {n.upgrade_to ? <Badge size="xs" color="blue">{t('nodes.upgrading', { version: n.upgrade_to })}</Badge> : isAdmin && n.outdated && n.paired && <Badge size="xs" color="orange" style={{ cursor: 'pointer' }} onClick={() => modals.openConfirmModal({ title: t('nodes.upgrade'), children: <Text size="sm">{t('nodes.upgradeConfirm', { name: n.name, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgrade'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgrade.mutate(n.id) })}>{t('nodes.outdated', { version: sys.data?.bosun_latest ?? '' })}</Badge>}
                      {isAdmin && n.paired && n.version && !n.upgrade_to && <Tooltip label={t('nodes.rollback')}><ActionIcon size="xs" variant="subtle" color="gray" aria-label={t('nodes.rollback')} onClick={() => modals.openConfirmModal({ title: t('nodes.rollback'), children: <Text size="sm">{t('nodes.rollbackConfirm', { name: n.name, version: n.version })}</Text>, labels: { confirm: t('nodes.rollback'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => rollback.mutate(n.id) })}><IconArrowBackUp size={14} /></ActionIcon></Tooltip>}
                    </Group>
                    <Text size="xs" c="dimmed">{n.platform}</Text>
                  </Table.Td>
                  <Table.Td>{ago(n.last_seen_at)}</Table.Td>
                </Table.Tr>
              ))}
              {q.data && filtered.length === 0 && <Table.Tr><Table.Td colSpan={9}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Card>
      <Text size="xs" c="dimmed" mt="xs">{t('nodes.hostTrafficHint')}</Text>
      <Text size="xs" c="dimmed" mt={4}>{t('nodes.proxyTrafficHint')}</Text>
      {organizing && layout.data && <NodeLayoutEditor initial={layout.data} nodes={q.data ?? []} onSaved={() => setNodeGroup(null)} close={() => setOrganizing(false)} />}
      <Modal opened={opened} onClose={() => { close(); setCreated(null) }} title={created ? t('nodes.pairTitle') : t('nodes.create')} size="lg">
        {created ? <PairCodeBox code={created.pair_code ?? ''} mode={created.mode} /> : (
          <form onSubmit={form.onSubmit((v) => create.mutate(v))}>
            <Stack>
              <Switch label={t('nodeMode.installOnly')} description={t('nodeMode.installHint')} checked={form.values.Mode === 'monitor'} onChange={e => form.setFieldValue('Mode', e.currentTarget.checked ? 'monitor' : 'full')} />
              <TextInput label={t('nodes.name')} placeholder="jp-1" required {...form.getInputProps('Name')} />
              <Text size="sm" c="dimmed">{t('nodeAddresses.createHint')}</Text>
              <Switch label={t('nodeAddresses.autoIPv4')} {...form.getInputProps('AutoIPv4', { type: 'checkbox' })} />
              {!form.values.AutoIPv4 && <TextInput label={t('nodes.publicAddr')} description={t('nodeAddresses.fixedHint')} placeholder="198.51.100.20" {...form.getInputProps('PublicAddr')} />}
              <Switch label={t('nodeAddresses.autoIPv6')} {...form.getInputProps('AutoIPv6', { type: 'checkbox' })} />
              {!form.values.AutoIPv6 && <TextInput label={t('nodes.v6Addr')} description={t('nodeAddresses.fixedHint')} {...form.getInputProps('V6Addr')} />}
              <NodeDomainFields domain={form.values.Domain} shared={form.values.DomainShared} onDomainChange={v => form.setFieldValue('Domain', v)} onSharedChange={v => form.setFieldValue('DomainShared', v)} check={domainCheck} />
              <TextInput label={t('nodes.internalAddr')} description={t('nodeAddresses.internalHint')} {...form.getInputProps('InternalAddr')} />
              <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" disabled={domainCheck.blocked} loading={create.isPending}>{t('common.create')}</Button></Group>
            </Stack>
          </form>
        )}
      </Modal>
    </>
  )
}
