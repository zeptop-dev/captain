import { AgentMigrationDialog } from '../components/AgentMigrationDialog'
import { AgentMigrationProgress } from '../components/AgentMigrationProgress'
import { localizedName } from '../lib/localized-name'
import { NodeLayoutEditor } from '../components/NodeLayoutEditor'
import { HostTrafficSummary } from '../components/HostTrafficSummary'
import { ProxyTrafficSummary } from '../components/ProxyTrafficSummary'
import { nodeLayoutQuery } from '../lib/node-layout'
import { Badge, Button, Card, Checkbox, Code, Group, Modal, Stack, Table, Text, TextInput, Anchor, ActionIcon, Tooltip, Select, Switch } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconArrowUp, IconArrowBackUp } from '@tabler/icons-react'
import { modals } from '@mantine/modals'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Node, type SystemUpdate } from '../lib/api'
import { ago } from '../lib/format'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'
import { useURLChoice } from '../lib/use-url-choice'
import { NodeDomainFields } from '../components/NodeDomainFields'
import { useNodeDomainCheck } from '../lib/use-node-domain-check'
import { useAuth } from '../lib/auth'
import { NodePublicAddresses } from '../components/NodePublicAddresses'
import { AgentConnectionFields } from '../components/AgentConnectionFields'
import { agentInstallCommands } from '../lib/agent-install'

export function PairCodeBox({ code, mode = 'full', agentURL, downloadSource }: { code: string; mode?: 'full' | 'monitor'; agentURL?: string; downloadSource?: string }) {
  return <PairCodeOptions key={JSON.stringify([code, mode, agentURL, downloadSource])} code={code} mode={mode} agentURL={agentURL} downloadSource={downloadSource} />
}

