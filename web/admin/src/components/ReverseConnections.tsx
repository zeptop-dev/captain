import { Alert, Badge, Button, Card, Group, Modal, MultiSelect, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Group as UserGroup, type Inbound, type Ingress, type Node } from '../lib/api'
import { firstFreeIngressPort, hostPort, ingressPort } from '../lib/ingress'
import { toast } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { InboundForm, toPayload, toValues } from './InboundForm'
import { RealityScan } from './RealityScan'
import { scanRealityViaNode } from '../lib/reality-scan'
import { withReverseProtocol, withReverseTarget } from '../lib/reverse-target'

interface Connection {
  id: string; exit_id: number; transit_id: number; version: number; user_inbound_id: number
  listen?: string; name: string; port: number; tunnel_port: number; ingress_id: number | null; group_id: number | null
  server_name: string; enabled: boolean; protocol: string; settings?: Record<string, unknown>
  state: string; host: string; public_port: number; public_tunnel_port: number
}
interface Detail { node: Node; inbounds: Inbound[]; ingresses: Ingress[] }


interface Props { node: Node; readOnly?: boolean; editRequest?: number; onEditRequestHandled?: () => void }
export function ReverseConnections(props: Props) {
  return props.readOnly ? <Connections key={props.node.id} {...props} /> : <SettingsDraftBoundary key={props.node.id}><Connections {...props} /></SettingsDraftBoundary>
}

