import { Alert, Badge, Button, Card, Group, Modal, NumberInput, Stack, Switch, Text, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type InternalAddressUsage, type Node } from '../lib/api'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
import { nodeEditValues, nodeEditPayload, type NodeEditSection } from '../lib/node-edit'
import { useNodeDomainCheck } from '../lib/use-node-domain-check'
import { NodeDomainFields } from './NodeDomainFields'
import { NodeAddressesCard } from './NodeAddressesCard'
import { supplierURL } from '../lib/node-infrastructure'
import { useAuth } from '../lib/auth'
import { InfoGrid, InfoTile } from './InfoTile'
import { NodeDNSBadges } from './NodeDNSBadges'

export function NodeDetailsCard({ node: n, section }: { node: Node; section: NodeEditSection }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { me } = useAuth()
  const [editNode, setEditNode] = useState(false)
  const [baseline, setBaseline] = useState(() => nodeEditValues(n))
  const nodeForm = useForm({ initialValues: baseline })
  useSettingsDirty(editNode && nodeForm.isDirty())
  const internalUsage = useQuery({ queryKey: ['internal-address-usage', String(n.id)], queryFn: () => api.get<InternalAddressUsage>(`/api/admin/nodes/${n.id}/internal-address-usage`), enabled: editNode && section === 'profile' })
  const domainCheck = useNodeDomainCheck(nodeForm.values.Domain, nodeForm.values.DomainShared, n.id, editNode && section === 'profile')
  const internalChanged = nodeForm.values.InternalAddr.trim() !== baseline.InternalAddr.trim()
  const internalInUse = (internalUsage.data?.uses.length ?? 0) > 0
  const internalBlocked = section === 'profile' && internalChanged && (internalUsage.isPending || internalUsage.isError || internalInUse && !nodeForm.values.InternalAddr.trim())
  const saveNode = useMutation({
    mutationFn: async (submitted: typeof baseline) => {
      // The legacy PATCH replaces scalar fields. Re-read the other section and
      // overlay only this form's edits, preserving fields outside its scope.
      const latest = await api.get<{ node: Node }>(`/api/admin/nodes/${n.id}`)
      return api.patch<{ ok: boolean; dns?: DNSResult[] }>(`/api/admin/nodes/${n.id}`, nodeEditPayload(latest.node, submitted, baseline, section))
    },
    onSuccess: r => { setEditNode(false); for (const key of ['node', 'nodes', 'lines', 'forwards', 'internal-address-usage', 'dns-health', 'dns-history']) qc.invalidateQueries({ queryKey: [key] }); toast.ok(t('common.saved')); dnsToast(r.dns) },
    onError: e => { toast.err(e); qc.invalidateQueries({ queryKey: ['node-domain-check'] }) },
  })
  const close = () => {
    if (saveNode.isPending) return
    if (!nodeForm.isDirty()) { setEditNode(false); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: () => setEditNode(false) })
  }
  return <>
    <Card mb="lg"><Group justify="space-between" mb="md"><Text fw={600}>{t(section === 'profile' ? 'serverDetail.profile' : 'serverDetail.settings.proxy')}</Text><Button size="xs" variant="light" onClick={() => { const values = nodeEditValues(n); setBaseline(values); nodeForm.setValues(values); nodeForm.resetDirty(values); setEditNode(true) }}>{t('common.edit')}</Button></Group>
      {section === 'profile' ? <InfoGrid>
        <InfoTile label={t('nodes.name')} value={n.name} /><InfoTile label={t('nodes.hostname')} value={n.hostname} mono /><InfoTile label={t('nodes.platform')} value={n.platform} /><InfoTile label={t('nodes.version')} value={n.version} />
        <InfoTile label={t('nodes.publicAddr')} value={n.public_addr} mono /><InfoTile label={t('nodes.internalAddr')} value={n.internal_addr} mono hint={t('nodeAddresses.internalHint')} /><InfoTile label={t('nodes.v6Addr')} value={n.v6_addr} mono /><InfoTile label={t('nodes.nodeDomain')} value={<><Text size="sm" fw={600} ff="monospace" style={{ overflowWrap: 'anywhere' }}>{n.domain || '—'}</Text><NodeDNSBadges node={n} /></>} />
        <InfoTile label={t('nodes.monitorUrl')} value={supplierURL(n.monitor_url) ? <a href={supplierURL(n.monitor_url)} target="_blank" rel="noopener noreferrer">{t('nodes.open')}</a> : n.monitor_url || '—'} />
      </InfoGrid> : <InfoGrid>
        <InfoTile label={t('nodes.speedLimit')} value={`${n.user_speed_limit_mbps ?? 0} Mbps`} hint={t('nodes.speedLimitHint')} />
        {[['nodes.mitaQuotas', n.mita_quotas], ['nodes.egressByIngress', n.egress_by_ingress], ['nodes.decoy', n.decoy_enabled]].map(([label, enabled]) => <InfoTile key={String(label)} label={t(String(label))} value={<Badge color={enabled ? 'teal' : 'gray'}>{t(enabled ? 'common.enabled' : 'common.disabled')}</Badge>} />)}
        {n.decoy_enabled && <InfoTile label={t('nodes.decoyUpstream')} value={n.decoy_upstream} />}
      </InfoGrid>}
    </Card>
    {section === 'profile' && <NodeAddressesCard nodeID={n.id} canWrite={me?.role === 'admin'} />}
    <Modal opened={editNode} onClose={close} title={t(section === 'profile' ? 'serverDetail.profile' : 'serverDetail.settings.proxy')} size="lg" closeOnClickOutside={false}>
      <form onSubmit={nodeForm.onSubmit(v => { if (!internalBlocked) saveNode.mutate(v) })}><fieldset disabled={saveNode.isPending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}><Stack>
        {section === 'profile' ? <>
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
        </> : <>
<NumberInput label={t('nodes.speedLimit')} description={t('nodes.speedLimitHint')} min={0} {...nodeForm.getInputProps('UserSpeedLimitMbps')} />
          <Switch label={t('nodes.mitaQuotas')} description={t('nodes.mitaQuotasHint')} {...nodeForm.getInputProps('MitaQuotas', { type: 'checkbox' })} />
          <Switch label={t('nodes.egressByIngress')} description={t('nodes.egressByIngressHint')} {...nodeForm.getInputProps('EgressByIngress', { type: 'checkbox' })} />
          <Switch label={t('nodes.decoy')} description={t('nodes.decoyHint')} disabled={!nodeForm.values.Domain} {...nodeForm.getInputProps('DecoyEnabled', { type: 'checkbox' })} />
          {nodeForm.values.DecoyEnabled && <TextInput label={t('nodes.decoyUpstream')} description={t('nodes.decoyUpstreamHint')} placeholder="https://www.example.com" {...nodeForm.getInputProps('DecoyUpstream')} />}

        </>}
        <Group justify="flex-end"><Button variant="default" disabled={saveNode.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" disabled={section === 'profile' && domainCheck.blocked || internalBlocked} loading={saveNode.isPending}>{t('common.save')}</Button></Group>
      </Stack></fieldset></form>
    </Modal>
  </>
}
