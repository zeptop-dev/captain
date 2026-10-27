import { PrivateAccessReadiness } from '../components/PrivateAccessReadiness'
import { NodeGroupCard } from '../components/NodeGroupCard'
import { NodeAddressesCard } from '../components/NodeAddressesCard'
import { NodeModeCard } from '../components/NodeModeCard'
import { DNSHealthCard } from '../components/DNSHealthCard'
import { NodeDomainFields } from '../components/NodeDomainFields'
import { useNodeDomainCheck } from '../lib/use-node-domain-check'
import { CoreManagementCard, type CoreInventory } from '../components/CoreManagementCard'
import { WireGuardConnections } from '../components/WireGuardConnections'
import { ReverseConnections } from '../components/ReverseConnections'
import type { ResourceHost } from '../lib/resources'
import { coreName } from '../lib/coreSelection'
import { NumberInput, Switch, Accordion, Badge, Button, Card, Code, Group, Modal, Stack, Table, Text, TextInput, Title, Alert, Loader } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconInfoCircle, IconPencil, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { api, runNodeJob, type DoctorReport, type Ingress, type CertStatus, type Inbound, type InternalAddressUsage, type Node } from '../lib/api'
import { ago, bytes, when } from '../lib/format'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { useNodeAccess } from '../lib/node-access-query'
import { AccessSummary } from '../components/NodeAccess'
import { PageHeader } from '../components/PageHeader'
import { InfoGrid, InfoTile, SectionTitle } from '../components/InfoTile'
import { HostTrafficSummary } from '../components/HostTrafficSummary'
import { ProxyTrafficSummary } from '../components/ProxyTrafficSummary'
import { NodeStatus, PairCodeBox } from './NodesPage'
import { NodeProbeCard } from '../components/NodeProbeCard'
import { NodeWorkflow } from '../components/NodeWorkflow'
import { ServerProxySettings } from '../components/ServerProxySettings'
import { ForwardsCard } from '../components/ForwardsCard'
import { MetadataEditor } from '../components/MetadataEditor'
import { DeleteNodeModal } from '../components/DeleteNodeModal'
import { SettingsDraftBoundary } from '../lib/settings-draft'

interface Detail { traffic?: Record<string, { today: number; total: number }>; node: Node; inbounds: Inbound[]; ingresses?: Ingress[]; status: { core_inventory?: CoreInventory | null; host: ResourceHost | null; cores: Record<string, { running: boolean; inbounds?: string[] }> | null; certs: CertStatus[] | null; doctor?: DoctorReport | null } | null }

