import { ActionIcon, Alert, Badge, Box, Button, Card, Checkbox, Code, Group, Loader, Modal, NumberInput, Select, SimpleGrid, Stack, TagsInput, Text, TextInput, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { IconArrowDown, IconArrowUp, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Line } from '../lib/api'
import { toast } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { changesetFingerprint, changesetInput, changesetValid, editableChangesetLine, keepSavedInboundSettings, lineTargetProtocol, modelFromDraft, newChangesetGroup, newChangesetMember, newChangesetOperation, operationIsReferenced, operationLabel, reconcileChangesetSave, selectChangesetResource, type ChangeKind, type ChangesetEntry, type ChangesetInbound, type ChangesetModel, type EditableOperation, type TopologyCatalog, type TopologyDraft, type TopologyPreview } from '../lib/topology-changesets'

const base = '/api/admin/topology-changesets'
const kindKey = (kind: ChangeKind) => `topologyChangesets.kind${kind[0].toUpperCase()}${kind.slice(1)}`
const move = <T,>(items: T[], from: number, to: number) => { const result = [...items]; const [item] = result.splice(from, 1); result.splice(to, 0, item); return result }

export function TopologyChangesetsPanel({ onCommitted }: { onCommitted?: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const catalog = useQuery({ queryKey: ['topology-changesets-catalog'], queryFn: () => api.get<TopologyCatalog>(`${base}/catalog`) })
  const drafts = useQuery({ queryKey: ['topology-changesets'], queryFn: () => api.get<TopologyDraft[]>(base) })
  const [selected, setSelected] = useState<string | null>(null)
  const refresh = () => { qc.invalidateQueries({ queryKey: ['topology-changesets'] }); qc.invalidateQueries({ queryKey: ['topology-changesets-catalog'] }) }
  const discard = useMutation({ mutationFn: (draft: TopologyDraft) => api.del(`${base}/${draft.id}?version=${draft.version}`), onSuccess: refresh })
  return <Stack>
    <Group justify="space-between"><Title order={3}>{t('topologyChangesets.title')}</Title><Button onClick={() => setSelected('new')} disabled={!catalog.data || catalog.isError}>{t('topologyChangesets.newDraft')}</Button></Group>
    <Alert color="blue">{t('topologyChangesets.scope')}</Alert>
    {(catalog.isPending || drafts.isPending) && <Loader size="sm" />}
    {(catalog.isError || drafts.isError) && <Alert color="red"><Text>{catalog.error?.message || drafts.error?.message || t('topologyChangesets.catalogError')}</Text><Button variant="subtle" onClick={() => { catalog.refetch(); drafts.refetch() }}>{t('entryFlow.retry')}</Button></Alert>}
    {discard.isError && <Alert color="red">{discard.error.message}</Alert>}
    {!drafts.data?.length && !drafts.isPending && <Text c="dimmed">{t('topologyChangesets.noDrafts')}</Text>}
    {drafts.data?.map(draft => <Card key={draft.id} withBorder><Group justify="space-between" align="flex-start"><Stack gap={4}><Text fw={600}>{draft.name}</Text><Text size="xs" c="dimmed">{new Date(draft.updated_at).toLocaleString()}</Text><Badge variant="light" color={draft.status === 'committed' ? 'teal' : 'gray'}>{t(draft.status === 'committed' ? 'topologyChangesets.statusCommitted' : 'topologyChangesets.statusDraft')}</Badge></Stack><Group><Button variant="default" onClick={() => setSelected(draft.id)} disabled={!catalog.data}>{t('topologyChangesets.open')}</Button>{draft.status === 'draft' && <ActionIcon color="red" variant="subtle" aria-label={t('topologyChangesets.discard')} disabled={discard.isPending} onClick={() => modals.openConfirmModal({ title: t('topologyChangesets.discard'), children: <Text size="sm">{t('topologyChangesets.discardHint')}</Text>, labels: { confirm: t('topologyChangesets.discard'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => discard.mutate(draft) })}><IconTrash size={16} /></ActionIcon>}</Group></Group></Card>)}
    {selected && catalog.data && <DraftLoader key={selected} id={selected} catalog={catalog.data} catalogError={catalog.isError} onRefreshCatalog={() => catalog.refetch()} onClose={() => setSelected(null)} onSaved={refresh} onCommitted={() => { refresh(); onCommitted?.() }} />}
  </Stack>
}
function DraftLoader({ id, ...props }: EditorProps & { id: string }) {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['topology-changeset', id], queryFn: () => api.get<TopologyDraft>(`${base}/${id}`), enabled: id !== 'new', staleTime: Infinity, refetchOnMount: 'always', refetchOnWindowFocus: false, refetchOnReconnect: false })
  if (id !== 'new' && (!query.isFetchedAfterMount || query.isError || !query.data)) return <Modal opened onClose={props.onClose} title={t('topologyChangesets.editDraft')}>{query.isError ? <Alert color="red"><Text>{query.error.message}</Text><Button onClick={() => query.refetch()}>{t('entryFlow.retry')}</Button></Alert> : <Loader />}</Modal>
  return <SettingsDraftBoundary><DraftEditor initial={id === 'new' ? undefined : query.data} {...props} /></SettingsDraftBoundary>
}
interface EditorProps { catalog: TopologyCatalog; catalogError: boolean; onRefreshCatalog: () => void; onClose: () => void; onSaved: () => void; onCommitted: () => void }
function DraftEditor({ initial, catalog, catalogError, onRefreshCatalog, onClose, onSaved, onCommitted }: EditorProps & { initial?: TopologyDraft }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [state, setState] = useState(() => {
    const model = initial ? modelFromDraft(initial, catalog) : { name: '', operations: [] }
    return { model, saved: initial, savedSource: changesetFingerprint(model), conflicts: [] as string[], lateEdits: false, review: null as { version: number; source: string; value: TopologyPreview } | null }
  })
  const source = changesetFingerprint(state.model)
  const dirty = source !== state.savedSource
  const committed = state.saved?.status === 'committed'
  const reviewCurrent = !!state.review && state.review.version === state.saved?.version && state.review.source === source && !dirty
  const save = useMutation({
    mutationFn: ({ model, id, version }: { model: ChangesetModel; id?: string; version: number }) => id ? api.put<TopologyDraft>(`${base}/${id}`, changesetInput(model, version)) : api.post<TopologyDraft>(base, changesetInput(model, version)),
    onSuccess: (saved, sent) => {
      qc.setQueryData(['topology-changeset', saved.id], saved)
      setState(current => { const merged = reconcileChangesetSave(current.model, sent.model, saved, catalog); const savedSource = changesetFingerprint(modelFromDraft(saved, catalog)); return { ...current, ...merged, saved, savedSource, review: null, lateEdits: changesetFingerprint(merged.model) !== savedSource } })
      toast.ok(t('topologyChangesets.saved')); onSaved()
    },
  })
  const preview = useMutation({ mutationFn: async ({ id, version, source }: { id: string; version: number; source: string }) => ({ version, source, value: await api.post<TopologyPreview>(`${base}/${id}/preview`, { version }) }), onSuccess: review => setState(current => ({ ...current, review })) })
  const commit = useMutation({ mutationFn: ({ id, version, revision }: { id: string; version: number; revision: string }) => api.post<TopologyDraft>(`${base}/${id}/commit`, { version, preview_revision: revision }), onSuccess: saved => { qc.setQueryData(['topology-changeset', saved.id], saved); setState(current => ({ ...current, saved, model: modelFromDraft(saved, catalog), savedSource: changesetFingerprint(modelFromDraft(saved, catalog)), review: null, conflicts: [], lateEdits: false })); toast.ok(t('topologyChangesets.committed')); onCommitted() }, onError: () => setState(current => ({ ...current, review: null })) })
  const busy = save.isPending || preview.isPending || commit.isPending
  const conflicts = state.conflicts.filter(key => state.model.operations.some(op => op.key === key))
  const valid = changesetValid(state.model) && state.model.operations.every(op => !op.line || !!lineTargetProtocol(op, state.model.operations, catalog)) && !catalogError && conflicts.length === 0
  useSettingsDirty(!committed && (dirty || busy))
  const close = () => { if (busy) return; if (!dirty || committed) return onClose(); modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose }) }
  const edit = (update: (model: ChangesetModel) => ChangesetModel) => setState(current => {
    const next = update(current.model)
    const operations = next.operations.map(op => { const protocol = lineTargetProtocol(op, next.operations, catalog); return op.line && protocol ? { ...op, line: { ...op.line, protocol } } : op })
    return { ...current, model: { ...next, operations }, review: null, lateEdits: false }
  })
  return <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!busy} withCloseButton={!busy} size="xl" title={t('topologyChangesets.editDraft')}><Stack>
    <Text size="sm" c="dimmed">{t('topologyChangesets.scope')}</Text>
    {catalogError && <Alert color="red"><Text>{t('topologyChangesets.catalogError')}</Text><Button variant="subtle" onClick={onRefreshCatalog}>{t('topologyChangesets.refreshCatalog')}</Button></Alert>}
    {[save.error, preview.error, commit.error].filter(Boolean).map((error, i) => <Alert key={i} color="red">{error!.message}</Alert>)}
    {state.lateEdits && <Alert color="yellow">{t('topologyChangesets.savedNewerEdits')}</Alert>}
    {conflicts.map(key => <Alert key={key} color="yellow"><Text size="sm">{t('topologyChangesets.settingsChangedWhileSaving')}</Text><Button variant="subtle" onClick={() => setState(current => ({ ...current, model: keepSavedInboundSettings(current.model, current.saved!, key), conflicts: current.conflicts.filter(value => value !== key), review: null }))}>{t('topologyChangesets.keepSavedSettings')}</Button></Alert>)}
    <Box component="fieldset" disabled={busy || committed} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack>
      <TextInput label={t('topologyChangesets.draftName')} required value={state.model.name} onChange={event => { const name = event.currentTarget.value; edit(model => ({ ...model, name })) }} />
      {state.model.operations.map((op, index) => <OperationEditor key={op.key} op={op} index={index} catalog={catalog} operations={state.model.operations} onChange={next => edit(model => ({ ...model, operations: model.operations.map(item => item.key === op.key ? next : item) }))} onRemove={() => edit(model => ({ ...model, operations: model.operations.filter(item => item.key !== op.key) }))} />)}
      {!committed && <><Group>{(['inbound', 'line', 'entry'] as const).map(kind => <Button key={kind} variant="light" disabled={state.model.operations.length >= 32} onClick={() => edit(model => ({ ...model, operations: [...model.operations, newChangesetOperation(kind)] }))}>{t(`topologyChangesets.add${kind[0].toUpperCase()}${kind.slice(1)}`)}</Button>)}</Group><Text size="xs" c="dimmed">{t('topologyChangesets.operationLimit')}</Text></>}
    </Stack></Box>
    {!state.model.operations.length && <Text c="dimmed">{t('topologyChangesets.noOperations')}</Text>}
    {reviewCurrent && state.review && <ChangesetImpact preview={state.review.value} />}
    {committed && <Alert color="teal"><Text fw={600}>{t('topologyChangesets.committed')}</Text>{state.saved?.result?.map(result => <Text key={result.key} size="sm">{operationLabel(state.saved!.operations.find(op => op.key === result.key) || { key: result.key, kind: result.kind })} · {t(kindKey(result.kind))} #{result.id}</Text>)}</Alert>}
    {dirty && state.saved && <Text size="sm" c="dimmed">{t('topologyChangesets.saveBeforePreview')}</Text>}
    <Group justify="flex-end"><Button variant="default" disabled={busy} onClick={close}>{t(committed ? 'topologyChangesets.close' : 'common.cancel')}</Button>{!committed && <><Button variant="default" loading={save.isPending} disabled={busy || !valid || !dirty && !!state.saved} onClick={() => save.mutate({ model: structuredClone(state.model), id: state.saved?.id, version: state.saved?.version || 0 })}>{t('topologyChangesets.saveDraft')}</Button><Button variant={reviewCurrent ? 'default' : 'filled'} loading={preview.isPending} disabled={busy || !valid || !state.saved || dirty} onClick={() => state.saved && preview.mutate({ id: state.saved.id, version: state.saved.version, source })}>{t('topologyChangesets.preview')}</Button>{reviewCurrent && <Button loading={commit.isPending} disabled={busy || !valid} onClick={() => state.saved && state.review && commit.mutate({ id: state.saved.id, version: state.saved.version, revision: state.review.value.revision })}>{t('topologyChangesets.commit')}</Button>}</>}</Group>
  </Stack></Modal>
}

interface OperationProps { op: EditableOperation; index: number; catalog: TopologyCatalog; operations: EditableOperation[]; onChange: (op: EditableOperation) => void; onRemove: () => void }
function OperationEditor({ op, index, catalog, operations, onChange, onRemove }: OperationProps) {
  const { t } = useTranslation()
  const resources = op.kind === 'inbound' ? catalog.inbounds.filter(item => item.editable).map(item => ({ value: String(item.id), label: `${catalog.nodes.find(node => node.id === item.node_id)?.name || `#${item.node_id}`} / ${item.tag}` })) : op.kind === 'line' ? catalog.lines.filter(item => editableChangesetLine(item.line)).map(item => ({ value: String(item.line.id), label: item.line.name })) : catalog.entries.filter(item => item.editable).map(item => ({ value: String(item.id), label: item.name }))
  if (op.id && !resources.some(item => item.value === String(op.id))) resources.push({ value: String(op.id), label: `${operationLabel(op)} (#${op.id})` })
  const referenced = operationIsReferenced(operations, op.key)
  return <Card withBorder><Stack gap="sm">
    <Group justify="space-between"><Title order={4}>{t('topologyChangesets.operation', { number: index + 1 })} · {t(kindKey(op.kind))}</Title><ActionIcon variant="subtle" color="red" aria-label={t('topologyChangesets.removeOperation')} disabled={referenced} onClick={onRemove}><IconTrash size={17} /></ActionIcon></Group>
    {referenced && <Text size="xs" c="dimmed">{t('topologyChangesets.blockedReference')}</Text>}
    <Select label={t('topologyChangesets.resource')} allowDeselect={false} value={op.editMode || op.id ? 'edit' : 'create'} disabled={referenced || !!op.inbound?.settings_configured} data={[{ value: 'create', label: t('topologyChangesets.createResource') }, { value: 'edit', label: t('topologyChangesets.editResource'), disabled: resources.length === 0 }]} onChange={value => onChange({ ...newChangesetOperation(op.kind), key: op.key, editMode: value === 'edit', ...(value === 'edit' ? { id: 0 } : {}) })} />
    {(op.editMode || !!op.id) && <Select label={t(kindKey(op.kind))} required searchable allowDeselect={false} value={op.id ? String(op.id) : null} data={resources.map(item => ({ ...item, disabled: operations.some(other => other.key !== op.key && other.kind === op.kind && other.id === Number(item.value)) }))} onChange={value => onChange(selectChangesetResource(op, Number(value), catalog))} />}
    {(!op.editMode || !!op.id) && (op.kind === 'inbound' && op.inbound ? <InboundFields value={op.inbound} existing={!!op.id} catalog={catalog} onChange={inbound => onChange({ ...op, inbound })} /> : op.kind === 'line' && op.line ? <LineFields op={op} catalog={catalog} operations={operations} onChange={onChange} /> : op.kind === 'entry' && op.entry ? <EntryFields key={`${op.id || 0}-${!!op.editMode}`} value={op.entry} catalog={catalog} operations={operations} onChange={entry => onChange({ ...op, entry })} /> : null)}
  </Stack></Card>
}
function IngressSelect({ nodeID, value, catalog, onChange }: { nodeID?: number; value?: number | null; catalog: TopologyCatalog; onChange: (value: number) => void }) {
  const { t } = useTranslation()
  return <Select label={t('inbounds.ingress')} allowDeselect={false} value={value ? String(value) : '0'} data={[{ value: '0', label: t('inbounds.ingressDirect') }, ...catalog.ingresses.filter(item => item.node_id === nodeID).map(item => ({ value: String(item.id), label: item.name }))]} onChange={value => onChange(Number(value))} />
}
function InboundFields({ value, existing, catalog, onChange }: { value: ChangesetInbound; existing: boolean; catalog: TopologyCatalog; onChange: (value: ChangesetInbound) => void }) {
  const { t } = useTranslation()
  const patch = (update: Partial<ChangesetInbound>) => onChange({ ...value, ...update })
  const frozen = existing || !!value.settings_configured
  return <Stack gap="sm"><Text size="xs" c="dimmed">{t('topologyChangesets.newInboundHint')}</Text><SimpleGrid cols={{ base: 1, sm: 2 }}>
    <Select label={t('entryFlow.server')} required searchable allowDeselect={false} disabled={existing} value={value.node_id ? String(value.node_id) : null} data={catalog.nodes.map(node => ({ value: String(node.id), label: node.name }))} onChange={id => patch({ node_id: Number(id), ingress_id: 0 })} />
    <TextInput label={t('inbounds.tag')} required value={value.tag} onChange={event => patch({ tag: event.currentTarget.value })} />
    <TextInput label={t('inbounds.listen')} value={value.listen} onChange={event => patch({ listen: event.currentTarget.value })} />
    <NumberInput label={t('inbounds.port')} required min={1} max={65535} allowDecimal={false} value={value.port || ''} onChange={port => patch({ port: Number(port) })} />
    <IngressSelect catalog={catalog} nodeID={value.node_id} value={value.ingress_id} onChange={ingress_id => patch({ ingress_id })} />
    <NumberInput label={t('topologyChangesets.sort')} allowDecimal={false} value={value.sort} onChange={sort => patch({ sort: Number(sort) })} />
  </SimpleGrid><Checkbox label={t('common.enabled')} checked={value.enabled} onChange={event => patch({ enabled: event.currentTarget.checked })} />
    {frozen ? <Text size="sm" c="dimmed">{t('topologyChangesets.settingsRetained')}</Text> : <><Title order={5}>{t('topologyChangesets.initialSettings')}</Title><SimpleGrid cols={{ base: 1, sm: 2 }}><Select label={t('inbounds.protocol')} required allowDeselect={false} value={value.protocol || 'vless'} data={['vless', 'vmess', 'trojan', 'shadowsocks'].map(protocol => ({ value: protocol, label: protocol.toUpperCase() }))} onChange={protocol => patch({ protocol: protocol || 'vless', settings: { transport: { type: 'tcp' }, ...(protocol === 'shadowsocks' ? { cipher: '2022-blake3-aes-128-gcm' } : {}), ...(value.settings?.tls ? { tls: value.settings.tls } : {}) } })} /><Select label={t('inbounds.core')} allowDeselect={false} value={value.core || ''} data={[{ value: '', label: t('inbounds.coreAuto') }, { value: 'xray', label: 'Xray' }, { value: 'sing-box', label: 'sing-box' }]} onChange={core => patch({ core: core || '' })} /></SimpleGrid><Checkbox label={t('topologyChangesets.standardTLS')} checked={value.settings?.tls?.mode === 1} onChange={event => patch({ settings: { ...value.settings, tls: event.currentTarget.checked ? { mode: 1, server_name: '' } : undefined } })} />{value.settings?.tls?.mode === 1 && <TextInput label={t('topologyChangesets.serverName')} description={t('topologyChangesets.tlsHint')} required value={value.settings.tls.server_name || ''} onChange={event => patch({ settings: { ...value.settings, tls: { mode: 1, server_name: event.currentTarget.value } } })} />}<Text size="xs" c="dimmed">{t('topologyChangesets.credentialsHint')}</Text></>}
  </Stack>
}
function targetOptions(kind: 'inbound' | 'line', catalog: TopologyCatalog, operations: EditableOperation[], draftLabel: (name: string) => string, existingLabel: (name: string) => string) {
  const existing = kind === 'inbound' ? catalog.inbounds.filter(item => item.editable).map(item => ({ value: `id:${item.id}`, label: existingLabel(`${catalog.nodes.find(node => node.id === item.node_id)?.name || `#${item.node_id}`} / ${item.tag}`) })) : catalog.lines.filter(item => editableChangesetLine(item.line)).map(item => ({ value: `id:${item.line.id}`, label: existingLabel(item.line.name) }))
  return [...existing, ...operations.filter(op => op.kind === kind && !op.id && !op.editMode).map(op => ({ value: `ref:${op.key}`, label: draftLabel(operationLabel(op)) }))]
}
function LineFields({ op, catalog, operations, onChange }: Pick<OperationProps, 'op' | 'catalog' | 'operations' | 'onChange'>) {
  const { t } = useTranslation()
  const line = op.line!
  const patch = (update: Partial<Line>) => onChange({ ...op, line: { ...line, ...update } })
  const groups = line.groups
  const options = targetOptions('inbound', catalog, operations, name => t('topologyChangesets.referenceDraft', { name }), name => t('topologyChangesets.referenceExisting', { name })).map(option => ({ ...option, disabled: !lineTargetProtocol({ ...op, inbound_ref: option.value.startsWith('ref:') ? option.value.slice(4) : undefined, line: { ...line, target_inbound_id: option.value.startsWith('id:') ? Number(option.value.slice(3)) : 0 } }, operations, catalog) }))
  return <Stack gap="sm"><Text size="sm" c="dimmed">{t('topologyChangesets.lineOnly')}</Text><TextInput label={t('lines.name')} required value={line.name} onChange={event => patch({ name: event.currentTarget.value })} />
    <Select label={t('topologyChangesets.targetInbound')} required searchable allowDeselect={false} value={op.inbound_ref ? `ref:${op.inbound_ref}` : line.target_inbound_id ? `id:${line.target_inbound_id}` : null} data={options} onChange={value => onChange({ ...op, inbound_ref: value?.startsWith('ref:') ? value.slice(4) : undefined, line: { ...line, target_inbound_id: value?.startsWith('id:') ? Number(value.slice(3)) : 0 } })} />
    <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput readOnly label={t('lines.protocol')} value={line.protocol === 'both' ? 'TCP + UDP' : line.protocol.toUpperCase()} /><Select label={t('forwardAddress.mode')} allowDeselect={false} value={line.target_address_mode || 'default'} data={[{ value: 'default', label: t('forwardAddress.default') }, { value: 'internal', label: t('nodes.internalAddr') }]} onChange={value => patch({ target_address_mode: value === 'internal' ? 'internal' : 'default' })} /></SimpleGrid>
    {groups.map((group, index) => <Card key={group.members[0]?.id || index} withBorder padding="sm"><Stack gap="sm"><Group justify="space-between"><Text fw={600}>{t(index ? 'lines.hopGroup' : 'lines.entryGroup', { number: index + 1 })}</Text><Group gap={3}><ActionIcon variant="subtle" aria-label={t('lines.moveUp')} disabled={!index} onClick={() => patch({ groups: move(groups, index, index - 1) })}><IconArrowUp size={15} /></ActionIcon><ActionIcon variant="subtle" aria-label={t('lines.moveDown')} disabled={index === groups.length - 1} onClick={() => patch({ groups: move(groups, index, index + 1) })}><IconArrowDown size={15} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('lines.removeGroup')} disabled={groups.length === 1} onClick={() => patch({ groups: groups.filter((_, i) => i !== index) })}><IconTrash size={15} /></ActionIcon></Group></Group>
      {index > 0 && <Select label={t('lines.strategy')} allowDeselect={false} value={group.strategy} data={[{ value: 'failover', label: t('lines.strategies.failover'), disabled: line.protocol !== 'tcp' && group.members.length > 1 }, { value: 'roundrobin', label: t('lines.strategies.roundrobin') }]} onChange={value => patch({ groups: groups.map((item, i) => i === index ? { ...item, strategy: value === 'roundrobin' ? 'roundrobin' : 'failover' } : item) })} />}
      {group.members.map((member, memberIndex) => {
        const update = (fields: Partial<typeof member>) => patch({ groups: groups.map((item, i) => i === index ? { ...item, members: item.members.map((old, j) => j === memberIndex ? { ...old, ...fields } : old) } : item) })
        return <Stack key={member.id} gap="xs"><SimpleGrid cols={{ base: 1, sm: 2 }}><Select label={t('entryFlow.server')} required searchable allowDeselect={false} value={member.node_id ? String(member.node_id) : null} data={catalog.nodes.map(node => ({ value: String(node.id), label: node.name, disabled: groups.some(group => group.members.some(other => other.id !== member.id && other.node_id === node.id)) }))} onChange={value => update({ node_id: Number(value), ingress_id: null })} /><NumberInput label={t('lines.listenPort')} required min={1} max={65535} allowDecimal={false} value={member.port || ''} onChange={value => update({ port: Number(value) })} /><IngressSelect nodeID={member.node_id} value={member.ingress_id} catalog={catalog} onChange={value => update({ ingress_id: value || null })} /><NumberInput label={t('lines.weight')} required min={1} max={100} allowDecimal={false} value={member.weight} onChange={value => update({ weight: Number(value) })} /></SimpleGrid>{index > 0 && <Select label={t('forwardAddress.mode')} allowDeselect={false} value={member.address_mode || 'default'} data={[{ value: 'default', label: t('forwardAddress.default') }, { value: 'internal', label: t('nodes.internalAddr') }]} onChange={value => update({ address_mode: value === 'internal' ? 'internal' : 'default' })} />}<Group justify="flex-end"><ActionIcon variant="subtle" aria-label={t('lines.moveUp')} disabled={!memberIndex} onClick={() => patch({ groups: groups.map((item, i) => i === index ? { ...item, members: move(item.members, memberIndex, memberIndex - 1) } : item) })}><IconArrowUp size={15} /></ActionIcon><ActionIcon variant="subtle" aria-label={t('lines.moveDown')} disabled={memberIndex === group.members.length - 1} onClick={() => patch({ groups: groups.map((item, i) => i === index ? { ...item, members: move(item.members, memberIndex, memberIndex + 1) } : item) })}><IconArrowDown size={15} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('lines.removeMember')} disabled={group.members.length === 1} onClick={() => patch({ groups: groups.map((item, i) => i === index ? { ...item, members: item.members.filter((_, j) => j !== memberIndex) } : item) })}><IconTrash size={15} /></ActionIcon></Group></Stack>
      })}
      <Button variant="subtle" size="xs" disabled={group.members.length >= 8} onClick={() => patch({ groups: groups.map((item, i) => i === index ? { ...item, members: [...item.members, newChangesetMember()], ...(line.protocol !== 'tcp' ? { strategy: 'roundrobin' as const } : {}) } : item) })}>{t('lines.addMember')}</Button>
    </Stack></Card>)}<Button variant="light" disabled={groups.length >= 8} onClick={() => patch({ groups: [...groups, newChangesetGroup()] })}>{t('lines.addGroup')}</Button>
  </Stack>
}
function EntryFields({ value, catalog, operations, onChange }: { value: ChangesetEntry; catalog: TopologyCatalog; operations: EditableOperation[]; onChange: (value: ChangesetEntry) => void }) {
  const { t } = useTranslation()
  const patch = (update: Partial<ChangesetEntry>) => onChange({ ...value, ...update })
  const lineTarget = !!value.line_id || !!value.line_ref
  const [mode, setMode] = useState<'inbound' | 'line'>(() => lineTarget ? 'line' : 'inbound')
  const target = mode === 'line' ? value.line_ref ? `ref:${value.line_ref}` : value.line_id ? `id:${value.line_id}` : null : value.inbound_ref ? `ref:${value.inbound_ref}` : value.inbound_id ? `id:${value.inbound_id}` : null
  const selectedLine = value.line_ref ? operations.find(op => op.key === value.line_ref)?.line : operations.find(op => op.kind === 'line' && op.id === value.line_id)?.line || catalog.lines.find(item => item.line.id === value.line_id)?.line
  const members = selectedLine?.groups[0]?.members || []
  return <Stack gap="sm"><TextInput label={t('entries.name')} required value={value.name} onChange={event => patch({ name: event.currentTarget.value })} /><SimpleGrid cols={{ base: 1, sm: 2 }}><Select label={t('topologyChangesets.entryTarget')} allowDeselect={false} value={mode} data={[{ value: 'inbound', label: t('topologyChangesets.directInbound') }, { value: 'line', label: t('topologyChangesets.forwardingLine') }]} onChange={next => { setMode(next === 'line' ? 'line' : 'inbound'); patch({ inbound_id: 0, inbound_ref: undefined, line_id: 0, line_ref: undefined, line_member_id: '', display_host: '', display_port: 0 }) }} /><Select label={t(mode === 'line' ? 'topologyChangesets.forwardingLine' : 'topologyChangesets.targetInbound')} required searchable allowDeselect={false} value={target} data={targetOptions(mode, catalog, operations, name => t('topologyChangesets.referenceDraft', { name }), name => t('topologyChangesets.referenceExisting', { name }))} onChange={selected => patch(mode === 'line' ? { line_id: selected?.startsWith('id:') ? Number(selected.slice(3)) : 0, line_ref: selected?.startsWith('ref:') ? selected.slice(4) : undefined, inbound_id: 0, inbound_ref: undefined, line_member_id: '', display_host: '', display_port: 0 } : { inbound_id: selected?.startsWith('id:') ? Number(selected.slice(3)) : 0, inbound_ref: selected?.startsWith('ref:') ? selected.slice(4) : undefined, line_id: 0, line_ref: undefined, line_member_id: '', display_host: '', display_port: 0 })} /></SimpleGrid>
    {mode === 'line' && <Select label={t('topologyChangesets.publishedMember')} required={members.length > 1} allowDeselect={false} value={value.line_member_id || ''} data={[{ value: '', label: t('topologyChangesets.automaticMember'), disabled: members.length > 1 }, ...members.map((member, index) => ({ value: member.id, label: `${catalog.nodes.find(node => node.id === member.node_id)?.name || t('topologyChangesets.draftMember', { number: index + 1 })} : ${member.port}` }))]} onChange={member => patch({ line_member_id: member || '' })} />}
    <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput label={t('entryFlow.connectHost')} disabled={mode === 'line'} value={value.display_host} onChange={event => patch({ display_host: event.currentTarget.value })} /><NumberInput label={t('entryFlow.connectPort')} disabled={mode === 'line'} min={0} max={65535} allowDecimal={false} value={value.display_port} onChange={port => patch({ display_port: Number(port) })} /><NumberInput label={t('entries.rate')} required min={0.01} decimalScale={4} value={value.rate} onChange={rate => patch({ rate: Number(rate) })} /><NumberInput label={t('topologyChangesets.sort')} allowDecimal={false} value={value.sort} onChange={sort => patch({ sort: Number(sort) })} /><TagsInput label={t('topologyChangesets.tags')} value={value.tags || []} onChange={tags => patch({ tags })} /><TextInput label={t('topologyChangesets.region')} value={value.region || ''} onChange={event => patch({ region: event.currentTarget.value })} /></SimpleGrid><Checkbox label={t('common.enabled')} checked={value.enabled} onChange={event => patch({ enabled: event.currentTarget.checked })} />
  </Stack>
}
function ChangesetImpact({ preview }: { preview: TopologyPreview }) {
  const { t } = useTranslation()
  return <Card withBorder><Stack><Title order={4}>{t('topologyChangesets.preview')}</Title><Text size="sm" c="dimmed">{t('topologyChangesets.reviewHint')}</Text>{preview.changes.map(change => <Stack key={change.key} gap="xs"><Text fw={600}>{t(kindKey(change.kind))} · {change.id ? `#${change.id}` : t('topologyChangesets.createResource')}</Text><SimpleGrid cols={{ base: 1, sm: 2 }}>{[change.before, change.after].map((value, i) => <Stack key={i} gap={3}><Text size="xs">{t(i ? 'topologyChangesets.after' : 'topologyChangesets.before')}</Text><Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 260 }}>{value ? JSON.stringify(value, null, 2) : t('topologyChangesets.noBefore')}</Code></Stack>)}</SimpleGrid>{change.forwards?.map(node => <Stack key={node.node_id} gap={3}><Text size="sm">{node.name}</Text><SimpleGrid cols={{ base: 1, sm: 2 }}>{[node.before, node.after].map((value, i) => <Code key={i} block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 220 }}>{JSON.stringify(value, null, 2)}</Code>)}</SimpleGrid></Stack>)}</Stack>)}</Stack></Card>
}