function Connections({ node, readOnly = false, editRequest = 0, onEditRequestHandled }: Props) {
  const { t } = useTranslation()
  const stateLabels: Record<string, string> = {
    pending: t('reverse.pending'), connected: t('reverse.connected'), disconnected: t('reverse.disconnected'), offline: t('reverse.offline'), upgrade: t('reverse.upgrade'), unknown: t('reverse.unknown'), disabled: t('reverse.disabled')
  }
  const qc = useQueryClient()
  const url = `/api/admin/nodes/${node.id}/reverse-connections`
  const q = useQuery({ queryKey: ['reverse', node.id], queryFn: () => api.get<Connection[]>(url), refetchInterval: 10000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UserGroup[]>('/api/admin/groups') })
  const [opened, setOpened] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [draft, setDraft] = useState<Connection[]>([])
  const [expected, setExpected] = useState<Record<string, number>>({})
  const [sni, setSni] = useState('')
  const [advanced, setAdvanced] = useState<number | null>(null)
  useSettingsDirty(opened && dirty)
  const details = useQueries({ queries: (nodes.data ?? []).filter(n => n.id !== node.id && n.proxy_allowed !== false && n.paired).map(n => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<Detail>(`/api/admin/nodes/${n.id}`), enabled: opened })) })
  const detail = (id: number) => details.find(d => d.data?.node.id === id)?.data
  const name = (id: number) => nodes.data?.find(n => n.id === id)?.name ?? `#${id}`
  const editable = !readOnly && node.proxy_allowed !== false && node.paired
  const handledRequest = useRef(0)
  const edit = useCallback(() => {
    if (!editable || !q.data || q.isError) return
    const current = (q.data ?? []).filter(c => c.exit_id === node.id)
    setDraft(structuredClone(current)); setExpected(Object.fromEntries(current.map(c => [c.id, c.version])))
    setDirty(false); setAdvanced(null); setOpened(true)
  }, [editable, q.data, q.isError, node.id])
  useEffect(() => {
    if (editRequest > 0 && handledRequest.current !== editRequest && editable && q.data && !q.isError) {
      handledRequest.current = editRequest
      edit()
      onEditRequestHandled?.()
    }
  }, [editRequest, editable, q.data, q.isError, edit, onEditRequestHandled])
  const close = () => {
    if (!dirty) { setOpened(false); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: () => { setOpened(false); setDirty(false) } })
  }
  const update = (index: number, value: Partial<Connection>) => { setDirty(true); setDraft(ds => ds.map((d, i) => i === index ? { ...d, ...value } : d)) }
  const updateTarget = (transitID: number, target: string) => { setDirty(true); setDraft(ds => ds.map(d => d.transit_id === transitID ? withReverseTarget(d, target) : d)) }
  const save = useMutation({ mutationFn: () => api.put(url, { expected, links: draft }), onSuccess: () => {
    setDirty(false); setOpened(false); toast.ok(t('common.saved'))
    for (const key of ['reverse', 'node', 'entries']) qc.invalidateQueries({ queryKey: [key] })
  }, onError: toast.err })
  const select = (ids: string[]) => {
    setDirty(true)
    setDraft(ids.map(id => {
      const existing = draft.find(c => c.transit_id === Number(id)) ?? q.data?.find(c => c.exit_id === node.id && c.transit_id === Number(id)); if (existing) return structuredClone(existing)
      const d = detail(Number(id)); const g = d?.ingresses?.find(g => g.require_ingress)
      const used = d?.inbounds.map(i => i.Port) ?? []
      const free = (start: number, exclude: number[]) => { while (exclude.includes(start) && start < 65535) start++; return start }
      const port = g ? firstFreeIngressPort(g, used) : free(18445, used)
      const tunnel = g ? firstFreeIngressPort(g, [...used, port]) : free(38921, [...used, port])
      return { id: '', exit_id: node.id, transit_id: Number(id), version: 0, user_inbound_id: 0, name: `${name(Number(id))} → ${node.name}`, port, tunnel_port: tunnel, ingress_id: g?.id ?? null, group_id: null, server_name: sni, enabled: true, protocol: 'vless', state: 'pending', host: '', public_port: 0, public_tunnel_port: 0 }
    }))
  }
  const a = draft.find(d => d.transit_id === advanced)
  return <Stack>
    <Text size="sm" c="dimmed">{t('reverse.hint')}</Text><Text size="xs" c="dimmed">{t('reverse.retryHint')}</Text>
    {q.isError && <Alert color="red">{t('reverse.loadFailed')} <Button variant="subtle" onClick={() => q.refetch()}>{t('common.refresh')}</Button></Alert>}
    {(q.data ?? []).map(c => <Card key={c.id} withBorder p="sm"><Group justify="space-between" align="flex-start">
      <Stack gap={4}><Text fw={600}>{c.name}</Text><Text size="xs" c="dimmed"><Link to={`/nodes/${c.transit_id}`}>{name(c.transit_id)}</Link> → <Link to={`/entries?tab=lines&section=reverse&server=${c.exit_id}`}>{name(c.exit_id)}</Link> · {hostPort(c.host, c.public_port)}</Text>
        {c.exit_id !== node.id && <Button component={Link} to={`/entries?tab=lines&section=reverse&server=${c.exit_id}`} variant="subtle" size="compact-xs">{t('reverse.manageExit')}</Button>}
      </Stack><Badge color={c.state === 'connected' ? 'teal' : c.state === 'disconnected' ? 'red' : 'gray'}>{stateLabels[c.state] ?? stateLabels.unknown}</Badge>
    </Group></Card>)}
    {!readOnly && <Group><Button onClick={edit} disabled={!editable || !q.data || q.isError}>{t('reverse.manage')}</Button></Group>}
    <Modal opened={!readOnly && opened} onClose={close} title={t('reverse.title')} size="xl" closeOnClickOutside={false}>
      <Stack>
        <Alert>{t('reverse.scope')}</Alert>
        <Alert color="yellow">{t('reverse.limits')}</Alert>
        <Group align="flex-end"><TextInput label={t('reverse.defaultSNI')} value={sni} onChange={e => setSni(e.currentTarget.value)} style={{ flex: 1 }} />
          <Button variant="light" disabled={!draft.length || !sni.trim()} onClick={() => modals.openConfirmModal({ title: t('reverse.applyCommon'), children: <Text>{t('reverse.applyHint', { count: draft.length })}</Text>, labels: { confirm: t('common.save'), cancel: t('common.cancel') }, onConfirm: () => { setDraft(ds => ds.map(d => withReverseTarget(d, sni.trim()))); setDirty(true) } })}>{t('reverse.applyCommon')}</Button>
        </Group>
        <Text size="xs" c="dimmed">{t('reverse.sniHint')}</Text>
        <MultiSelect searchable label={t('reverse.transits')} value={draft.map(d => String(d.transit_id))} data={(nodes.data ?? []).filter(n => n.id !== node.id && n.proxy_allowed !== false && n.paired).map(n => ({ value: String(n.id), label: n.name, disabled: !detail(n.id) }))} onChange={select} />
        {details.some(d => d.isError) && <Alert color="red">{t('reverse.loadFailed')}</Alert>}
        {draft.map((d, i) => {
          const dt = detail(d.transit_id); const gs = dt?.ingresses ?? []; const g = gs.find(g => g.id === d.ingress_id)
          const host = g ? g.entry_domain || g.entry_host : dt?.node.domain || dt?.node.public_addr || ''
          return <Card key={d.transit_id} withBorder><Stack gap="sm">
            <Group justify="space-between"><Text fw={600}>{name(d.transit_id)}</Text><Switch label={t('common.enabled')} checked={d.enabled} onChange={e => update(i, { enabled: e.currentTarget.checked })} /></Group>
            <TextInput label={t('entries.name')} value={d.name} onChange={e => update(i, { name: e.currentTarget.value })} />
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <NumberInput label={t('reverse.userPort')} value={d.port} min={1} max={65535} onChange={v => update(i, { port: Number(v) })} />
              <NumberInput label={t('reverse.tunnelPort')} value={d.tunnel_port} min={1} max={65535} onChange={v => update(i, { tunnel_port: Number(v) })} />
              <Select label={t('inbounds.ingress')} value={d.ingress_id ? String(d.ingress_id) : ''} allowDeselect={false} data={[{ value: '', label: t('inbounds.ingressDirect'), disabled: gs.some(g => g.require_ingress) }, ...gs.map(g => ({ value: String(g.id), label: g.name }))]} onChange={v => update(i, { ingress_id: v ? Number(v) : null })} />
              <Select label={t('inbounds.group')} clearable value={d.group_id ? String(d.group_id) : null} data={(groups.data ?? []).map(g => ({ value: String(g.ID), label: g.Name }))} onChange={v => update(i, { group_id: v ? Number(v) : null })} />
            </SimpleGrid>
            <TextInput required label={t('reverse.sni')} value={d.server_name} onChange={e => updateTarget(d.transit_id, e.currentTarget.value)} />
            <Text size="xs" c="dimmed">{t('reverse.scanFrom', { node: name(d.transit_id) })}</Text>
            {opened && advanced !== d.transit_id && <RealityScan current={d.server_name} scan={(hosts, signal) => scanRealityViaNode(d.transit_id, hosts, signal)} onPick={host => updateTarget(d.transit_id, host)} />}
            <Text size="xs" c="dimmed">{t('reverse.preview', { user: hostPort(host, g ? ingressPort(g, d.port) : d.port), tunnel: hostPort(host, g ? ingressPort(g, d.tunnel_port) : d.tunnel_port) })}</Text>
            <Group><Badge variant="light">{d.protocol} · Xray</Badge><Button variant="subtle" size="compact-xs" onClick={() => setAdvanced(d.transit_id)}>{t('reverse.protocol')}</Button></Group>
          </Stack></Card>
        })}
        <Text size="sm" c="dimmed">{t('reverse.removal')}</Text>
        <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button disabled={!editable || !dirty || draft.some(d => !detail(d.transit_id) || !d.server_name.trim())} loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
      </Stack>
    </Modal>
    <Modal opened={!readOnly && opened && !!a} onClose={() => setAdvanced(null)} title={t('reverse.protocol')} size="xl" closeOnClickOutside={false}>
      {opened && a && <InboundForm key={a.transit_id} fixedCore="xray" nodeID={a.transit_id} initial={toValues({ ID: a.user_inbound_id, NodeID: a.transit_id, Tag: a.id ? `rv-user-${a.id}` : 'reverse-user', Protocol: a.protocol, Listen: a.listen || '', Port: a.port, Core: 'xray', GroupID: a.group_id, Enabled: a.enabled, Sort: 0, IngressID: a.ingress_id, Settings: withReverseTarget(a, a.server_name).settings ?? { flow: 'xtls-rprx-vision', tls: { mode: 2, server_name: a.server_name, reality: { handshake_server: a.server_name, handshake_port: 443 } } } })} ingresses={detail(a.transit_id)?.ingresses} groups={groups.data ?? []} busy={false} onCancel={() => setAdvanced(null)} onSubmit={v => {
        if (v.NewIngress) { toast.err(new Error(t('reverse.existingIngress'))); return }
        const p = toPayload(v)
        setDraft(ds => ds.map(d => d.transit_id === a.transit_id ? withReverseProtocol({ ...d, listen: p.Listen, protocol: p.Protocol, port: p.Port, ingress_id: p.IngressID, group_id: p.GroupID }, p.Settings) : d))
        setDirty(true); setAdvanced(null)
      }} />}
    </Modal>
  </Stack>
}
