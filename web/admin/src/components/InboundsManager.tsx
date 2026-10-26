import { ActionIcon, Alert, Badge, Button, Code, Group, Loader, Modal, Stack, Table, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Group as UserGroup, type Inbound, type Ingress } from '../lib/api'
import { useAuth } from '../lib/auth'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { InboundForm, toPayload, toValues, type InboundValues } from './InboundForm'
import { ingressPayload } from './IngressesCard'
import { ServerSelector, type ServerProxyDetail, type ServerSelectionProps } from './LinesConnections'

interface Props extends ServerSelectionProps {
  editInboundID?: number | 'new'
  onEditorClose?: () => void
}

export function InboundsManager(props: Props) {
  return <SettingsDraftBoundary><Manager key={`${props.serverID ?? 0}:${props.editInboundID ?? ''}`} {...props} /></SettingsDraftBoundary>
}

function Manager({ serverID, onServerChange, canWrite = true, editInboundID, onEditorClose }: Props) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const permitted = me?.role === 'admin' || me?.role === 'operator'
  const q = useQuery({ queryKey: ['node', String(serverID)], queryFn: () => api.get<ServerProxyDetail>(`/api/admin/nodes/${serverID}`), enabled: !!serverID && permitted, refetchInterval: 10_000 })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UserGroup[]>('/api/admin/groups'), enabled: permitted })
  const [localEditing, setEditing] = useState<Inbound | 'new' | null | undefined>()
  const [dirty, setDirty] = useState(false)
  const createdIngress = useRef(new Map<string, Ingress>())
  const data = q.data
  const selectionAllowed = canWrite && permitted && !!data?.node.paired && data.node.proxy_allowed !== false
  const editable = selectionAllowed && !q.isError && !groups.isPending && !groups.isError
  const requested = editInboundID === 'new' ? 'new' : data?.inbounds.find(ib => ib.ID === editInboundID)
  const requestedEditable = requested && (requested === 'new' || (!requested.Settings.reverse && !requested.Settings.wireguard_slot))
  const editing = localEditing === undefined ? (selectionAllowed && groups.data && requestedEditable ? requested : null) : localEditing
  useSettingsDirty(dirty)
  // Clear the deep link after the draft boundary has observed the saved/discarded state.
  useEffect(() => {
    if (localEditing !== null || dirty || !editInboundID || !onEditorClose) return
    const frame = requestAnimationFrame(onEditorClose)
    return () => cancelAnimationFrame(frame)
  }, [localEditing, dirty, editInboundID, onEditorClose])
  const close = () => {
    const finish = () => { setEditing(null); setDirty(false) }
    if (!dirty) { finish(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: finish })
  }
  const invalidate = () => {
    for (const key of ['node', 'nodes', 'entries', 'node-access']) void qc.invalidateQueries({ queryKey: [key] })
  }
  const save = useMutation({ mutationFn: async (v: InboundValues) => {
    if (!editable || !editing || !data) throw new Error(t('workspace.forbidden'))
    if (v.NewIngress) {
      const payload = ingressPayload(v.NewIngress)
      const key = `${data.node.id}:${JSON.stringify(payload)}`
      let ingress = createdIngress.current.get(key)
      if (!ingress) {
        const r = await api.post<{ ingress: Ingress; dns?: DNSResult[] }>(`/api/admin/nodes/${data.node.id}/ingresses`, payload)
        ingress = r.ingress
        createdIngress.current.set(key, ingress)
        dnsToast(r.dns)
      }
      v = { ...v, IngressID: String(ingress.id), NewIngress: undefined }
    }
    return editing === 'new' ? api.post(`/api/admin/nodes/${data.node.id}/inbounds`, toPayload(v)) : api.patch(`/api/admin/inbounds/${editing.ID}`, toPayload(v))
  }, onSuccess: () => { setEditing(null); setDirty(false); invalidate(); toast.ok(t('common.saved')) }, onError: toast.err })
  const remove = useMutation({ mutationFn: (ib: Inbound) => {
    if (!editable || ib.Settings.reverse || ib.Settings.wireguard_slot) throw new Error(t('workspace.forbidden'))
    return api.del(`/api/admin/inbounds/${ib.ID}`)
  }, onSuccess: () => { invalidate(); toast.ok(t('common.deleted')) }, onError: toast.err })
  if (!permitted) return <Alert color="orange">{t('workspace.forbidden')}</Alert>
  return <Stack>
    <ServerSelector serverID={serverID} onServerChange={onServerChange} />
    <Text size="sm" c="dimmed">{t('inbounds.subtitle')}</Text>
    {!!serverID && q.isPending && <Loader size="sm" />}
    {(q.isError || groups.isError) && <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => { void q.refetch(); void groups.refetch() }}>{t('workspace.retry')}</Button></Alert>}
    {data && <>
      {!data.node.paired && <Alert color="orange">{t('nodes.unpaired')}</Alert>}
      {data.node.proxy_allowed === false && <Alert color="orange">{t('nodeMode.monitorHint')}</Alert>}
      <Group justify="space-between"><Text fw={600}>{t('inbounds.title')}</Text>{canWrite && <Button size="xs" disabled={!editable || groups.isPending} leftSection={<IconPlus size={14} />} onClick={() => { setEditing('new'); setDirty(false) }}>{t('inbounds.create')}</Button>}</Group>
      <Table.ScrollContainer minWidth={550}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('inbounds.tag')}</Table.Th><Table.Th>{t('inbounds.protocol')}</Table.Th><Table.Th>{t('inbounds.port')}</Table.Th><Table.Th>{t('inbounds.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>{data.inbounds.map(ib => <Table.Tr key={ib.ID}>
          <Table.Td>{ib.Tag}</Table.Td><Table.Td><Badge>{ib.Protocol}</Badge></Table.Td><Table.Td><Code>{ib.Listen || '::'}:{ib.Port}</Code></Table.Td>
          <Table.Td><Badge color={ib.Enabled ? 'teal' : 'gray'}>{t(ib.Enabled ? 'common.enabled' : 'common.disabled')}</Badge></Table.Td>
          <Table.Td>{ib.Settings.reverse || ib.Settings.wireguard_slot ? <Button component={Link} variant="subtle" size="compact-xs" to={`/entries?node=${data.node.id}`}>{t('reverse.managed')}</Button> : <Group gap={4} justify="flex-end">
            {canWrite && <Button component={Link} to={`/entries?node=${data.node.id}&inbound=${ib.ID}&create=node`} size="compact-xs" variant="subtle">{t('entries.create')}</Button>}
            {editable && <><ActionIcon aria-label={t('common.edit')} variant="subtle" onClick={() => { setEditing(ib); setDirty(false) }}><IconPencil size={16} /></ActionIcon><ActionIcon aria-label={t('common.delete')} variant="subtle" color="red" disabled={remove.isPending} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text>{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => remove.mutate(ib) })}><IconTrash size={16} /></ActionIcon></>}
          </Group>}</Table.Td>
        </Table.Tr>)}</Table.Tbody>
      </Table></Table.ScrollContainer>
      {data.inbounds.length === 0 && <Text size="sm" c="dimmed">{t('common.empty')}</Text>}
      <Modal opened={editing !== null} onClose={close} title={editing === 'new' ? t('inbounds.create') : t('common.edit')} size="xl" closeOnClickOutside={false}>
        {editing !== null && <InboundForm key={editing === 'new' ? 'new' : editing.ID} nodeID={data.node.id} domain={data.node.domain} decoyDomain={data.node.decoy_enabled ? data.node.domain : undefined} lineOnly={!data.node.public_addr && !data.node.domain} ingresses={data.ingresses ?? []} usedPorts={data.inbounds.filter(ib => editing === 'new' || ib.ID !== editing.ID).map(ib => ib.Port)} initial={toValues(editing === 'new' ? undefined : editing)} groups={groups.data ?? []} busy={save.isPending || !editable} onDirtyChange={setDirty} onSubmit={v => save.mutate(v)} onCancel={close} />}
      </Modal>
    </>}
  </Stack>
}