function PairCodeOptions({ code, mode, agentURL, downloadSource }: { code: string; mode: 'full' | 'monitor'; agentURL?: string; downloadSource?: string }) {
  const { t } = useTranslation()
  const [installSystemPackages, setInstallSystemPackages] = useState(downloadSource !== 'captain')
  const { install, docker, snippet } = agentInstallCommands(code, agentURL || window.location.origin, mode, downloadSource, installSystemPackages)
  return (
    <Stack gap="xs">
      <Text size="sm" c="dimmed">{t('nodes.pairHint')}</Text>
      {mode === 'full' && <Switch label={t('nodes.installSystemPackages')} description={t('nodes.installSystemPackagesHint')} checked={installSystemPackages} onChange={e => setInstallSystemPackages(e.currentTarget.checked)} />}
      <Text size="xs" fw={600}>{t('nodes.installCmd')}</Text>
      <Group align="flex-start" gap="xs"><Code block style={{ flex: 1, wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{install}</Code><Copy value={install} /></Group>
      {downloadSource !== 'captain' && <>
        <Text size="xs" fw={600}>{t('nodes.dockerCmd')}</Text>
        <Group align="flex-start" gap="xs"><Code block style={{ flex: 1, wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{docker}</Code><Copy value={docker} /></Group>
      </>}
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
  const { t, i18n } = useTranslation()
  const isAdmin = useAuth().me?.role === 'admin'
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes'), refetchInterval: 15_000 })
  const layout = useQuery(nodeLayoutQuery)
  const [organizing, setOrganizing] = useState(false)
 const [selected, setSelected] = useState<number[]>([])
 const [migrateOpened,setMigrateOpened]=useState(false)
  const [nodeGroup, setNodeGroup] = useState<string | null>(null)
  const [status, setStatus] = useURLChoice('status', ['all', 'offline', 'unpaired', 'doctor', 'monitor'], 'all')
  const filtered = (q.data ?? []).filter(n => nodeGroup === null || (n.monitor_group ?? '') === nodeGroup).filter(n => status === 'all' || (status === 'offline' && n.paired && !n.online) || (status === 'unpaired' && !n.paired) || (status === 'doctor' && n.paired && n.doctor_fail) || (status === 'monitor' && n.mode === 'monitor'))
  const [opened, { open, close }] = useDisclosure()
  const [created, setCreated] = useState<Node | null>(null)
  const form = useForm({ initialValues: { Name: '', AutoIPv4: true, AutoIPv6: true, PublicAddr: '', InternalAddr: '', V6Addr: '', Domain: '', DomainShared: false, MonitorURL: '', Mode: 'full', AgentURL: '', DownloadSource: 'github' } })
  const [domainIncomplete, setDomainIncomplete] = useState(false)
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
        {isAdmin && <Button variant="light" disabled={!selected.length} onClick={()=>setMigrateOpened(true)}>{t('agentEntries.migrate')} ({selected.length})</Button>}
        {isAdmin && outdated > 0 && <Button variant="light" color="orange" leftSection={<IconArrowUp size={16} />} loading={upgradeAll.isPending} onClick={() => modals.openConfirmModal({ title: t('nodes.upgradeAll', { count: outdated }), children: <Text size="sm">{t('nodes.upgradeAllConfirm', { count: outdated, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgradeAll', { count: outdated }), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgradeAll.mutate() })}>{t('nodes.upgradeAll', { count: outdated })}</Button>}
        <Button variant="light" disabled={!layout.data || !q.data || layout.isError || q.isError} loading={layout.isFetching} onClick={() => { void layout.refetch().then(r => { if (r.data && !r.isError) setOrganizing(true); else toast.err(r.error) }) }}>{t('nodeLayout.title')}</Button>
        <Button leftSection={<IconPlus size={16} />} onClick={open}>{t('nodes.create')}</Button>
      </>} />
      <Group align="flex-end" mb="md"><Select style={{ flex: '1 1 200px', minWidth: 0 }} label={t('monitoring.group')} placeholder={t('common.all')} clearable value={nodeGroup} onChange={setNodeGroup} data={[{ value: '', label: t('nodeLayout.ungrouped') }, ...(layout.data?.groups ?? []).map(g => ({ value: g, label: localizedName(i18n.resolvedLanguage || i18n.language, layout.data?.group_names?.[g]?.zh, layout.data?.group_names?.[g]?.en, g) }))]} />
      <Select style={{ flex: '1 1 200px', minWidth: 0 }} maw={320} label={t('monitoring.status')} value={status} onChange={value => setStatus(value ?? 'all')} allowDeselect={false} data={[{ value: 'all', label: t('common.all') }, { value: 'offline', label: t('nodes.offline') }, { value: 'unpaired', label: t('nodes.unpaired') }, { value: 'doctor', label: t('dashboard.lastCheckFailed') }, { value: 'monitor', label: t('nodeMode.monitor') }]} /></Group>
      <Card p={0}>
        <Table.ScrollContainer minWidth={1450}>
          <Table>
            <Table.Thead style={{ whiteSpace: 'nowrap' }}><Table.Tr>
              {isAdmin && <Table.Th><Checkbox aria-label={t('agentEntries.selectAll')} checked={filtered.length>0&&filtered.every(n=>selected.includes(n.id))} indeterminate={filtered.some(n=>selected.includes(n.id))&&!filtered.every(n=>selected.includes(n.id))} onChange={e=>setSelected(e.currentTarget.checked?filtered.map(n=>n.id):[])} /></Table.Th>}<Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('monitoring.group')}</Table.Th><Table.Th>{t('nodes.status')}</Table.Th><Table.Th>{t('nodes.publicAddr')}</Table.Th>
              <Table.Th>{t('nodes.inbounds')}</Table.Th><Table.Th>{t('nodes.hostTraffic')}</Table.Th><Table.Th>{t('nodes.trafficToday')}</Table.Th><Table.Th>{t('nodes.version')}</Table.Th><Table.Th>{t('nodes.lastSeen')}</Table.Th>
            </Table.Tr></Table.Thead>
            <Table.Tbody>
              {filtered.map((n) => (
                <Table.Tr key={n.id}>
                  {isAdmin && <Table.Td><Checkbox aria-label={`${t('agentEntries.select')} ${n.name}`} checked={selected.includes(n.id)} onChange={e=>setSelected(e.currentTarget.checked?[...selected,n.id]:selected.filter(id=>id!==n.id))} /></Table.Td>}
                  <Table.Td miw={220}><Anchor component={Link} to={`/nodes/${n.id}`} fw={600}>{n.name}</Anchor>{n.mode === 'monitor' && <Badge ml="xs" size="xs" color="cyan" style={{ maxWidth: 'none' }}>{t('nodeMode.monitor')}</Badge>}<Text size="xs" c="dimmed">{n.hostname}</Text></Table.Td>
                  <Table.Td><Text size="sm">{localizedName(i18n.resolvedLanguage || i18n.language, n.monitor_group_zh, n.monitor_group_en, n.monitor_group) || t('nodeLayout.ungrouped')}</Text></Table.Td>
                  <Table.Td miw={140}><Group gap={4} style={{ minWidth: 'max-content' }}><NodeStatus n={n} />{n.cert_problem && <Badge color="red" size="xs" title={t('nodes.certProblem')}>TLS</Badge>}{n.doctor_fail && <Badge color="red" size="xs" variant="light" title={t('nodes.doctorFailHint')}>{t('nodes.doctor')}</Badge>}</Group></Table.Td>
                  <Table.Td><NodePublicAddresses node={n} /></Table.Td>
                  <Table.Td>{n.inbounds}</Table.Td>
                  <Table.Td><HostTrafficSummary traffic={n.host_traffic} compact /></Table.Td>
                  <Table.Td><ProxyTrafficSummary traffic={n.proxy_traffic} collectedBytes={n.traffic_today_bytes} /></Table.Td>
                  <Table.Td miw={170} style={{ whiteSpace: 'nowrap' }}>
                    <Stack gap={3} align="flex-start">
                      <Group gap={6} wrap="nowrap">
                        <Text size="sm">{n.version || '—'}</Text>
                        {isAdmin && n.paired && n.version && !n.upgrade_to && <Tooltip label={t('nodes.rollback')}><ActionIcon size={28} variant="subtle" color="gray" style={{ flexShrink: 0 }} aria-label={t('nodes.rollback')} onClick={() => modals.openConfirmModal({ title: t('nodes.rollback'), children: <Text size="sm">{t('nodes.rollbackConfirm', { name: n.name, version: n.version })}</Text>, labels: { confirm: t('nodes.rollback'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => rollback.mutate(n.id) })}><IconArrowBackUp size={16} /></ActionIcon></Tooltip>}
                      </Group>
                      <Text size="xs" c="dimmed">{n.platform}</Text>
                      {n.upgrade_to ? <Badge size="sm" color="blue" style={{ maxWidth: 'none', flexShrink: 0 }}>{t('nodes.upgrading', { version: n.upgrade_to })}</Badge> : isAdmin && n.outdated && n.paired && <Button size="compact-xs" variant="light" color="orange" leftSection={<IconArrowUp size={14} />} onClick={() => modals.openConfirmModal({ title: t('nodes.upgrade'), children: <Text size="sm">{t('nodes.upgradeConfirm', { name: n.name, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgrade'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgrade.mutate(n.id) })}>{t('nodes.outdated', { version: sys.data?.bosun_latest ?? '' })}</Button>}
                    </Stack>
                  </Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>{ago(n.last_seen_at)}</Table.Td>
                </Table.Tr>
              ))}
              {q.data && filtered.length === 0 && <Table.Tr><Table.Td colSpan={isAdmin ? 10 : 9}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Card>
      <Text size="xs" c="dimmed" mt="xs">{t('nodes.hostTrafficHint')}</Text>
      <Text size="xs" c="dimmed" mt={4}>{t('nodes.proxyTrafficHint')}</Text>
      {isAdmin && <AgentMigrationProgress />}
      <AgentMigrationDialog ids={selected} opened={migrateOpened} close={()=>setMigrateOpened(false)} />
      {organizing && layout.data && <NodeLayoutEditor initial={layout.data} nodes={q.data ?? []} onSaved={() => setNodeGroup(null)} close={() => setOrganizing(false)} />}
      <Modal keepMounted opened={opened} onClose={() => { close(); setCreated(null) }} title={created ? t('nodes.pairTitle') : t('nodes.create')} size="lg">
        {created ? <PairCodeBox code={created.pair_code ?? ''} mode={created.mode} agentURL={created.effective_agent_url} downloadSource={created.download_source} /> : (
          <form onSubmit={form.onSubmit((v) => { if (!domainIncomplete && !domainCheck.blocked) create.mutate(v) })}>
            <Stack>
              <Switch label={t('nodeMode.installOnly')} description={t('nodeMode.installHint')} checked={form.values.Mode === 'monitor'} onChange={e => form.setFieldValue('Mode', e.currentTarget.checked ? 'monitor' : 'full')} />
              {form.values.Mode === 'monitor' && <Text size="xs" c="dimmed">{t('nodeMode.openwrtHint')}</Text>}
              <TextInput label={t('nodes.name')} placeholder="jp-1" required {...form.getInputProps('Name')} />
              <Text size="sm" c="dimmed">{t('nodeAddresses.createHint')}</Text>
              <Switch label={t('nodeAddresses.autoIPv4')} {...form.getInputProps('AutoIPv4', { type: 'checkbox' })} />
              {!form.values.AutoIPv4 && <TextInput label={t('nodes.publicAddr')} description={t('nodeAddresses.fixedHint')} placeholder="198.51.100.20" {...form.getInputProps('PublicAddr')} />}
              <Switch label={t('nodeAddresses.autoIPv6')} {...form.getInputProps('AutoIPv6', { type: 'checkbox' })} />
              {!form.values.AutoIPv6 && <TextInput label={t('nodes.v6Addr')} description={t('nodeAddresses.fixedHint')} {...form.getInputProps('V6Addr')} />}
              <NodeDomainFields active={opened && !created} domain={form.values.Domain} shared={form.values.DomainShared} onDomainChange={v => form.setFieldValue('Domain', v)} onSharedChange={v => form.setFieldValue('DomainShared', v)} check={domainCheck} onIncompleteChange={setDomainIncomplete} />
              <TextInput label={t('nodes.internalAddr')} description={t('nodeAddresses.internalHint')} {...form.getInputProps('InternalAddr')} />
              <AgentConnectionFields active={opened && !created} agentURL={form.values.AgentURL} downloadSource={form.values.DownloadSource} onURLChange={value => form.setFieldValue('AgentURL', value)} onSourceChange={value => form.setFieldValue('DownloadSource', value)} />
              <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" disabled={domainCheck.blocked || domainIncomplete} loading={create.isPending}>{t('common.create')}</Button></Group>
            </Stack>
          </form>
        )}
      </Modal>
    </>
  )
}
