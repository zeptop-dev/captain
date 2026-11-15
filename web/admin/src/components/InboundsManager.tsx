import { ActionIcon, Alert, Badge, Button, Code, Group, Loader, Modal, Select, Stack, Table, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Entry, type Group as UserGroup, type Inbound, type Ingress, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { inboundDirectory, inboundUsage, managedInboundDestination } from '../lib/inbound-directory'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { standaloneConnection } from '../lib/standalone'
import { InboundForm, toPayload, toValues, type InboundValues } from './InboundForm'
import { ingressPayload } from './IngressesCard'
import { ServerSelector, type ServerProxyDetail, type ServerSelectionProps } from './LinesConnections'

interface Props extends ServerSelectionProps {
  global?: boolean
  editInboundID?: number | 'new'
  onEditorClose?: () => void
}

export function InboundsManager(props: Props) {
  return <SettingsDraftBoundary><Manager key={`${props.global ? 'all' : props.serverID ?? 0}:${props.editInboundID ?? ''}`} {...props} /></SettingsDraftBoundary>
}

function Manager({ serverID, onServerChange, canWrite = true, editInboundID, onEditorClose, global = false }: Props) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const permitted = me?.role === 'admin' || me?.role === 'operator'
  const q = useQuery({ queryKey: ['node', String(serverID)], queryFn: () => api.get<ServerProxyDetail>(`/api/admin/nodes/${serverID}`), enabled: !!serverID && permitted, refetchInterval: 10_000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes'), enabled: global && permitted })
  const ownerQueries = useQueries({ queries: global && permitted ? (nodes.data ?? []).map(node => ({ queryKey: ['node', String(node.id)], queryFn: () => api.get<ServerProxyDetail>(`/api/admin/nodes/${node.id}`), refetchInterval: 10_000 })) : [] })
  const entries = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries'), enabled: global && permitted })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UserGroup[]>('/api/admin/groups'), enabled: permitted })
  const owners = global ? ownerQueries.flatMap(query => query.data ? [query.data] : []) : q.data ? [q.data] : []
  const failedOwners = new Set((nodes.data ?? []).filter((_, index) => ownerQueries[index]?.isError).map(node => node.id))
  const ownerFor = (id?: number) => owners.find(owner => owner.node.id === id) ?? (q.data?.node.id === id ? q.data : undefined)
  const [protocol, setProtocol] = useState<string | null>(null)
  const [usage, setUsage] = useState<string | null>(null)
  const rows = inboundDirectory(owners, entries.data ?? [], serverID, protocol, usage)
  const [localEditing, setEditing] = useState<Inbound | 'new' | null | undefined>()
  const [editingServer, setEditingServer] = useState<number | null | undefined>()
  const [dirty, setDirty] = useState(false)
  const createdIngress = useRef(new Map<string, Ingress>())
  const requested = editInboundID === 'new' ? 'new' : ownerFor(serverID)?.inbounds.find(ib => ib.ID === editInboundID)
  const targetServer = editingServer === null ? undefined : editingServer ?? (localEditing && localEditing !== 'new' ? localEditing.NodeID : serverID)
  const data = ownerFor(targetServer)
  const ready = (owner?: ServerProxyDetail) => canWrite && permitted && !!owner?.node.paired && owner.node.proxy_allowed !== false && !failedOwners.has(owner.node.id) && !(q.isError && q.data?.node.id === owner.node.id) && !groups.isPending && !groups.isError
  const editable = ready(data)
  const canStart = canWrite && permitted && !groups.isPending && !groups.isError && (!global || !nodes.isPending && !nodes.isError)
  const requestedEditable = requested && (requested === 'new' || (!requested.Settings.reverse && !requested.Settings.wireguard_slot && (!requested.Settings.standalone || me?.role === 'admin')))
  const editing = localEditing === undefined ? (canStart && (!serverID && global || editable) && requestedEditable ? requested : null) : localEditing
  useSettingsDirty(dirty)
  useEffect(() => {
    if (localEditing !== null || dirty || !editInboundID || !onEditorClose) return
    const frame = requestAnimationFrame(onEditorClose)
    return () => cancelAnimationFrame(frame)
  }, [localEditing, dirty, editInboundID, onEditorClose])
  const close = () => {
    if (save.isPending) return
    const finish = () => { setEditing(null); setEditingServer(undefined); setDirty(false) }
    if (!dirty) { finish(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: finish })
  }
  const changeEditorServer = (id: number | null) => {
    if (save.isPending) return
    const finish = () => { setEditingServer(id); setDirty(false) }
    if (!dirty) { finish(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: finish })
  }
  const invalidate = () => {
    for (const key of ['node', 'nodes', 'entries', 'node-access']) void qc.invalidateQueries({ queryKey: [key] })
  }
  const save = useMutation({ mutationFn: async (values: InboundValues) => {
    if (!editable || !editing || !data) throw new Error(t('workspace.forbidden'))
    let v = values
    // Bind this mutation to the actual editor owner, independently of list filters.
    const ownerID = data.node.id
    if (editing !== 'new' && editing.NodeID !== ownerID) throw new Error(t('workspace.forbidden'))
    if (v.NewIngress) {
      const payload = ingressPayload(v.NewIngress)
      const key = `${ownerID}:${JSON.stringify(payload)}`
      let ingress = createdIngress.current.get(key)
      if (!ingress) {
        const r = await api.post<{ ingress: Ingress; dns?: DNSResult[] }>(`/api/admin/nodes/${ownerID}/ingresses`, payload)
        ingress = r.ingress
        createdIngress.current.set(key, ingress)
        dnsToast(r.dns)
      }
      v = { ...v, IngressID: String(ingress.id), NewIngress: undefined }
    }
    return editing === 'new' ? api.post(`/api/admin/nodes/${ownerID}/inbounds`, toPayload(v)) : api.patch(`/api/admin/inbounds/${editing.ID}`, toPayload(v))
  }, onSuccess: () => { setEditing(null); setEditingServer(undefined); setDirty(false); invalidate(); toast.ok(t('common.saved')) }, onError: toast.err })
  const remove = useMutation({ mutationFn: (ib: Inbound) => {
    if (!ready(ownerFor(ib.NodeID)) || ib.Settings.reverse || ib.Settings.wireguard_slot) throw new Error(t('workspace.forbidden'))
    return api.del(`/api/admin/inbounds/${ib.ID}`)
  }, onSuccess: () => { invalidate(); toast.ok(t('common.deleted')) }, onError: toast.err })
  const loading = global ? nodes.isPending || ownerQueries.some(query => query.isPending) : !!serverID && q.isPending
  const error = global ? nodes.isError || ownerQueries.some(query => query.isError) || entries.isError : q.isError
  const retry = () => { if (global) { void nodes.refetch(); void entries.refetch(); ownerQueries.forEach(query => { void query.refetch() }) } else if (serverID) void q.refetch(); void groups.refetch() }
  if (!permitted) return <Alert color="orange">{t('workspace.forbidden')}</Alert>
  return <Stack>
    <Group align="flex-end">
      <ServerSelector serverID={serverID} onServerChange={onServerChange} placeholder={global ? t('entryFlow.allServers') : undefined} />
      {global && <>
        <Select label={t('inbounds.protocol')} clearable searchable placeholder={t('inboundDirectory.allProtocols')} value={protocol} onChange={setProtocol} data={[...new Set([...owners.flatMap(owner => owner.inbounds.map(ib => ib.Protocol)), ...(protocol ? [protocol] : [])])]} />
        <Select label={t('standalone.mode')} clearable placeholder={t('inboundDirectory.allUsage')} value={usage} onChange={setUsage} data={[{ value: 'plan', label: t('standalone.plan') }, { value: 'standalone', label: t('standalone.independent') }, { value: 'managed', label: t('inboundDirectory.managed') }]} />
      </>}
    </Group>
    {!global && <Text size="sm" c="dimmed">{t('inbounds.subtitle')}</Text>}
    {loading && <Loader size="sm" />}
    {(error || groups.isError) && <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={retry}>{t('workspace.retry')}</Button></Alert>}
    {serverID && ownerFor(serverID)?.node.paired === false && <Alert color="orange">{t('nodes.unpaired')}</Alert>}
    {serverID && ownerFor(serverID)?.node.proxy_allowed === false && <Alert color="orange">{t('nodeMode.monitorHint')}</Alert>}
    {(global || q.data) && <>
      <Group justify="space-between"><Text fw={600}>{global ? t('inboundDirectory.count', { count: rows.length }) : t('inbounds.title')}</Text>{canWrite && <Button size="xs" disabled={!canStart || !!serverID && !ready(ownerFor(serverID))} leftSection={<IconPlus size={14} />} onClick={() => { setEditingServer(serverID); setEditing('new'); setDirty(false) }}>{t('inbounds.create')}</Button>}</Group>
      <Table.ScrollContainer minWidth={global ? 1050 : 550}><Table style={{ tableLayout: 'auto' }}>
        <Table.Thead><Table.Tr>{global && <Table.Th>{t('entryFlow.targetServer')}</Table.Th>}<Table.Th>{t('inbounds.tag')}</Table.Th><Table.Th>{t('inbounds.protocol')}</Table.Th><Table.Th>{t('inbounds.port')}</Table.Th><Table.Th>{t('standalone.mode')}</Table.Th>{global && <Table.Th>{t('inboundDirectory.linkedNodes')}</Table.Th>}<Table.Th>{t('inbounds.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>{rows.map(({ ib, owner, entries: linked }) => <Table.Tr key={ib.ID}>
          {global && <Table.Td><Text component={Link} to={`/nodes/${owner.node.id}`} size="sm">{owner.node.name}</Text></Table.Td>}
          <Table.Td>{ib.Tag}</Table.Td><Table.Td><Badge>{ib.Protocol}</Badge></Table.Td><Table.Td><Code>{ib.Listen || '::'}:{ib.Port}</Code></Table.Td>
          <Table.Td><Badge variant="light" color={ib.Settings.standalone ? 'grape' : 'blue'}>{t(inboundUsage(ib) === 'managed' ? 'inboundDirectory.managed' : ib.Settings.standalone ? 'standalone.independent' : 'standalone.plan')}</Badge></Table.Td>
          {global && <Table.Td><Text size="sm" title={linked.map(entry => entry.Name).join(' / ')}>{entries.isPending || entries.isError ? t('nodeAccess.unknown') : linked.length}</Text></Table.Td>}
          <Table.Td><Badge color={ib.Enabled ? 'teal' : 'gray'}>{t(ib.Enabled ? 'common.enabled' : 'common.disabled')}</Badge></Table.Td>
          <Table.Td>{ib.Settings.reverse || ib.Settings.wireguard_slot ? <Button component={Link} variant="subtle" size="compact-xs" to={managedInboundDestination(ib.ID, owner.node.id, entries.data ?? [])}>{t('inboundDirectory.manageConnection')}</Button> : <Group gap={4} justify="flex-end">
            {canWrite && !ib.Settings.standalone && <Button component={Link} to={`/entries?node=${owner.node.id}&inbound=${ib.ID}&create=node`} size="compact-xs" variant="subtle">{t('entries.create')}</Button>}
            {!!ib.Settings.standalone && <Button size="compact-xs" variant="subtle" disabled={!standaloneConnection({ ib, node: owner.node, ingress: owner.ingresses?.find(g => g.id === ib.IngressID) })} onClick={async () => { try { const value = standaloneConnection({ ib, node: owner.node, ingress: owner.ingresses?.find(g => g.id === ib.IngressID) }); if (value) { await navigator.clipboard.writeText(value); toast.ok(t('standalone.copied')) } } catch (err) { toast.err(err) } }}>{t('standalone.copy')}</Button>}
            {ready(owner) && (!ib.Settings.standalone || me?.role === 'admin') && <><ActionIcon aria-label={t('common.edit')} variant="subtle" onClick={() => { setEditingServer(owner.node.id); setEditing(ib); setDirty(false) }}><IconPencil size={16} /></ActionIcon><ActionIcon aria-label={t('common.delete')} variant="subtle" color="red" disabled={remove.isPending} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text>{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => remove.mutate(ib) })}><IconTrash size={16} /></ActionIcon></>}
          </Group>}</Table.Td>
        </Table.Tr>)}</Table.Tbody>
      </Table></Table.ScrollContainer>
      {!loading && rows.length === 0 && <Text size="sm" c="dimmed">{t('common.empty')}</Text>}
    </>}
    <Modal opened={editing !== null} onClose={close} title={editing === 'new' ? t('inbounds.create') : t('common.edit')} size="xl" closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending}>
      {global && editing === 'new' && <ServerSelector serverID={editingServer === null ? undefined : editingServer ?? serverID} onServerChange={changeEditorServer} />}
      {global && editing && editing !== 'new' && <Text size="sm" c="dimmed" mb="sm">{data?.node.name}</Text>}
      {editing !== null && !data && <Text size="sm" c="dimmed">{t('inboundDirectory.chooseServer')}</Text>}
      {data && editing !== null && !ready(data) && <Alert color="orange">{t(!data.node.paired ? 'nodes.unpaired' : data.node.proxy_allowed === false ? 'nodeMode.monitorHint' : 'workspace.loadError')}</Alert>}
      {data && editing !== null && (editing !== 'new' || ready(data)) && <InboundForm allowStandalone={me?.role === 'admin'} key={`${data.node.id}:${editing === 'new' ? 'new' : editing.ID}`} node={data.node} nodeID={data.node.id} domain={data.node.domain} decoyDomain={data.node.decoy_enabled ? data.node.domain : undefined} lineOnly={!data.node.public_addr && !data.node.domain} ingresses={data.ingresses ?? []} usedPorts={data.inbounds.filter(ib => editing === 'new' || ib.ID !== editing.ID).map(ib => ib.Port)} initial={toValues(editing === 'new' ? undefined : editing)} groups={groups.data ?? []} busy={save.isPending || !editable} onDirtyChange={setDirty} onSubmit={v => save.mutate(v)} onCancel={close} />}
    </Modal>
  </Stack>
}