export default function NodePage() {
  const { id } = useParams()
  const location = useLocation()
  const [params] = useSearchParams()
  const { me } = useAuth()
  const { t } = useTranslation()
  const qc = useQueryClient()
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['node', id], queryFn: () => api.get<Detail>(`/api/admin/nodes/${id}`), refetchInterval: 10_000 })
  const access = useNodeAccess()
  const [editNode, setEditNode] = useState(false)
  const internalUsage = useQuery({ queryKey: ['internal-address-usage', id], queryFn: () => api.get<InternalAddressUsage>(`/api/admin/nodes/${id}/internal-address-usage`), enabled: editNode })
  const [pair, setPair] = useState<string | null>(null)
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['node', id] }); qc.invalidateQueries({ queryKey: ['nodes'] }) }
  const repair = useMutation({ mutationFn: () => api.post<{ pair_code: string }>(`/api/admin/nodes/${id}/repair`), onSuccess: (r) => { setPair(r.pair_code); invalidate() }, onError: toast.err })
  const [deletingNode, setDeletingNode] = useState(false)
  const nodeForm = useForm({ initialValues: { Name: '', PublicAddr: '', InternalAddr: '', V6Addr: '', Domain: '', DomainShared: false, MonitorURL: '', DecoyEnabled: false, DecoyUpstream: '', UserSpeedLimitMbps: 0, MitaQuotas: false, EgressByIngress: false } })
  const domainCheck = useNodeDomainCheck(nodeForm.values.Domain, nodeForm.values.DomainShared, Number(id), editNode)
  const saveNode = useMutation({ mutationFn: (v: typeof nodeForm.values) => api.patch<{ ok: boolean; dns?: DNSResult[] }>(`/api/admin/nodes/${id}`, v), onSuccess: (r) => { toast.ok(t('common.saved')); setEditNode(false); invalidate(); qc.invalidateQueries({ queryKey: ['node'] }); qc.invalidateQueries({ queryKey: ['lines'] }); qc.invalidateQueries({ queryKey: ['forwards'] }); qc.invalidateQueries({ queryKey: ['internal-address-usage'] }); qc.invalidateQueries({ queryKey: ['dns-health'] }); qc.invalidateQueries({ queryKey: ['dns-history'] }); dnsToast(r.dns) }, onError: (e) => { toast.err(e); qc.invalidateQueries({ queryKey: ['node-domain-check'] }) } })

  const d = q.data
  if (q.isPending) return <Loader size="sm" />
  if (q.isError || !d) return <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>
  const n = d.node
  const internalChanged = nodeForm.values.InternalAddr.trim() !== n.internal_addr.trim()
  const internalInUse = (internalUsage.data?.uses.length ?? 0) > 0
  const internalBlocked = internalChanged && (internalUsage.isPending || internalUsage.isError || internalInUse && !nodeForm.values.InternalAddr.trim())
  const host = d.status?.host
  const proxyAllowed = n.proxy_allowed !== false
  // One blocker owns all server forms; a clean sibling must not hide another
  // form's draft. Confirmed navigation resets drafts for the new destination.
  return (
    <SettingsDraftBoundary key={`${n.id}:${location.key}`}>
      {(n.domain_shared || n.domain_conflict) && <Alert mb="md" color={n.domain_shared ? 'blue' : 'orange'} title={t(n.domain_shared ? 'nodes.domainExternal' : 'nodes.domainDuplicate')}>{t(n.domain_shared ? 'nodes.domainSharedHint' : 'nodes.domainLegacyHint')}</Alert>}
      <DeleteNodeModal node={n} opened={deletingNode} onClose={() => setDeletingNode(false)} onDeleted={() => { qc.invalidateQueries({ queryKey: ['nodes'] }); nav('/nodes') }} />
      <PageHeader title={n.name} subtitle={`${n.hostname || ''} ${n.platform || ''} ${n.version || ''}`.trim()} actions={<>
        <NodeStatus n={n} />
        {me?.role === 'admin' && <Button component={Link} to={`/nodes/${n.id}/host`} variant="light" size="xs">{t('host.title')}</Button>}
        {me?.role === 'admin' && <Button variant="light" size="xs" onClick={() => nav(`/infrastructure?node=${n.id}`)}>{t('infra.title')}</Button>}
        <Button variant="default" size="xs" leftSection={<IconPencil size={14} />} onClick={() => { nodeForm.setValues({ Name: n.name, PublicAddr: n.public_addr, InternalAddr: n.internal_addr, V6Addr: n.v6_addr, Domain: n.domain ?? '', DomainShared: !!n.domain_shared, MonitorURL: n.monitor_url, DecoyEnabled: !!n.decoy_enabled, DecoyUpstream: n.decoy_upstream ?? '', UserSpeedLimitMbps: n.user_speed_limit_mbps ?? 0, MitaQuotas: !!n.mita_quotas, EgressByIngress: !!n.egress_by_ingress }); setEditNode(true) }}>{t('common.edit')}</Button>
        <Button variant="default" size="xs" onClick={() => modals.openConfirmModal({ title: t('nodes.repair'), children: <Text size="sm">{t('nodes.repairHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => repair.mutate() })}>{t('nodes.repair')}</Button>
        <Button color="red" variant="light" size="xs" leftSection={<IconTrash size={14} />} onClick={() => setDeletingNode(true)}>{t('common.delete')}</Button>
      </>} />

      <NodeModeCard node={n} canWrite={me?.role === 'admin'} />
      {proxyAllowed && me?.role === 'admin' && <CoreManagementCard inventory={d.status?.core_inventory} online={n.online} canWrite inbounds={d.inbounds.filter(ib => ib.Enabled).map(ib => ib.Tag)} onAction={async request => {
        const result = await runNodeJob<CoreInventory>(n.id, 'core_manage', request, 920_000)
        qc.setQueryData<Detail>(['node', id], old => old?.status ? { ...old, status: { ...old.status, core_inventory: result } } : old)
        invalidate()
        qc.invalidateQueries({ queryKey: ['core-options'] })
      }} />}
      {proxyAllowed && <PrivateAccessReadiness key={`readiness-${n.id}`} nodeID={n.id} />}
      {proxyAllowed && <NodeWorkflow node={n} inbounds={d.inbounds} cores={d.status?.cores} />}
      {!n.paired && n.pair_code && <Card mb="lg"><Title order={5} mb="sm">{t('nodes.pairTitle')}</Title><PairCodeBox code={n.pair_code} mode={n.mode} /></Card>}
      {n.paired && <NodeProbeCard key={`probe-${n.id}`} nodeID={n.id} reportedHost={host} />}
      <NodeGroupCard key={`${n.id}:${n.monitor_group ?? ''}`} node={n} />
      <Card mb="lg"><MetadataEditor endpoint={`/api/admin/nodes/${n.id}/metadata`} /></Card>
      {d.status?.doctor && <DoctorCard report={d.status.doctor} />}
      <NodeAddressesCard nodeID={n.id} canWrite={me?.role === 'admin'} />
      <DNSHealthCard nodeID={n.id} />
      {d.status?.certs && d.status.certs.length > 0 && (
        <Card mb="lg">
          <Text size="sm" c="dimmed" fw={500} mb="xs">{t('nodes.certs')}</Text>
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

      <Card mb="lg">
        <SectionTitle icon={<IconInfoCircle size={20} />}>{t('nodes.info')}</SectionTitle>
        <InfoGrid>
          <InfoTile label={t('nodes.hostname')} value={n.hostname} mono />
          <InfoTile label={t('nodes.platform')} value={n.platform} />
          <InfoTile label={t('nodes.version')} value={n.version} mono />
          <InfoTile label={t('nodes.publicAddr')} value={n.public_addr} mono />
          <InfoTile label={t('nodes.internalAddr')} value={n.internal_addr} mono />
          <InfoTile label={t('nodes.v6Addr')} value={n.v6_addr} mono />
          <InfoTile label={t('nodes.nodeDomain')} value={n.domain} mono />
          <InfoTile label={t('nodes.lastSeen')} value={n.last_seen_at ? ago(n.last_seen_at) : undefined} />
          <InfoTile label={t('nodes.hostTraffic')} value={<HostTrafficSummary traffic={n.host_traffic} />} hint={t('nodes.hostTrafficHint')} />
          <InfoTile label={t('nodes.trafficToday')} value={<ProxyTrafficSummary traffic={n.proxy_traffic} collectedBytes={n.traffic_today_bytes} />} hint={t('nodes.proxyTrafficHint')} />
          <InfoTile label={t('nodes.cores')} value={Object.keys(d.status?.cores ?? {}).length ? <Group gap={4}>{Object.entries(d.status?.cores ?? {}).map(([name, c]) => <Badge key={name} color={c.running ? 'teal' : 'gray'}>{name}</Badge>)}</Group> : undefined} />
          {n.monitor_url && <InfoTile label={t('nodes.monitor')} value={<a href={n.monitor_url} target="_blank" rel="noreferrer">{t('nodes.open')}</a>} />}
        </InfoGrid>
      </Card>


      {proxyAllowed && <><Card p={0} id="node-inbounds" style={{ scrollMarginTop: 80 }}>
        <Group justify="space-between" p="md" pb="xs">
          <div><Text fw={600}>{t('inbounds.title')}</Text><Text size="xs" c="dimmed">{t('inbounds.subtitle')}</Text></div>
          <Group gap="xs"><Button component={Link} to={`/entries?node=${n.id}`} variant="light" size="xs">{t('entries.title')}</Button>{(me?.role === 'admin' || me?.role === 'operator') && <Button component={Link} to={`/entries?section=inbounds&server=${n.id}`} variant="default" size="xs">{t('inbounds.title')}</Button>}</Group>
        </Group>
        <Table>
          <Table.Thead><Table.Tr><Table.Th>{t('inbounds.tag')}</Table.Th><Table.Th>{t('inbounds.protocol')}</Table.Th><Table.Th>{t('inbounds.port')}</Table.Th><Table.Th>{t('inbounds.core')}</Table.Th><Table.Th>{t('inbounds.traffic')}</Table.Th><Table.Th>{t('nodeAccess.title')}</Table.Th><Table.Th>{t('inbounds.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {d.inbounds.map((ib) => (
              <Table.Tr key={ib.ID}>
                <Table.Td><Text fw={600}>{ib.Tag}</Text></Table.Td>
                <Table.Td><Badge>{ib.Protocol}</Badge></Table.Td>
                <Table.Td><Code>{ib.Listen || '::'}:{ib.Port}</Code></Table.Td>
                <Table.Td><Text size="sm">{ib.Core ? coreName(ib.Core) : t('inbounds.coreAuto')}</Text><Text size="xs" c="dimmed">{(() => { const actual = Object.entries(d.node.online ? d.status?.cores ?? {} : {}).find(([, c]) => c.running && c.inbounds?.includes(ib.Tag))?.[0]; return actual ? t('inbounds.coreSelection.running', { core: coreName(actual) }) : t('inbounds.coreSelection.notReported') })()}</Text></Table.Td>
                <Table.Td>{d.traffic?.[String(ib.ID)] ? <Text size="xs">{bytes(d.traffic[String(ib.ID)].today)} <Text span c="dimmed">/ {bytes(d.traffic[String(ib.ID)].total)}</Text></Text> : <Text size="xs" c="dimmed">—</Text>}</Table.Td>
                <Table.Td>{me?.role === 'admin' ? <AccessSummary row={access.isError ? undefined : access.data?.inbounds.find(row => row.inbound_id === ib.ID)} directory={access.data} /> : <Text size="xs" c="dimmed">{t('nodeAccess.adminOnly')}</Text>}</Table.Td>
                <Table.Td>{ib.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
                <Table.Td>{(me?.role === 'admin' || (me?.role === 'operator' && !ib.Settings.reverse && !ib.Settings.wireguard_slot)) && <Button component={Link} to={(ib.Settings.reverse || ib.Settings.wireguard_slot) ? `/entries?node=${n.id}` : `/entries?section=inbounds&server=${n.id}&inbound=${ib.ID}`} variant="subtle" size="compact-xs">{t((ib.Settings.reverse || ib.Settings.wireguard_slot) ? 'reverse.managed' : 'inbounds.title')}</Button>}</Table.Td>
              </Table.Tr>
            ))}
            {d.inbounds.length === 0 && <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
          </Table.Tbody>
        </Table>
      </Card>

      <ServerProxySettings key={`server-settings-${n.id}-${params.get('section') ?? ''}`} node={n} inbounds={d.inbounds} ingresses={d.ingresses ?? []} section={params.get('section')} />
      <Card mt="lg" p={0}>
        <Text fw={600} p="md">{t('lines.tab')}</Text>
        <Accordion multiple chevronPosition="right" variant="default">
          {me?.role === 'admin' && <Accordion.Item value="reverse"><Accordion.Control><Text size="sm" fw={600}>{t('reverse.title')}</Text></Accordion.Control><Accordion.Panel><ReverseConnections key={n.id} node={n} readOnly /><Button component={Link} to={`/entries?tab=lines&section=reverse&server=${n.id}`} variant="light" size="xs" mt="sm">{t('reverse.manage')}</Button></Accordion.Panel></Accordion.Item>}
          {me?.role === 'admin' && <Accordion.Item value="wireguard"><Accordion.Control><Text size="sm" fw={600}>{t('wireguard.title')}</Text></Accordion.Control><Accordion.Panel><WireGuardConnections key={n.id} node={n} readOnly /><Button component={Link} to={`/entries?tab=lines&section=wireguard&server=${n.id}`} variant="light" size="xs" mt="sm">{t('wireguard.title')}</Button></Accordion.Panel></Accordion.Item>}
          <Accordion.Item value="forwards"><Accordion.Control><Text size="sm" fw={600}>{t('forwards.title')}</Text><Text size="xs" c="dimmed">{t('nodes.advForwardsHint')}</Text></Accordion.Control><Accordion.Panel><ForwardsCard key={n.id} embedded node={n} ingresses={d.ingresses ?? []} readOnly /><Button component={Link} to={`/entries?tab=lines&section=forward&mode=single&server=${n.id}`} variant="light" size="xs" mt="sm">{t('forwards.title')}</Button></Accordion.Panel></Accordion.Item>
        </Accordion>
      </Card>
      </>}
      <Modal opened={editNode} onClose={() => setEditNode(false)} title={t('common.edit')}>
        <form onSubmit={nodeForm.onSubmit((v) => { if (!internalBlocked) saveNode.mutate(v) })}><Stack>
          <TextInput label={t('nodes.name')} required {...nodeForm.getInputProps('Name')} />
          <TextInput label={t('nodes.publicAddr')} {...nodeForm.getInputProps('PublicAddr')} />
          <NodeDomainFields domain={nodeForm.values.Domain} shared={nodeForm.values.DomainShared} onDomainChange={v => nodeForm.setFieldValue('Domain', v)} onSharedChange={v => nodeForm.setFieldValue('DomainShared', v)} check={domainCheck} />
          <Group grow><TextInput label={t('nodes.internalAddr')} description={t('nodeAddresses.internalHint')} {...nodeForm.getInputProps('InternalAddr')} /><TextInput label={t('nodes.v6Addr')} {...nodeForm.getInputProps('V6Addr')} /></Group>
          {internalUsage.isPending && <Text size="xs" c="dimmed">{t('forwardAddress.checkingUsage')}</Text>}
          {internalUsage.isError && <Alert color="red"><Text size="sm">{t('forwardAddress.usageError')}</Text><Button variant="subtle" size="xs" onClick={() => internalUsage.refetch()}>{t('workspace.retry')}</Button></Alert>}
          {internalInUse && <Alert color={internalChanged ? 'yellow' : 'blue'} title={t('forwardAddress.usedBy')}>
            <Stack gap={4}>{internalUsage.data?.uses.map((use, index) => <Text size="sm" key={`${use.kind}-${use.id}-${use.source_node_id}-${use.tag ?? index}`}>
              <Link to={use.kind === 'line' ? `/entries?tab=lines&section=forward&line=${use.id}#line-${use.id}` : `/entries?tab=lines&section=forward&mode=single&server=${use.source_node_id}`}>{use.name}{use.tag ? ` / ${use.tag}` : ''}</Link>
            </Text>)}</Stack>
            {internalChanged && <Text size="sm" mt="xs">{t(nodeForm.values.InternalAddr.trim() ? 'forwardAddress.changeWarning' : 'forwardAddress.clearBlocked')}</Text>}
          </Alert>}
          <TextInput label={t('nodes.monitorUrl')} description={t('nodes.monitorUrlHint')} placeholder="https://monitor.example.com/..." {...nodeForm.getInputProps('MonitorURL')} />
          {proxyAllowed && <><NumberInput label={t('nodes.speedLimit')} description={t('nodes.speedLimitHint')} min={0} {...nodeForm.getInputProps('UserSpeedLimitMbps')} />
          <Switch label={t('nodes.mitaQuotas')} description={t('nodes.mitaQuotasHint')} {...nodeForm.getInputProps('MitaQuotas', { type: 'checkbox' })} />
          <Switch label={t('nodes.egressByIngress')} description={t('nodes.egressByIngressHint')} {...nodeForm.getInputProps('EgressByIngress', { type: 'checkbox' })} />
          <Switch label={t('nodes.decoy')} description={t('nodes.decoyHint')} disabled={!nodeForm.values.Domain} {...nodeForm.getInputProps('DecoyEnabled', { type: 'checkbox' })} />
          {nodeForm.values.DecoyEnabled && <TextInput label={t('nodes.decoyUpstream')} description={t('nodes.decoyUpstreamHint')} placeholder="https://www.example.com" {...nodeForm.getInputProps('DecoyUpstream')} />}
          </>}
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditNode(false)}>{t('common.cancel')}</Button><Button type="submit" disabled={domainCheck.blocked || internalBlocked} loading={saveNode.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
      <Modal opened={pair !== null} onClose={() => setPair(null)} title={t('nodes.pairTitle')} size="lg">{pair && <PairCodeBox code={pair} mode={n.mode} />}</Modal>
    </SettingsDraftBoundary>
  )
}

// The node's last self-check (bosun >= 0.18 sends one on change or every 30 min).
function DoctorCard({ report }: { report: DoctorReport }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(report.summary.fail > 0 || report.summary.warn > 0)
  const color = (s: string) => (s === 'ok' ? 'teal' : s === 'warn' ? 'orange' : s === 'fail' ? 'red' : 'gray')
  // Inbounds the agent left out (no core, missing key, waiting for users):
  // the one thing an operator must act on, so it gets its own bar.
  const notApplied = report.checks.filter((c) => (c.detail ?? '').startsWith('not applied'))
  return (
    <Card mb="lg">
      {notApplied.length > 0 && (
        <Alert color="yellow" variant="light" mb="sm" title={t('nodes.doctorNotApplied')}>
          {notApplied.map((c) => <Text size="sm" key={c.id}><b>{c.id.replace(/^inbound:/, '')}</b>: {(c.detail ?? '').replace(/^not applied: /, '')}</Text>)}
        </Alert>
      )}
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
