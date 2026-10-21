import { Alert, Badge, Button, Card, Group, Modal, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Node, type Group as UserGroup } from '../lib/api'
import { hostPort } from '../lib/ingress'
import { toast } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { scanRealityViaNode } from '../lib/reality-scan'
import { PrivateAccessFields } from './PrivateAccessFields'
import { RealityScan } from './RealityScan'
import { WireGuardReadiness, useWireGuardReadiness } from './WireGuardReadiness'

type Policy = { mode: string; rules?: { cidr: string; protocol?: string; port_start?: number; port_end?: number }[] }
type Forwarding = { state: string; backend?: string; reason?: string; checked_at: number }
interface Connection {
  id: string; exit_id: number; transit_id: number; version: number; name: string
  port: number; tunnel_port: number; endpoint: string; mtu: number; server_name: string
  group_id: number | null; private_access: Policy | null; enabled: boolean; state: string
  domain_preference?: '' | 'ipv4' | 'ipv6'; auto_forwarding?: boolean; forwarding?: Forwarding
}
function ForwardingStatus({ status, observedAt }: { status?: Forwarding; observedAt: number }) {
  const { t } = useTranslation()
  const known = ['synced', 'external', 'not_required', 'manual', 'unsupported', 'error']
  const checked = status?.checked_at ? status.checked_at * 1000 : 0
  const stale = checked > 0 && (observedAt - checked > 180000 || checked > observedAt)
  const state = stale ? 'stale' : status && known.includes(status.state) ? status.state : 'unknown'
  const color = state === 'synced' ? 'teal' : ['error', 'manual', 'unsupported'].includes(state) ? 'orange' : 'gray'
  const backend = status?.backend === 'ufw' ? 'UFW' : status?.backend === 'firewalld' ? 'firewalld' : ''
  const reason = status?.reason?.split(':')[0] ?? ''
  const reasons = ['policy_routing_unsupported', 'route_interface_ambiguous', 'private_grant_route_unavailable', 'no_forwarding_route', 'ufw_forward_rule_conflict', 'owned_rule_changed_retained', 'inactive_backend_rules_retained', 'automatic_forwarding_disabled', 'ufw_forward_runtime_verification_failed']
  return <Stack gap={4} mt="sm">
    <Group gap="xs"><Text size="xs" fw={500}>{t('wireguard.forwardingTitle')}{backend && ` (${backend})`}</Text><Badge size="sm" color={color}>{t(`wireguard.forwardingStates.${state}`)}</Badge></Group>
    <Text size="xs" c="dimmed">{t(`wireguard.forwardingHints.${state}`)}</Text>
    {!stale && reasons.includes(reason) && <Text size="xs" c="orange">{t(`wireguard.forwardingReasons.${reason}`)}</Text>}
    {checked > 0 && <Text size="xs" c="dimmed">{t('host.checked', { time: new Date(checked).toLocaleString() })}</Text>}
  </Stack>
}
interface Props { node: Node; readOnly?: boolean; editRequest?: number; onEditRequestHandled?: () => void }
export function WireGuardConnections(props: Props) {
  return props.readOnly ? <Connections key={props.node.id} {...props} /> : <SettingsDraftBoundary key={props.node.id}><Connections {...props} /></SettingsDraftBoundary>
}
function Connections({ node, readOnly = false, editRequest = 0, onEditRequestHandled }: Props) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const url = `/api/admin/settings/nodes/${node.id}/wireguard-connections`
  const q = useQuery({ queryKey: ['wireguard', node.id], queryFn: () => api.get<Connection[]>(url), refetchInterval: 10000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UserGroup[]>('/api/admin/groups') })
  const [draft, setDraft] = useState<Connection | null>(null)
  const [dirty, setDirty] = useState(false)
  const exitReady = useWireGuardReadiness(node.id)
  const transitReady = useWireGuardReadiness(draft?.transit_id ?? 0, true)
  useSettingsDirty(!!draft && dirty)
  const update = (value: Partial<Connection>) => { setDirty(true); setDraft(d => d && { ...d, ...value }) }
  const close = () => {
    if (!dirty) { setDraft(null); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: () => { setDraft(null); setDirty(false) } })
  }
  const invalidate = () => { for (const key of ['wireguard', 'node', 'entries']) qc.invalidateQueries({ queryKey: [key] }) }
  const save = useMutation({ mutationFn: (value: Connection) => value.id ? api.put(`${url}/${value.id}`, value) : api.post(url, value), onSuccess: () => { setDraft(null); setDirty(false); invalidate(); toast.ok(t('common.saved')) }, onError: toast.err })
  const remove = useMutation({ mutationFn: (value: Connection) => api.del(`${url}/${value.id}`, { version: value.version }), onSuccess: invalidate, onError: toast.err })
  const editable = !readOnly && node.proxy_allowed !== false && node.paired
  const handledRequest = useRef(0)
  const create = useCallback(() => {
    if (!editable || !q.data || q.isError) return
    setDirty(false)
    setDraft({ id: '', exit_id: node.id, transit_id: 0, version: 0, name: '', port: 18443, tunnel_port: 51820, endpoint: '', mtu: 1380, server_name: '', group_id: null, private_access: { mode: 'off' }, enabled: true, state: 'pending', domain_preference: 'ipv4', auto_forwarding: true })
  }, [editable, q.data, q.isError, node.id])
  useEffect(() => {
    if (editRequest > 0 && handledRequest.current !== editRequest && editable && q.data && !q.isError) {
      handledRequest.current = editRequest
      create()
      onEditRequestHandled?.()
    }
  }, [editRequest, editable, q.data, q.isError, create, onEditRequestHandled])
  const labels: Record<string, string> = { connected: t('reverse.connected'), disconnected: t('reverse.disconnected'), pending: t('reverse.pending'), disabled: t('reverse.disabled'), unavailable: t('wireguard.unavailable'), blocked: t('wireguard.blocked') }
  return <Stack>
    <Text size="sm" c="dimmed">{t('wireguard.hint')}</Text>
    <WireGuardReadiness key={node.id} nodeID={node.id} query={exitReady} readOnly={readOnly} />
    {q.isError && <Alert color="red">{t('reverse.loadFailed')} <Button variant="subtle" onClick={() => q.refetch()}>{t('common.refresh')}</Button></Alert>}
    {(q.data ?? []).map(c => <Card key={c.id} withBorder p="sm"><Group justify="space-between" align="flex-start">
      <Stack gap={4}><Text fw={600}>{c.name}</Text><Text size="xs" c="dimmed">{c.endpoint}</Text>
        {c.exit_id !== node.id && <Button component={Link} to={`/entries?tab=lines&section=wireguard&server=${c.exit_id}`} variant="subtle" size="compact-xs">{t('reverse.manageExit')}</Button>}
      </Stack><Stack gap="xs" align="flex-end"><Badge color={c.state === 'connected' ? 'teal' : c.state === 'blocked' ? 'red' : 'gray'}>{labels[c.state] ?? labels.pending}</Badge>
        {editable && c.exit_id === node.id && <Group gap="xs"><Button size="compact-xs" variant="subtle" onClick={() => { setDraft(structuredClone(c)); setDirty(false) }}>{t('common.edit')}</Button>
          <Button size="compact-xs" variant="subtle" color="red" loading={remove.isPending} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text>{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => remove.mutate(c) })}>{t('common.delete')}</Button></Group>}
      </Stack>
    </Group><ForwardingStatus status={c.forwarding} observedAt={Math.max(q.dataUpdatedAt, q.errorUpdatedAt)} /></Card>)}
    {!readOnly && <Group><Button disabled={!editable || !q.data || q.isError} onClick={create}>{t('common.create')}</Button></Group>}
    <Modal opened={!readOnly && !!draft} onClose={close} title={t('wireguard.title')} size="lg" closeOnClickOutside={false}>
      {draft && <Stack>
        <Alert>{t('wireguard.requirements')}</Alert>
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput required label={t('entries.name')} value={draft.name} onChange={e => update({ name: e.currentTarget.value })} />
          <Select required searchable label={t('wireguard.transit')} disabled={!!draft.id} value={draft.transit_id ? String(draft.transit_id) : null} data={(nodes.data ?? []).filter(n => n.id !== node.id && n.proxy_allowed !== false && n.paired).map(n => ({ value: String(n.id), label: n.name }))} onChange={id => { const n = nodes.data?.find(n => n.id === Number(id)); update({ transit_id: Number(id), endpoint: n?.public_addr ? hostPort(n.public_addr, draft.tunnel_port) : '' }) }} />
          <NumberInput required label={t('reverse.userPort')} min={1} max={65535} value={draft.port} onChange={n => update({ port: Number(n) })} />
          <NumberInput required label={t('reverse.tunnelPort')} min={1} max={65535} value={draft.tunnel_port} onChange={n => update({ tunnel_port: Number(n) })} />
          <TextInput required label={t('wireguard.endpoint')} placeholder="198.51.100.20:51820" value={draft.endpoint} onChange={e => update({ endpoint: e.currentTarget.value })} />
          <NumberInput required label="MTU" min={1280} max={1420} value={draft.mtu} onChange={n => update({ mtu: Number(n) })} />
        </SimpleGrid>
        <WireGuardReadiness key={`B-${node.id}`} nodeID={node.id} role="B" query={exitReady} />
        {draft.transit_id > 0 && <WireGuardReadiness key={`A-${draft.transit_id}`} nodeID={draft.transit_id} role="A" query={transitReady} />}
        <Text size="xs" c="dimmed">{t('wireguard.endpointHint')}</Text>
        <Switch label={t('wireguard.autoForwarding')} description={t('wireguard.autoForwardingHint')} checked={draft.auto_forwarding ?? false} onChange={e => update({ auto_forwarding: e.currentTarget.checked })} />
        <Select label={t('wireguard.domainPreference')} description={t('wireguard.domainPreferenceHint')} value={draft.domain_preference ?? ''} data={[
          { value: 'ipv4', label: t('wireguard.preferIPv4') },
          { value: 'ipv6', label: t('wireguard.preferIPv6') },
          { value: '', label: t('wireguard.legacyPreference') },
        ]} allowDeselect={false} onChange={value => { if (value === '' || value === 'ipv4' || value === 'ipv6') update({ domain_preference: value }) }} />
        <TextInput required label={t('wireguard.sni')} description={t('wireguard.sniHint')} value={draft.server_name} onChange={e => update({ server_name: e.currentTarget.value })} />
        <Text size="xs" c="dimmed">{draft.transit_id > 0 ? t('wireguard.scanFrom', { node: nodes.data?.find(n => n.id === draft.transit_id)?.name ?? String(draft.transit_id) }) : t('wireguard.selectTransitToScan')}</Text>
        <RealityScan key={draft.transit_id} disabled={!draft.transit_id} current={draft.server_name} scan={(hosts, signal) => scanRealityViaNode(draft.transit_id, hosts, signal)} onPick={server_name => update({ server_name })} />
        <Select clearable label={t('inbounds.group')} placeholder={t('inbounds.groupAll')} value={draft.group_id ? String(draft.group_id) : null} data={(groups.data ?? []).map(g => ({ value: String(g.ID), label: g.Name }))} onChange={id => update({ group_id: id ? Number(id) : null })} />
        <PrivateAccessFields explicitOnly json={JSON.stringify({ private_access: draft.private_access })} onChange={json => update({ private_access: JSON.parse(json).private_access })} />
        <Switch label={t('common.enabled')} checked={draft.enabled} onChange={e => update({ enabled: e.currentTarget.checked })} />
        <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button loading={save.isPending} disabled={!editable || (draft.enabled && (!exitReady.data?.ready || !transitReady.data?.ready || exitReady.isError || transitReady.isError)) || !draft.transit_id || !draft.name.trim() || !draft.endpoint || !draft.server_name.trim()} onClick={() => save.mutate(draft)}>{t('common.save')}</Button></Group>
      </Stack>}
    </Modal>
  </Stack>
}
