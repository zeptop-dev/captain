import { ActionIcon, Alert, Badge, Button, Card, Checkbox, Code, Group, Loader, Menu, Select, Stack, Table, Tabs, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconKey, IconMapPin, IconChevronDown, IconGripVertical, IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useLocation, useSearchParams } from 'react-router-dom'
import { api, type Entry, type EntryCategory, type Line, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { type EntryNodeDetail, isSubscriberInbound } from '../lib/entry-flow'
import { hostPort } from '../lib/ingress'
import { toast } from '../lib/notify'
import { flag } from '../lib/regions'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { PageHeader } from '../components/PageHeader'
import { EntryEditor } from '../components/EntryEditor'
import { EntryLocationEditor } from '../components/EntryLocationEditor'
import { EntryCategoriesManager } from '../components/EntryCategoriesManager'
import { matchesEntryCategory, visibleEntrySelection } from '../lib/entry-categories'
import { LineEditor, LinesPanel } from '../components/LinesPanel'
import { LineChangesModal } from '../components/LineChangesModal'
import { LineRolloutHistory } from '../components/LineRollouts'
import { PathRoutesPanel } from '../components/PathRoutesPanel'
import type { LineHistorySelection } from '../lib/line-change-evidence'
import { LinesConnections, ServerSelector } from '../components/LinesConnections'
import { isServerSettingsSection } from '../lib/navigation'
import { useNodeAccess } from '../lib/node-access-query'
import { AccessSummary, InboundAccessModal } from '../components/NodeAccess'
import { accessMatches, type InboundAccess, type NodeAccessDirectory } from '../lib/node-access'
import { InboundsManager } from '../components/InboundsManager'

export default function EntriesPage() {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const section = params.get('section')
  // Preserve bookmarks from the first unified workspace without mounting a
  // second copy of these server-scoped editors.
  if (isServerSettingsSection(section)) {
    const serverID = Number(params.get('server'))
    if (Number.isSafeInteger(serverID) && serverID > 0) return <Navigate replace to={`/nodes/${serverID}?section=${section}`} />
    return <Stack><PageHeader title={t('nodes.proxySettings')} subtitle={t('nodes.proxySettingsMoved')} />
      <ServerSelector onServerChange={id => { if (id) setParams(previous => { const next = new URLSearchParams(previous); next.set('server', String(id)); return next }, { replace: true }) }} />
    </Stack>
  }
  return <EntriesWorkspace />
}

function EntriesWorkspace() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const admin = me?.role === 'admin'
  const canWrite = admin || me?.role === 'operator'
  const access = useNodeAccess()
  const [accessFilter, setAccessFilter] = useState<string | null>(null)
  const [editingAccess, setEditingAccess] = useState<Entry | null>(null)
  const qc = useQueryClient()
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const tab = params.get('tab') === 'lines' ? 'lines' : 'entries'
  const section = params.get('section') ?? 'forward'
  const connectionSection = section === 'wireguard' || section === 'reverse' ? section : null
  const singleForward = section === 'legacy' || params.get('mode') === 'single'
  const pathRoutes = params.get('mode') === 'paths'
  const nodeFilter = params.get('node') ?? ''
  const serverID = Number(params.get('server')) || undefined
  const q = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries') })
  const categories = useQuery({ queryKey: ['entry-categories'], queryFn: () => api.get<EntryCategory[]>('/api/admin/entry-categories') })
  const tags = useQuery({ queryKey: ['entry-tags'], queryFn: () => api.get<string[]>('/api/admin/entries/tags') })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const detailQueries = useQueries({ queries: (nodes.data ?? []).map(n => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<EntryNodeDetail>(`/api/admin/nodes/${n.id}`) })) })
  const details = detailQueries.flatMap(query => query.data ? [query.data] : [])
  const inbounds = details.flatMap(d => d.inbounds.filter(isSubscriberInbound).map(ib => ({ ib, node: d.node, ingress: d.ingresses?.find(g => g.id === ib.IngressID) })))
  const lines = useQuery({ queryKey: ['lines'], queryFn: () => api.get<Line[]>('/api/admin/lines'), enabled: admin, refetchInterval: admin ? 10_000 : false })
  const sourcesLoading = nodes.isLoading || detailQueries.some(query => query.isLoading)
  const sourcesError = nodes.isError || detailQueries.some(query => query.isError)
  const reloadSources = () => { nodes.refetch(); detailQueries.forEach(query => query.refetch()); if (admin) lines.refetch() }
  const [editingLocation, setEditingLocation] = useState<Entry | null>(null)
  const [editing, setEditing] = useState<Entry | 'new' | null>(null)
  const [initialLineID, setInitialLineID] = useState<number | undefined>()
  const requestedInboundID = params.get('create') === 'node' ? Number(params.get('inbound')) : 0
  const createRequest = Number.isSafeInteger(requestedInboundID) && requestedInboundID > 0 ? `${location.key}:${requestedInboundID}` : null
  const requestedInbound = inbounds.find(choice => choice.ib.ID === requestedInboundID && (!nodeFilter || String(choice.ib.NodeID) === nodeFilter))
  const handledCreateRequest = useRef<string | null>(null)
  useEffect(() => {
    if (!createRequest || handledCreateRequest.current === createRequest || !canWrite || !requestedInbound || !q.data || editing !== null) return
    // Wait for the selected inbound before the form takes its initial snapshot.
    // A later refetch must never reopen or replace an existing draft.
    const frame = requestAnimationFrame(() => {
      handledCreateRequest.current = createRequest
      setInitialLineID(undefined)
      setEditing('new')
    })
    return () => cancelAnimationFrame(frame)
  }, [createRequest, canWrite, requestedInbound, q.data, editing])
  useEffect(() => {
    if (!createRequest || editing !== null || handledCreateRequest.current !== createRequest) return
    // The editor boundary has unmounted, so clearing the request cannot trigger
    // a second unsaved-changes prompt after saving or confirming discard.
    const frame = requestAnimationFrame(() => setParams(previous => {
      const next = new URLSearchParams(previous)
      next.delete('create'); next.delete('inbound')
      return next
    }, { replace: true }))
    return () => cancelAnimationFrame(frame)
  }, [createRequest, editing, setParams])
  const [editingLine, setEditingLine] = useState<Line | 'new' | null>(null)
  const [rollouts, setRollouts] = useState<{ id?: string; lineID?: number } | null>(null)
  const [lineHistory, setLineHistory] = useState<LineHistorySelection | null>(null)
  const [connectionRequest, setConnectionRequest] = useState(0)
  const connectionSerial = useRef(0)
  const [filter, setFilter] = useState<string | null>(null)
  const [categoryFilter, setCategoryFilter] = useState<string | null>(null)
  // A deleted category must not leave a phantom filter that hides every row.
  const activeCategoryFilter = categoryFilter === '0' || categories.data?.some(category => String(category.ID) === categoryFilter) ? categoryFilter : null
  const [selected, setSelected] = useState<number[]>([])
  const [assignCategory, setAssignCategory] = useState<string | null>(null)
  const [managingCategories, setManagingCategories] = useState(false)
  const categoryOptions = [{ value: '0', label: t('entryCategories.unclassified') }, ...(categories.data ?? []).map(category => ({ value: String(category.ID), label: category.Name }))]
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['entry-tags'] }); qc.invalidateQueries({ queryKey: ['node-access'] }) }
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/entries/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const reorder = useMutation({ mutationFn: (ids: number[]) => api.put('/api/admin/entries/order', { IDs: ids }), onSuccess: invalidate, onError: (error: Error) => { toast.err(error); invalidate() } })
  const assign = useMutation({ mutationFn: (IDs: number[]) => api.put('/api/admin/entries/category', { IDs, CategoryID: Number(assignCategory) }), onSuccess: () => { setSelected([]); setAssignCategory(null); invalidate(); toast.ok(t('common.saved')) }, onError: toast.err })
  const label = (id: number) => { const choice = inbounds.find(c => c.ib.ID === id); return choice ? `${choice.node.name} / ${choice.ib.Tag} (${choice.ib.Protocol}:${choice.ib.Port})` : `#${id}` }
  const managedType = (entry: Entry): 'wireguard' | 'reverse' | undefined => {
    if (entry.ManagedConnection) return entry.ManagedConnection.type
    const settings = inbounds.find(c => c.ib.ID === entry.InboundID)?.ib.Settings
    return settings?.wireguard_slot ? 'wireguard' : settings?.reverse ? 'reverse' : undefined
  }
  const editEntry = (entry: Entry) => {
    const managed = managedType(entry)
    if (managed) { setConnectionRequest(0); updateParams({ tab: 'lines', section: managed, server: entry.ManagedConnection?.exit_node_id ? String(entry.ManagedConnection.exit_node_id) : null }); return }
    setInitialLineID(undefined); setEditing(entry)
  }
  const updateParams = (values: Record<string, string | null>) => setParams(previous => { const next = new URLSearchParams(previous); for (const [key, value] of Object.entries(values)) { if (value === null) next.delete(key); else next.set(key, value) } return next })
  const createNode = (line?: Line) => { setInitialLineID(line?.id); setEditing('new') }
  const createConnection = (kind: 'wireguard' | 'reverse') => { updateParams({ tab: 'lines', section: kind, line: null }); connectionSerial.current += 1; setConnectionRequest(connectionSerial.current) }
  // A refetch never replaces an open editor's local draft.
  const [dragged, setDragged] = useState<{ base: Entry[] | undefined; rows: Entry[] } | null>(null)
  const rows = dragged && dragged.base === q.data ? dragged.rows : q.data ?? []
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  const draggable = canWrite && !filter && !nodeFilter && !activeCategoryFilter && !accessFilter && !reorder.isPending && !q.isError
  const onDragEnd = (event: DragEndEvent) => {
    if (!draggable || !event.over || event.active.id === event.over.id) return
    const from = rows.findIndex(row => row.ID === event.active.id), to = rows.findIndex(row => row.ID === event.over!.id)
    if (from < 0 || to < 0) return
    const next = arrayMove(rows, from, to)
    setDragged({ base: q.data, rows: next }); reorder.mutate(next.map(row => row.ID))
  }
  const visible = rows.filter(row => accessMatches(access.isError ? undefined : access.data?.inbounds.find(ib => ib.inbound_id === row.InboundID), admin ? accessFilter : null) && matchesEntryCategory(row, activeCategoryFilter) && (!filter || row.Tags?.includes(filter)) && (!nodeFilter || inbounds.some(c => c.ib.ID === row.InboundID && String(c.ib.NodeID) === nodeFilter)))
  const selectedVisible = visibleEntrySelection(selected, visible)
  const selectedSet = new Set(selectedVisible)
  const assignable = assignCategory !== null && categoryOptions.some(option => option.value === assignCategory)
  return <>
    <PageHeader title={t('entryFlow.title')} subtitle={t('entryFlow.subtitle')} actions={<Group gap="xs">
      {admin && <Button component={Link} to="/settings/category/monitoring#service-status" variant="default">{t('serviceStatus.title')}</Button>}
      {canWrite && <Button leftSection={<IconPlus size={16} />} onClick={() => createNode()} disabled={q.isLoading || q.isError || sourcesLoading}>{t('entryFlow.createNode')}</Button>}
      {admin && <Menu position="bottom-end"><Menu.Target><Button variant="light" rightSection={<IconChevronDown size={16} />}>{t('lines.create')}</Button></Menu.Target><Menu.Dropdown>
        <Menu.Item onClick={() => { updateParams({ tab: 'lines', section: 'forward', mode: null }); setEditingLine('new') }}>{t('lines.normal')}</Menu.Item>
        <Menu.Item onClick={() => createConnection('wireguard')}>{t('lines.sections.wireguard')}</Menu.Item>
        <Menu.Item onClick={() => createConnection('reverse')}>{t('lines.sections.reverse')}</Menu.Item>
      </Menu.Dropdown></Menu>}
    </Group>} />
    {tab === 'entries' ? <div>
        {section === 'inbounds' ? <Stack>
          <Group justify="space-between"><Text fw={600}>{t('entryFlow.inbounds')}</Text><Button variant="subtle" onClick={() => updateParams({ section: null, inbound: null })}>{t('entryFlow.backToNodes')}</Button></Group>
          <InboundsManager serverID={serverID} onServerChange={id => updateParams({ server: id ? String(id) : null, inbound: null })} canWrite={canWrite} editInboundID={params.get('inbound') === 'new' ? 'new' : Number(params.get('inbound')) || undefined} onEditorClose={() => updateParams({ inbound: null })} />
        </Stack> : <>
          <Group mb="md" align="flex-end">
            <Select label={t('entryFlow.targetServer')} placeholder={t('entryFlow.allServers')} clearable searchable data={(nodes.data ?? []).map(n => ({ value: String(n.id), label: n.name }))} value={nodeFilter || null} onChange={value => { setSelected([]); updateParams({ node: value, inbound: null }) }} />
            {(tags.data?.length ?? 0) > 0 && <Select label={t('entries.tags')} clearable placeholder={t('entries.filterTag')} data={tags.data ?? []} value={filter} onChange={value => { setSelected([]); setFilter(value) }} />}
            <Select label={t('entryCategories.label')} placeholder={t('entryCategories.all')} clearable searchable data={categoryOptions} value={activeCategoryFilter} onChange={value => { setSelected([]); setCategoryFilter(value) }} disabled={categories.isLoading || categories.isError} />
            {admin && <Select label={t('nodeAccess.title')} clearable searchable disabled={!access.data || access.isError} value={accessFilter} onChange={value => { setAccessFilter(value); setSelected([]) }} data={[...(['all', 'groups', 'none'] as const).map(value => ({ value, label: t(`nodeAccess.${value}`) })), ...(access.data?.groups ?? []).map(g => ({ value: `group:${g.id}`, label: t('nodeAccess.groupSummary', { groups: g.name }) }))]} />}
            {canWrite && <Button variant="light" onClick={() => { setSelected([]); setManagingCategories(true) }}>{t('entryCategories.manage')}</Button>}
            <Button variant="subtle" onClick={() => updateParams({ section: 'inbounds', server: nodeFilter || null, inbound: null })}>{t('entryFlow.manageInbounds')}</Button>
            {nodeFilter && <Button component={Link} variant="subtle" to={`/nodes/${nodeFilter}`}>{t('entryFlow.serverDetails')}</Button>}
          </Group>
          {q.isLoading && <Loader size="sm" mb="md" />}
          {createRequest && sourcesLoading && <Loader size="sm" mb="md" />}
          {createRequest && !sourcesLoading && !requestedInbound && <Alert color="red" mb="md">{t('entryFlow.targetInbound')} #{requestedInboundID}: {t('entryFlow.loadError')}</Alert>}
          {q.isError && <Alert color="red" mb="md"><Text size="sm">{t('entryFlow.loadError')}</Text><Button variant="subtle" size="xs" onClick={() => q.refetch()}>{t('entryFlow.retry')}</Button></Alert>}
          {sourcesError && <Alert color="yellow" mb="md"><Text size="sm">{t('entryFlow.partialLoad')}</Text><Button variant="subtle" size="xs" onClick={reloadSources}>{t('entryFlow.retry')}</Button></Alert>}
          {admin && access.isError && <Alert color="yellow" mb="md">{t('nodeAccess.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => { void access.refetch() }}>{t('common.refresh')}</Button>{accessFilter && <Button size="compact-xs" variant="subtle" onClick={() => setAccessFilter(null)}>{t('nodeAccess.clearFilter')}</Button>}</Alert>}
          {categories.isError && <Alert color="yellow" mb="md"><Text size="sm">{t('entryCategories.loadError')}</Text><Button variant="subtle" size="xs" onClick={() => categories.refetch()}>{t('entryFlow.retry')}</Button></Alert>}
          {canWrite && selectedVisible.length > 0 && <Group mb="md" align="flex-end">
            <Text size="sm" pb={6}>{t('entryCategories.selected', { count: selectedVisible.length })}</Text>
            <Select label={t('entryCategories.assign')} placeholder={t('entryCategories.pick')} data={categoryOptions} searchable value={assignCategory} onChange={setAssignCategory} disabled={assign.isPending || categories.isLoading || categories.isError} />
            <Button onClick={() => assign.mutate(selectedVisible)} loading={assign.isPending} disabled={!assignable || q.isError || categories.isLoading || categories.isError || selectedVisible.length > 10000}>{t('entryCategories.apply')}</Button>
            <Button variant="subtle" disabled={assign.isPending} onClick={() => setSelected([])}>{t('entryCategories.clearSelection')}</Button>
          </Group>}
          {selectedVisible.length > 10000 && <Alert color="yellow" mb="md">{t('entryCategories.batchLimit')}</Alert>}
          <Card p={0}>
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}><SortableContext items={visible.map(row => row.ID)} strategy={verticalListSortingStrategy}>
              <Table.ScrollContainer minWidth={940}><Table verticalSpacing="sm">
                <Table.Thead><Table.Tr><Table.Th w={32} />{canWrite && <Table.Th w={36}><Checkbox aria-label={t('entryCategories.selectVisible')} checked={visible.length > 0 && selectedVisible.length === visible.length} indeterminate={selectedVisible.length > 0 && selectedVisible.length < visible.length} disabled={q.isLoading || q.isError || assign.isPending || !visible.length} onChange={event => setSelected(event.currentTarget.checked ? visible.map(entry => entry.ID) : [])} /></Table.Th>}<Table.Th>{t('entries.name')}</Table.Th><Table.Th>{t('entryCategories.label')}</Table.Th><Table.Th>{t('entryFlow.path')}</Table.Th><Table.Th>{t('entryFlow.publishedAddress')}</Table.Th><Table.Th>{t('entries.rate')}</Table.Th><Table.Th>{t('entries.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
                <Table.Tbody>{visible.map(entry => <EntryRow key={entry.ID} entry={entry} accessRow={access.isError ? undefined : access.data?.inbounds.find(row => row.inbound_id === entry.InboundID)} accessDirectory={access.data} onAccess={admin ? () => setEditingAccess(entry) : undefined} categoryName={categories.data?.find(category => category.ID === entry.CategoryID)?.Name} categoryKnown={!categories.isLoading && !categories.isError} selectable={canWrite} selected={selectedSet.has(entry.ID)} onSelect={checked => setSelected(checked ? [...selectedVisible, entry.ID] : selectedVisible.filter(id => id !== entry.ID))} selectionBusy={assign.isPending || q.isError} inboundLabel={label(entry.InboundID)} inboundKnown={inbounds.some(c => c.ib.ID === entry.InboundID)} managed={managedType(entry)} lineName={lines.data?.find(line => line.id === entry.LineID)?.name} dialerName={q.data?.find(item => item.ID === entry.ClientDialerEntryID)?.Name} draggable={draggable} canWrite={canWrite && (!managedType(entry) || admin)} busy={del.isPending} canEditLocation={canWrite} onLocation={() => setEditingLocation(entry)} onEdit={() => editEntry(entry)} onDelete={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('entryFlow.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(entry.ID) })} />)}
                  {!q.isLoading && !q.isError && visible.length === 0 && <Table.Tr><Table.Td colSpan={canWrite ? 9 : 8}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
                </Table.Tbody>
              </Table></Table.ScrollContainer>
            </SortableContext></DndContext>
          </Card>
          {draggable && rows.length > 1 && <Text size="xs" c="dimmed" mt="xs">{t('entries.dragHint')}</Text>}
        </>}
      </div> : <div>
        <Stack>
          <Select label={t('lines.section')} allowDeselect={false} value={connectionSection ?? 'forward'} data={['forward', 'wireguard', 'reverse'].map(value => ({ value, label: t(`lines.sections.${value}`) }))} onChange={value => { setConnectionRequest(0); updateParams({ section: value, mode: null, inbound: null }) }} maw={420} />
          {connectionSection ? <LinesConnections section={connectionSection} serverID={serverID} onServerChange={id => updateParams({ server: id ? String(id) : null })} editRequest={connectionRequest} onEditRequestHandled={() => setConnectionRequest(0)} canWrite={canWrite} /> : <Tabs value={singleForward ? 'single' : pathRoutes ? 'paths' : 'grouped'} onChange={value => updateParams({ section: 'forward', mode: value === 'grouped' ? null : value })} keepMounted={false}>
            <Tabs.List mb="md"><Tabs.Tab value="grouped">{t('lines.forwardGroups')}</Tabs.Tab><Tabs.Tab value="paths">{t('pathRoutes.routes')}</Tabs.Tab><Tabs.Tab value="single">{t('lines.sections.legacy')}</Tabs.Tab></Tabs.List>
            <Tabs.Panel value="single"><LinesConnections section="legacy" serverID={serverID} onServerChange={id => updateParams({ server: id ? String(id) : null })} canWrite={canWrite} /></Tabs.Panel>
            <Tabs.Panel value="paths">{admin ? <PathRoutesPanel nodes={nodes.data ?? []} details={details} inbounds={inbounds} lines={lines.data ?? []} loading={sourcesLoading || lines.isLoading} error={sourcesError || lines.isError} onRetry={reloadSources} onHistory={setLineHistory} onCreateNode={lineID => { setInitialLineID(lineID); setEditing('new') }} /> : <Alert color="blue">{t('lines.adminOnly')}</Alert>}</Tabs.Panel>
            <Tabs.Panel value="grouped">{admin ? <LinesPanel lines={(lines.data ?? []).filter(line => !line.path_route)} nodes={nodes.data ?? []} inbounds={inbounds} loading={lines.isLoading} error={lines.isError} observedAt={Math.max(lines.dataUpdatedAt, lines.errorUpdatedAt)} focusID={Number(params.get('line')) || undefined} onRetry={() => lines.refetch()} onEdit={setEditingLine} onCreateNode={createNode} onHistory={setLineHistory} onRollouts={setRollouts} /> : <Alert color="blue">{t('lines.adminOnly')}</Alert>}</Tabs.Panel>
          </Tabs>}
        </Stack>
      </div>}
    {editingAccess && <SettingsDraftBoundary><InboundAccessModal inboundID={editingAccess.InboundID} entryID={editingAccess.ID} onClose={() => setEditingAccess(null)} /></SettingsDraftBoundary>}
    {editingLocation && <SettingsDraftBoundary><EntryLocationEditor key={`entry-location-${editingLocation.ID}`} entry={editingLocation} onClose={() => setEditingLocation(null)} /></SettingsDraftBoundary>}
    {editing !== null && <SettingsDraftBoundary><EntryEditor key={editing === 'new' ? `new-${initialLineID ?? ''}` : editing.ID} entry={editing === 'new' ? undefined : editing} nodes={nodes.data ?? []} inbounds={inbounds} entries={q.data ?? []} lines={lines.data ?? []} tags={tags.data ?? []} categories={categories.data ?? []} initialInboundID={Number(params.get('inbound')) || undefined} initialLineID={initialLineID} initialServerID={Number(nodeFilter) || serverID} canManageLines={admin} loading={sourcesLoading || categories.isLoading || categories.isError || admin && lines.isLoading} loadError={sourcesError || categories.isError || admin && lines.isError} onReload={() => { reloadSources(); categories.refetch() }} onClose={() => setEditing(null)} /></SettingsDraftBoundary>}
    {managingCategories && <SettingsDraftBoundary><EntryCategoriesManager onClose={() => setManagingCategories(false)} /></SettingsDraftBoundary>}
    {admin && rollouts && <LineRolloutHistory key={`${rollouts.id ?? 'all'}-${rollouts.lineID ?? 'all'}`} initialID={rollouts.id} lineID={rollouts.lineID} onClose={() => setRollouts(null)} />}
    {admin && lineHistory && <LineChangesModal key={`line-history-${lineHistory.lineID ?? 'all'}-${lineHistory.changeID ?? 'latest'}`} selection={lineHistory} onClose={() => setLineHistory(null)} />}
    {editingLine !== null && <SettingsDraftBoundary><LineEditor key={editingLine === 'new' ? 'line-new' : `line-${editingLine.id}`} line={editingLine === 'new' ? undefined : editingLine} nodes={nodes.data ?? []} details={details} inbounds={inbounds} loading={sourcesLoading} loadError={sourcesError} onReload={reloadSources} onClose={() => setEditingLine(null)} onSaved={setLineHistory} onPlanned={id => setRollouts({ id })} /></SettingsDraftBoundary>}
  </>
}

function EntryRow({ entry, accessRow, accessDirectory, onAccess, categoryName, categoryKnown, selectable, selected, onSelect, selectionBusy, inboundLabel, inboundKnown, managed, lineName, dialerName, draggable, canWrite, busy, canEditLocation, onLocation, onEdit, onDelete }: {
  entry: Entry; accessRow?: InboundAccess; accessDirectory?: NodeAccessDirectory; onAccess?: () => void; categoryName?: string; categoryKnown: boolean; selectable: boolean; selected: boolean; onSelect: (checked: boolean) => void; selectionBusy: boolean; inboundLabel: string; inboundKnown: boolean; managed?: 'wireguard' | 'reverse'; lineName?: string; dialerName?: string; draggable: boolean; canWrite: boolean; busy: boolean; canEditLocation: boolean; onLocation: () => void; onEdit: () => void; onDelete: () => void
}) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: entry.ID, disabled: !draggable })
  return <Table.Tr ref={setNodeRef} style={{ transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.6 : 1, position: 'relative', zIndex: isDragging ? 1 : undefined }}>
    <Table.Td><ActionIcon variant="subtle" color="gray" size="sm" disabled={!draggable} aria-label={t('entryFlow.reorder')} style={{ cursor: draggable ? 'grab' : 'default', touchAction: 'none' }} {...attributes} {...listeners}><IconGripVertical size={14} /></ActionIcon></Table.Td>
    {selectable && <Table.Td><Checkbox aria-label={t('entryCategories.selectNode', { name: entry.Name })} checked={selected} onChange={event => onSelect(event.currentTarget.checked)} disabled={selectionBusy} /></Table.Td>}
    <Table.Td><Stack gap={4}><Text fw={600}>{entry.Region ? `${flag(entry.Region)} ` : ''}{entry.Name}</Text><>{onAccess && <AccessSummary row={accessRow} directory={accessDirectory} />}</><Group gap={4}>{entry.Tags?.map(tag => <Badge key={tag} size="xs" variant="light" color="grape">{tag}</Badge>)}</Group></Stack></Table.Td>
    <Table.Td><Text size="sm" c={categoryName ? undefined : 'dimmed'}>{categoryName ?? (entry.CategoryID ? `#${entry.CategoryID}` : categoryKnown ? t('entryCategories.unclassified') : '—')}</Text></Table.Td>
    <Table.Td><Stack gap={4}><Badge size="xs" variant="light" color={managed || entry.LineID ? 'indigo' : entry.ClientDialerEntryID ? 'grape' : 'gray'}>{managed ? t(`lines.sections.${managed}`) : entry.ClientDialerEntryID ? t('entryFlow.clientChain') : entry.LineID ? t('entryFlow.viaLine') : inboundKnown ? t('entryFlow.direct') : t('entryFlow.pathUnknown')}</Badge><Text size="xs">{entry.ClientDialerEntryID ? `${dialerName ?? `#${entry.ClientDialerEntryID}`} → ` : entry.LineID ? `${lineName ?? `#${entry.LineID}`} → ` : ''}{inboundLabel}</Text></Stack></Table.Td>
    <Table.Td><Code>{hostPort(entry.DisplayHost, entry.DisplayPort)}</Code></Table.Td><Table.Td>×{entry.Rate}</Table.Td>
    <Table.Td><Badge color={entry.Enabled ? 'teal' : 'gray'}>{t(entry.Enabled ? 'common.enabled' : 'common.disabled')}</Badge></Table.Td>
    <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">{onAccess && <ActionIcon variant="subtle" aria-label={t('nodeAccess.manage')} title={t('nodeAccess.manage')} disabled={busy} onClick={onAccess}><IconKey size={16} /></ActionIcon>}{canEditLocation && <ActionIcon variant="subtle" aria-label={t('mapLocation.title')} title={t('mapLocation.title')} disabled={busy} onClick={onLocation}><IconMapPin size={16} /></ActionIcon>}{canWrite && <><ActionIcon variant="subtle" aria-label={t('common.edit')} disabled={busy} onClick={onEdit}><IconPencil size={16} /></ActionIcon>{!managed && <ActionIcon variant="subtle" color="red" aria-label={t('common.delete')} disabled={busy} onClick={onDelete}><IconTrash size={16} /></ActionIcon>}</>}</Group></Table.Td>
  </Table.Tr>
}
