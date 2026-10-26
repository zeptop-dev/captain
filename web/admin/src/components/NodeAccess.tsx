import { Alert, Badge, Button, Card, Checkbox, Group, Loader, Modal, MultiSelect, ScrollArea, Select, Stack, Text, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNodeAccess, useInvalidateNodeAccess } from '../lib/node-access-query'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { accessChanged, accessDraft, availableToGroup, changedAccessPayload, groupInboundIDs, groupSelection, sameIDs, sharedEntries, validAccess, type AccessDraft, type InboundAccess, type NodeAccessDirectory } from '../lib/node-access'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

export function AccessSummary({ row, directory }: { row?: InboundAccess; directory?: NodeAccessDirectory }) {
  const { t } = useTranslation()
  const label = !row ? t('nodeAccess.unknown') : row.policy === 'groups' ? t('nodeAccess.groupSummary', { groups: row.group_ids.map(id => directory?.groups.find(g => g.id === id)?.name ?? `#${id}`).join(' / ') }) : t(`nodeAccess.${row.policy}`)
  return <Text size="xs" c="dimmed" maw={260} lineClamp={2} title={label}>{label}</Text>
}
export function AccessFields({ draft, directory, onChange, entryID, busy = false }: { draft: AccessDraft; directory: NodeAccessDirectory; onChange: (draft: AccessDraft) => void; entryID?: number; busy?: boolean }) {
  const { t } = useTranslation()
  const shared = sharedEntries(draft.source, entryID)
  const changed = accessChanged(draft)
  const plans = directory.plans.filter(p => draft.policy === 'all' || draft.policy === 'groups' && p.group_id !== null && draft.group_ids.includes(p.group_id))
  return <Stack gap="xs">
    <Select label={t('nodeAccess.title')} description={t('nodeAccess.scopeHint')} allowDeselect={false} value={draft.policy} disabled={busy || !draft.source.editable} data={(['all', 'groups', 'none'] as const).map(value => ({ value, label: t(`nodeAccess.${value}`) }))} onChange={value => onChange({ ...draft, policy: value as AccessDraft['policy'], confirm_shared: false })} />
    {draft.policy === 'groups' && <MultiSelect required searchable label={t('nodeAccess.groups')} data={directory.groups.map(g => ({ value: String(g.id), label: g.name }))} value={draft.group_ids.map(String)} disabled={busy || !draft.source.editable} error={!draft.group_ids.length ? t('nodeAccess.pickGroup') : undefined} onChange={values => onChange({ ...draft, group_ids: values.map(Number), confirm_shared: false })} />}
    <Text size="xs" c="dimmed">{t('nodeAccess.relatedPlans')}: {plans.map(p => p.name).join(' / ') || t('common.none')}</Text>
    {shared.length > 0 && <Alert color="yellow"><Text size="sm">{t('nodeAccess.sharedHint')}</Text><Text size="sm" mt={4}>{shared.map(e => e.name).join(' / ')}</Text>{changed && <Checkbox mt="sm" label={t('nodeAccess.confirmShared')} checked={draft.confirm_shared} disabled={busy} onChange={e => onChange({ ...draft, confirm_shared: e.currentTarget.checked })} />}</Alert>}
    {!draft.source.editable && <Text size="xs" c="dimmed">{t('nodeAccess.receiver')}</Text>}
  </Stack>
}
function LoadFailure({ retry }: { retry: () => void }) {
  const { t } = useTranslation()
  return <Alert color="red">{t('nodeAccess.loadError')} <Button size="compact-xs" variant="subtle" onClick={retry}>{t('common.refresh')}</Button></Alert>
}
function discardThen(action: () => void, dirty: boolean, t: ReturnType<typeof useTranslation>['t']) {
  if (!dirty) { action(); return }
  modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: action })
}
export function InboundAccessControl({ inboundID }: { inboundID: number }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const q = useNodeAccess()
  const [opened, setOpened] = useState(false)
  if (me?.role !== 'admin') return <Text size="xs" c="dimmed">{t('nodeAccess.adminOnly')}</Text>
  return <Stack gap={4}><Text size="sm" fw={500}>{t('nodeAccess.title')}</Text><AccessSummary row={q.isError ? undefined : q.data?.inbounds.find(r => r.inbound_id === inboundID)} directory={q.data} /><Button size="compact-xs" variant="subtle" w="fit-content" onClick={() => setOpened(true)}>{t('nodeAccess.manage')}</Button>{opened && <InboundAccessModal inboundID={inboundID} onClose={() => setOpened(false)} />}</Stack>
}
export function InboundAccessModal({ inboundID, entryID, onClose }: { inboundID: number; entryID?: number; onClose: () => void }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const q = useNodeAccess()
  const invalidate = useInvalidateNodeAccess()
  const [draft, setDraft] = useState<AccessDraft | null>(null)
  const current = q.data?.inbounds.find(row => row.inbound_id === inboundID)
  const value = draft ?? (current ? accessDraft(current) : null)
  const dirty = !!value && accessChanged(value)
  const stale = !!value && value.source.revision !== q.data?.revision
  const save = useMutation({ mutationFn: async () => {
    if (!value || !q.data || q.isError || !validAccess(value, q.data)) throw new Error(t('nodeAccess.reloadHint'))
    const payload = changedAccessPayload(value, inboundID)
    if (!payload || sharedEntries(value.source, entryID).length && !value.confirm_shared) throw new Error(t('nodeAccess.confirmShared'))
    return api.put(`/api/admin/node-access/inbounds/${inboundID}`, payload)
  }, onSuccess: () => { invalidate(); toast.ok(t('common.saved')); onClose() } })
  useSettingsDirty(dirty || save.isPending)
  const reload = () => discardThen(() => { setDraft(null); save.reset(); void q.refetch() }, dirty, t)
  return <Modal opened onClose={() => { if (!save.isPending) discardThen(onClose, dirty, t) }} title={t('nodeAccess.manage')} size="lg" closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending}>
    <Stack>
      {me?.role !== 'admin' ? <Alert>{t('nodeAccess.adminOnly')}</Alert> : <>
        {q.isPending && <Loader size="sm" />}{q.isError && <LoadFailure retry={reload} />}
        {!q.isPending && !q.isError && !current && <Alert color="red">{t('nodeAccess.missing')}</Alert>}
        {value && q.data && <><Text size="sm">{value.source.node_name} / {value.source.tag} · {value.source.protocol}:{value.source.port}</Text><AccessFields draft={value} directory={q.data} onChange={setDraft} entryID={entryID} busy={save.isPending || q.isError || q.isFetching} /></>}
        {(stale || save.error instanceof ApiError && save.error.status === 409) && <Alert color="yellow">{t('nodeAccess.reloadHint')} <Button variant="subtle" size="compact-xs" onClick={reload}>{t('common.refresh')}</Button></Alert>}
        {save.isError && <Alert color="red">{save.error.message}</Alert>}
        <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={() => discardThen(onClose, dirty, t)}>{t('common.cancel')}</Button><Button loading={save.isPending} disabled={!dirty || !value || !q.data || q.isError || q.isFetching || !validAccess(value, q.data) || !!sharedEntries(value.source, entryID).length && !value.confirm_shared} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
      </>}
    </Stack>
  </Modal>
}
export function PlanNodeAccess({ groupID }: { groupID: number }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const q = useNodeAccess()
  const [openedGroup, setOpenedGroup] = useState<number | null>(null)
  if (me?.role !== 'admin') return null
  const rows = q.data?.inbounds.filter(row => availableToGroup(row, groupID)) ?? []
  return <Card withBorder p="sm"><Stack gap="xs">
    <Group justify="space-between"><Text size="sm" fw={600}>{t('nodeAccess.availableNodes')}</Text><Button size="compact-xs" variant="light" disabled={!groupID || !q.data || q.isError || q.isFetching} onClick={() => setOpenedGroup(groupID)}>{t('nodeAccess.manageNodes')}</Button></Group>
    <Text size="xs" c="dimmed">{t('nodeAccess.planHint')}</Text>
    {!groupID && <Text size="xs" c="dimmed">{t('nodeAccess.noPlanGroup')}</Text>}
    {q.isPending ? <Loader size="sm" /> : q.isError ? <LoadFailure retry={() => { void q.refetch() }} /> : <ScrollArea.Autosize mah={180}><Stack gap={4}>{rows.map(row => <Text size="xs" key={row.inbound_id}>{row.entries.map(e => e.name).join(' / ') || row.tag} <Text span c="dimmed">· {row.node_name} / {row.tag}{row.policy === 'all' ? ` · ${t('nodeAccess.alwaysAvailable')}` : ''}</Text></Text>)}{!rows.length && <Text size="sm" c="dimmed">{t('common.empty')}</Text>}</Stack></ScrollArea.Autosize>}
    {openedGroup === groupID && q.data && <GroupAccessModal key={groupID} groupID={groupID} initial={q.data} onClose={() => setOpenedGroup(null)} />}
  </Stack></Card>
}
function GroupAccessModal({ groupID, initial, onClose }: { groupID: number; initial: NodeAccessDirectory; onClose: () => void }) {
  const { t } = useTranslation()
  const q = useNodeAccess()
  const invalidate = useInvalidateNodeAccess()
  const [snapshot, setSnapshot] = useState(initial)
  const [selected, setSelected] = useState(() => groupInboundIDs(initial, groupID))
  const [confirmed, setConfirmed] = useState(false)
  const [search, setSearch] = useState('')
  const original = groupInboundIDs(snapshot, groupID)
  const dirty = !sameIDs(original, selected)
  const stale = snapshot.revision !== q.data?.revision
  const plans = snapshot.plans.filter(p => p.group_id === groupID)
  const visible = snapshot.inbounds.filter(row => `${row.node_name} ${row.tag} ${row.entries.map(e => e.name).join(' ')}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
  const save = useMutation({ mutationFn: async () => {
    if (stale || q.isError || !confirmed) throw new Error(t('nodeAccess.reloadHint'))
    return api.put(`/api/admin/node-access/groups/${groupID}`, { inbound_ids: groupSelection(snapshot, selected), revision: snapshot.revision, confirm_shared: confirmed })
  }, onSuccess: () => { invalidate(); toast.ok(t('common.saved')); onClose() } })
  useSettingsDirty(dirty || save.isPending)
  const reload = () => discardThen(() => { void q.refetch().then(result => { if (!result.isError && result.data) { setSnapshot(result.data); setSelected(groupInboundIDs(result.data, groupID)); setConfirmed(false); save.reset() } }) }, dirty, t)
  return <Modal opened title={`${t('nodeAccess.manageNodes')} · ${snapshot.groups.find(g => g.id === groupID)?.name ?? `#${groupID}`}`} size="lg" closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} onClose={() => { if (!save.isPending) discardThen(onClose, dirty, t) }}>
    <Stack>
      <Alert color="yellow"><Text size="sm">{t('nodeAccess.groupImpact')}</Text><Text size="sm" mt={4}>{plans.map(p => p.name).join(' / ') || t('nodeAccess.noRelatedPlans')}</Text></Alert>
      <Text size="xs" c="dimmed">{t('nodeAccess.groupSaveHint')}</Text>
      <TextInput aria-label={t('nodeAccess.search')} placeholder={t('nodeAccess.search')} value={search} onChange={e => setSearch(e.currentTarget.value)} />
      <ScrollArea.Autosize mah={340}><Stack gap="sm">{visible.map(row => <Checkbox key={row.inbound_id} checked={selected.includes(row.inbound_id)} disabled={!row.editable || row.policy === 'all' || save.isPending || stale || q.isError} label={<><Text size="sm">{row.entries.map(e => e.name).join(' / ') || row.tag}</Text><Text size="xs" c="dimmed">{row.node_name} / {row.tag} · {row.protocol}:{row.port}</Text>{row.policy === 'all' && <Badge size="xs" variant="light">{t('nodeAccess.alwaysAvailable')}</Badge>}{!row.editable && <Text size="xs" c="dimmed">{t('nodeAccess.receiver')}</Text>}</>} onChange={event => { setSelected(groupSelection(snapshot, event.currentTarget.checked ? [...selected, row.inbound_id] : selected.filter(id => id !== row.inbound_id))); setConfirmed(false) }} />)}{!visible.length && <Text c="dimmed" size="sm">{t('common.empty')}</Text>}</Stack></ScrollArea.Autosize>
      <Text size="xs" c="dimmed">{t('nodeAccess.allFixed')}</Text>
      {dirty && <Checkbox label={t('nodeAccess.confirmGroup')} checked={confirmed} disabled={save.isPending} onChange={e => setConfirmed(e.currentTarget.checked)} />}
      {(stale || save.error instanceof ApiError && save.error.status === 409) && <Alert color="yellow">{t('nodeAccess.reloadHint')} <Button variant="subtle" size="compact-xs" onClick={reload}>{t('common.refresh')}</Button></Alert>}
      {q.isError && <LoadFailure retry={reload} />}{save.isError && <Alert color="red">{save.error.message}</Alert>}
      <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={() => discardThen(onClose, dirty, t)}>{t('common.cancel')}</Button><Button loading={save.isPending} disabled={!dirty || !confirmed || stale || q.isError || q.isFetching} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
    </Stack>
  </Modal>
}
