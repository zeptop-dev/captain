import { LineRolloutHistory, LineRolloutPlanner } from './LineRollouts'
import { ActionIcon, Alert, Badge, Box, Button, Card, Checkbox, Code, Group, Loader, Modal, NumberInput, Select, SimpleGrid, Stack, Table, Tabs, Text, TextInput, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconPencil, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ForwardAddressMode, type Line, type Node } from '../lib/api'
import { proxyServer, type EntryNodeDetail, type InboundChoice } from '../lib/entry-flow'
import { hostPort } from '../lib/ingress'
import type { LineHistorySelection } from '../lib/line-change-evidence'
import { linearSourceLine, movePathItem, pathInput, pathSourceLineOptions, pathStepsComplete, routeInput, routePortsComplete, selectPathSourceLine, validPathPort, type ForwardPath, type PathBranch, type PathRoute, type PathRoutePreview, type PathStep, type PathUse } from '../lib/path-routes'
import { toast } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { ForwardAddressModeSelect } from './ForwardAddressModeSelect'
import { PathHealthFields, PathHealthModal } from './PathHealth'
import { pathHealthInput, validPathHealthPolicy } from '../lib/path-health'

interface Resources { nodes: Node[]; details: EntryNodeDetail[]; inbounds: InboundChoice[]; lines: Line[] }
interface Props extends Resources {
  loading?: boolean; error?: boolean; onRetry?: () => void
  onHistory?: (selection: LineHistorySelection) => void
  onCreateNode?: (lineID: number) => void
}
const key = () => crypto.randomUUID()
const revisionQuery = (id: number, revision: number) => ({ queryKey: ['forward-path-revision', id, revision], queryFn: () => api.get<ForwardPath>(`/api/admin/forward-paths/${id}/revisions/${revision}`), enabled: id > 0 && revision > 0 })

export function PathRoutesPanel({ nodes, details, inbounds, lines, loading = false, error = false, onRetry, onHistory, onCreateNode }: Props) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const paths = useQuery({ queryKey: ['forward-paths'], queryFn: () => api.get<ForwardPath[]>('/api/admin/forward-paths') })
  const routes = useQuery({ queryKey: ['path-routes'], queryFn: () => api.get<PathRoute[]>('/api/admin/path-routes') })
  const [pathEditor, setPathEditor] = useState<ForwardPath | 'new' | null>(null)
  const [routeEditor, setRouteEditor] = useState<PathRoute | 'new' | null>(null)
  const [release, setRelease] = useState<{route: PathRoute; remove: boolean} | null>(null)
  const [releaseHistory, setReleaseHistory] = useState<{id?: string; routeID?: number} | null>(null)
  const [uses, setUses] = useState<ForwardPath | null>(null)
  const [healthView, setHealthView] = useState<PathRoute | null>(null)
  const refresh = () => { qc.invalidateQueries({ queryKey: ['forward-paths'] }); qc.invalidateQueries({ queryKey: ['path-routes'] }); qc.invalidateQueries({ queryKey: ['lines'] }); qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['line-changes'] }); qc.invalidateQueries({ queryKey: ['node'] }) }
  const del = useMutation({
    mutationFn: ({ kind, id, version }: { kind: 'path' | 'route'; id: number; version: number; name: string }) => api.del<{ change_id?: string }>(`/api/admin/${kind === 'path' ? 'forward-paths' : 'path-routes'}/${id}?version=${version}`),
    onSuccess: (response, item) => { refresh(); toast.ok(t('common.deleted')); if (item.kind === 'route') onHistory?.({ lineID: item.id, lineName: item.name, changeID: response?.change_id }) },
  })
  const remove = (kind: 'path' | 'route', item: ForwardPath | PathRoute) => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t(kind === 'path' ? 'pathRoutes.deletePathHint' : 'pathRoutes.deleteRouteHint', { name: item.name })}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate({ kind, id: item.id, version: item.version, name: item.name }) })
  const resources = { nodes, details, inbounds, lines }
  return <Stack>
    <Alert color="blue">{t('pathRoutes.scope')}</Alert>
    {(error || paths.isError || routes.isError) && <Alert color="red"><Text>{paths.error?.message || routes.error?.message || t('lines.loadError')}</Text><Button variant="subtle" onClick={() => { paths.refetch(); routes.refetch(); onRetry?.() }}>{t('entryFlow.retry')}</Button></Alert>}
    {del.isError && <Alert color="red">{del.error.message}</Alert>}
    <Tabs defaultValue="routes" keepMounted={false}>
      <Tabs.List><Tabs.Tab value="routes">{t('pathRoutes.routes')}</Tabs.Tab><Tabs.Tab value="paths">{t('pathRoutes.paths')}</Tabs.Tab></Tabs.List>
      <Tabs.Panel value="routes" pt="md"><Stack>
        <Group justify="flex-end"><Button variant="default" onClick={() => setReleaseHistory({})}>{t('lineRollouts.history')}</Button><Button onClick={() => setRouteEditor('new')} disabled={loading || error || paths.isPending || paths.isError || !paths.data?.length}>{t('pathRoutes.createRoute')}</Button></Group>
        {routes.isPending && <Loader size="sm" />}
        {routes.data?.length === 0 && <Text c="dimmed">{t('pathRoutes.noRoutes')}</Text>}
        {routes.data?.map(route => <Card key={route.id} withBorder><Stack gap="sm">
          <Group justify="space-between"><Title order={4}>{route.name}</Title><Badge variant="light">TCP · {t(`lines.strategies.${route.strategy}`)}</Badge></Group>
          <Text size="sm">{nodes.find(node => node.id === route.entry.node_id)?.name || `#${route.entry.node_id}`} : {route.entry.port} → {inbounds.find(choice => choice.ib.ID === route.target_inbound_id)?.ib.Tag || `#${route.target_inbound_id}`}</Text>
          {route.branches.map((branch, index) => <Text key={branch.id || index} size="sm">{t('pathRoutes.branch', { number: index + 1 })}: {paths.data?.find(path => path.id === branch.path_id)?.name || `#${branch.path_id}`} · {t('pathRoutes.revisionValue', { version: branch.path_revision })} · {branch.ports.join(' → ')}</Text>)}
          <Group><Button size="xs" variant="subtle" onClick={() => setHealthView(route)}>{t('pathHealth.title')}</Button><Text size="xs" c="dimmed">{t(!route.health?.enabled ? 'pathHealth.states.disabled' : route.health.auto_failover ? 'pathHealth.auto' : 'pathHealth.observe')}</Text></Group>
          <Group justify="flex-end"><Button size="xs" variant="default" onClick={() => setReleaseHistory({routeID: route.id})}>{t('lineRollouts.history')}</Button><Button size="xs" variant="light" color="red" onClick={() => setRelease({route,remove:true})}>{t('lineRollouts.remove')}</Button>{onCreateNode && <Button variant="light" size="xs" onClick={() => onCreateNode(route.id)}>{t('entryFlow.createNode')}</Button>}{onHistory && <Button size="xs" variant="default" onClick={() => onHistory({ lineID: route.id, lineName: route.name })}>{t('lineChanges.application')}</Button>}<Button size="xs" variant="default" leftSection={<IconPencil size={15} />} disabled={loading || error || paths.isError || paths.isPending} onClick={() => setRouteEditor(route)}>{t('common.edit')}</Button><ActionIcon color="red" variant="subtle" aria-label={t('common.delete')} disabled={del.isPending} onClick={() => remove('route', route)}><IconTrash size={16} /></ActionIcon></Group>
        </Stack></Card>)}
      </Stack></Tabs.Panel>
      <Tabs.Panel value="paths" pt="md"><Stack>
        <Text size="sm" c="dimmed">{t('pathRoutes.templateHint')}</Text>
        <Group justify="flex-end"><Button onClick={() => setPathEditor('new')} disabled={loading || error || paths.isError || paths.isPending}>{t('pathRoutes.createPath')}</Button></Group>
        {paths.isPending && <Loader size="sm" />}
        {paths.data?.length === 0 && <Text c="dimmed">{t('pathRoutes.noPaths')}</Text>}
        {paths.data?.map(path => <Card withBorder key={path.id}><Stack gap="sm">
          <Group justify="space-between"><Title order={4}>{path.name}</Title><Badge variant="outline">{t('pathRoutes.revisionValue', { version: path.version })}</Badge></Group>
          <Text size="sm">{path.hops.map(hop => nodes.find(node => node.id === hop.node_id)?.name || `#${hop.node_id}`).join(' → ')}</Text>
          <Group justify="flex-end"><Button variant="default" size="xs" onClick={() => setUses(path)}>{t('pathRoutes.uses')}</Button><Button variant="default" size="xs" leftSection={<IconPencil size={15} />} onClick={() => setPathEditor(path)} disabled={loading || error}>{t('common.edit')}</Button><ActionIcon color="red" variant="subtle" aria-label={t('common.delete')} disabled={del.isPending} onClick={() => remove('path', path)}><IconTrash size={16} /></ActionIcon></Group>
        </Stack></Card>)}
      </Stack></Tabs.Panel>
    </Tabs>
    {pathEditor && <SettingsDraftBoundary><PathEditor path={pathEditor === 'new' ? undefined : pathEditor} paths={paths.data || []} {...resources} onClose={() => setPathEditor(null)} onSaved={refresh} /></SettingsDraftBoundary>}
    {routeEditor && <SettingsDraftBoundary><RouteEditor route={routeEditor === 'new' ? undefined : routeEditor} paths={paths.data || []} {...resources} onClose={() => setRouteEditor(null)} onStage={draft => { setRouteEditor(null); setRelease({route: draft,remove:false}) }} onSaved={saved => { refresh(); onHistory?.({ lineID: saved.id, lineName: saved.name, changeID: saved.change_id }) }} /></SettingsDraftBoundary>}
    {release && <SettingsDraftBoundary><LineRolloutPlanner resource="path-rollouts" draft={release.route} remove={release.remove} onClose={() => setRelease(null)} onCreated={id => {setRelease(null);refresh();setReleaseHistory({id})}} /></SettingsDraftBoundary>}
    {releaseHistory && <LineRolloutHistory resource="path-rollouts" initialID={releaseHistory.id} lineID={releaseHistory.routeID} onClose={() => setReleaseHistory(null)} />}
    {uses && <PathUses path={uses} onClose={() => setUses(null)} />}
    {healthView && <PathHealthModal key={healthView.id} routeID={healthView.id} name={healthView.name} onClose={() => setHealthView(null)} />}
  </Stack>
}

function Reorder({ index, length, onMove, onRemove, disabled }: { index: number; length: number; onMove: (to: number) => void; onRemove: () => void; disabled?: boolean }) {
  const { t } = useTranslation()
  return <Group gap={3}><ActionIcon variant="subtle" aria-label={t('lines.moveUp')} disabled={disabled || index === 0} onClick={() => onMove(index - 1)}><IconArrowUp size={16} /></ActionIcon><ActionIcon variant="subtle" aria-label={t('lines.moveDown')} disabled={disabled || index === length - 1} onClick={() => onMove(index + 1)}><IconArrowDown size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('common.delete')} disabled={disabled} onClick={onRemove}><IconTrash size={16} /></ActionIcon></Group>
}
function NodeHopFields({ value, onChange, nodes, details, entry = false }: { value: { node_id: number; ingress_id?: number | null; address_mode?: ForwardAddressMode }; onChange: (patch: Partial<PathStep>) => void; nodes: Node[]; details: EntryNodeDetail[]; entry?: boolean }) {
  const { t } = useTranslation()
  const selected = nodes.find(node => node.id === value.node_id)
  const detail = details.find(detail => detail.node.id === value.node_id)
  const ingresses = detail?.ingresses || []
  return <Stack gap="sm"><SimpleGrid cols={{ base: 1, sm: 2 }}>
    <Select label={t('entryFlow.server')} searchable required value={value.node_id ? String(value.node_id) : null} data={nodes.filter(proxyServer).map(node => ({ value: String(node.id), label: node.name }))} onChange={id => onChange({ node_id: Number(id), ingress_id: null, address_mode: 'default' })} />
    <Select label={t('inbounds.ingress')} allowDeselect={false} disabled={!value.node_id || !detail} value={value.ingress_id ? String(value.ingress_id) : ''} data={[{ value: '', label: t('inbounds.ingressDirect'), disabled: ingresses.some(ingress => ingress.require_ingress) }, ...ingresses.map(ingress => ({ value: String(ingress.id), label: ingress.name }))]} onChange={id => onChange({ ingress_id: id ? Number(id) : null })} />
  </SimpleGrid>{!entry && <ForwardAddressModeSelect node={selected} value={value.address_mode} onChange={address_mode => onChange({ address_mode })} />}</Stack>
}
function PinnedPath({ id, revision, nodes }: { id: number; revision: number; nodes: Node[] }) {
  const { t } = useTranslation()
  const query = useQuery(revisionQuery(id, revision))
  if (!id || !revision) return null
  if (query.isError) return <Alert color="red"><Text size="sm">{query.error.message}</Text><Button variant="subtle" size="xs" onClick={() => query.refetch()}>{t('entryFlow.retry')}</Button></Alert>
  if (!query.data) return <Loader size="xs" />
  return <Text size="sm" c="dimmed">{query.data.hops.map(hop => nodes.find(node => node.id === hop.node_id)?.name || `#${hop.node_id}`).join(' → ')}</Text>
}
function PathEditor({ path, paths, nodes, details, lines, inbounds, onClose, onSaved }: Resources & { path?: ForwardPath; paths: ForwardPath[]; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [initial] = useState(() => ({ name: path?.name || '', version: path?.version || 0, steps: (path?.steps || [{ id: '', node_id: 0 }]).map(step => ({ ...step, uiKey: key() })) }))
  const [draft, setDraft] = useState(initial)
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial)
  const save = useMutation({ mutationFn: () => path ? api.put<ForwardPath>(`/api/admin/forward-paths/${path.id}`, pathInput(draft)) : api.post<ForwardPath>('/api/admin/forward-paths', pathInput(draft)), onSuccess: () => { toast.ok(t('common.saved')); onSaved(); onClose() } })
  useSettingsDirty(dirty || save.isPending)
  const close = () => { if (save.isPending) return; if (!dirty) return onClose(); modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose }) }
  const patch = (index: number, update: Partial<PathStep>) => setDraft(value => ({ ...value, steps: value.steps.map((step, i) => i === index ? { ...step, ...update } : step) }))
  const add = (step: PathStep) => setDraft(value => ({ ...value, steps: [...value.steps, { ...step, uiKey: key() }] }))
  const sources = lines.filter(line => {
    const inbound = inbounds.find(choice => choice.ib.ID === line.target_inbound_id)?.ib
    return linearSourceLine(line) && inbound && !inbound.Settings.reverse && !inbound.Settings.wireguard_slot && inbound.Protocol !== 'wireguard'
  })
  return <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={t(path ? 'pathRoutes.editPath' : 'pathRoutes.createPath')} size="lg"><form onSubmit={event => { event.preventDefault(); if (draft.name.trim() && pathStepsComplete(draft.steps)) save.mutate() }}><Stack>
    <Text size="sm" c="dimmed">{t('pathRoutes.templateHint')}</Text>
    {save.isError && <Alert color="red">{save.error.message}</Alert>}
    <Box component="fieldset" disabled={save.isPending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}><Stack>
      <TextInput required label={t('pathRoutes.name')} value={draft.name} onChange={event => { const name = event.currentTarget.value; setDraft(value => ({ ...value, name })) }} />
      {draft.steps.map((step, index) => <Card key={step.uiKey} withBorder><Stack gap="sm"><Group justify="space-between"><Text fw={600} size="sm">{t('pathRoutes.step', { number: index + 1 })} · {t(step.path_id !== undefined ? 'pathRoutes.pathReference' : step.line_id !== undefined ? 'pathRoutes.lineReference' : 'pathRoutes.serverHop')}</Text><Reorder index={index} length={draft.steps.length} onMove={to => setDraft(value => ({ ...value, steps: movePathItem(value.steps, index, to) }))} onRemove={() => setDraft(value => ({ ...value, steps: value.steps.filter((_, i) => i !== index) }))} /></Group>
        {step.path_id !== undefined ? <><Select label={t('pathRoutes.pathReference')} searchable required value={step.path_id ? String(step.path_id) : null} data={paths.filter(item => item.id !== path?.id).map(item => ({ value: String(item.id), label: item.name }))} onChange={id => patch(index, { path_id: Number(id), path_revision: paths.find(item => item.id === Number(id))?.version || 0 })} /><NumberInput required label={t('pathRoutes.fixedRevision')} min={1} allowDecimal={false} value={step.path_revision || ''} onChange={value => patch(index, { path_revision: Number(value) })} /><PinnedPath id={step.path_id} revision={step.path_revision || 0} nodes={nodes} /></>
          : step.line_id !== undefined ? <><Select label={t('pathRoutes.lineReference')} searchable required allowDeselect={false} value={step.line_id ? String(step.line_id) : null} data={pathSourceLineOptions(lines, sources, step, `${t('pathRoutes.fixedRevision')}: ${step.line_version}`)} onChange={id => patch(index, selectPathSourceLine(step, Number(id), sources))} /><NumberInput required label={t('pathRoutes.fixedRevision')} min={1} allowDecimal={false} value={step.line_version || ''} onChange={value => patch(index, { line_version: Number(value) })} /><Checkbox label={t('pathRoutes.includeLanding')} checked={!!step.include_landing} onChange={event => patch(index, { include_landing: event.currentTarget.checked })} /><Text size="xs" c="dimmed">{t('pathRoutes.lineHint')}</Text></>
            : <NodeHopFields value={{ ...step, node_id: step.node_id || 0 }} onChange={update => patch(index, update)} nodes={nodes} details={details} />}
      </Stack></Card>)}
      <Group><Button variant="light" disabled={draft.steps.length >= 8} onClick={() => add({ id: '', node_id: 0 })}>{t('pathRoutes.addServer')}</Button><Button variant="light" disabled={draft.steps.length >= 8 || !paths.some(item => item.id !== path?.id)} onClick={() => add({ id: '', path_id: 0, path_revision: 0 })}>{t('pathRoutes.addPath')}</Button><Button variant="light" disabled={draft.steps.length >= 8 || sources.length === 0} onClick={() => add({ id: '', line_id: 0, line_version: 0, include_landing: false })}>{t('pathRoutes.addLine')}</Button></Group>
      <Text size="xs" c="dimmed">{t('pathRoutes.pinnedHint')}</Text>
    </Stack></Box>
    <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={!draft.name.trim() || !pathStepsComplete(draft.steps)}>{t('common.save')}</Button></Group>
  </Stack></form></Modal>
}

function RouteEditor({ route, paths, nodes, details, inbounds, onClose, onSaved, onStage }: Resources & { route?: PathRoute; paths: ForwardPath[]; onClose: () => void; onSaved: (route: PathRoute) => void; onStage: (draft: PathRoute) => void }) {
  const { t } = useTranslation()
  const [initial] = useState(() => ({ ...(route || { id: 0, version: 0, name: '', entry: { id: '', node_id: 0, port: 0, weight: 1 }, target_inbound_id: 0, target_address_mode: 'default' as const, strategy: 'failover' as const, branches: [] }), health: pathHealthInput(route?.health), branches: (route?.branches || [{ id: '', path_id: 0, path_revision: 0, weight: 1, ports: [] }]).map(branch => ({ ...branch, ports: [...branch.ports], uiKey: key() })) }))
  const [draft, setDraft] = useState(initial)
  const [review, setReview] = useState<{ source: string; value: PathRoutePreview } | null>(null)
  const input = routeInput(draft)
  const reviewCurrent = review?.source === JSON.stringify(input)
  const branchQueries = useQueries({ queries: draft.branches.map(branch => revisionQuery(branch.path_id, branch.path_revision)) })
  const options = useQuery({ queryKey: ['line-options', draft.target_inbound_id], queryFn: () => api.get<{ protocol: string; error?: string }>(`/api/admin/lines/options?inbound_id=${draft.target_inbound_id}`), enabled: draft.target_inbound_id > 0 })
  const target = inbounds.find(choice => choice.ib.ID === draft.target_inbound_id)
  const valid = !!draft.name.trim() && draft.entry.node_id > 0 && validPathPort(draft.entry.port) && !!target?.ib.Enabled && options.data?.protocol === 'tcp' && !options.data.error && !options.isError && validPathHealthPolicy(draft.health, draft.strategy) && routePortsComplete(draft.branches, branchQueries.map(query => query.isError ? undefined : query.data))
  const preview = useMutation({ mutationFn: async () => ({ source: JSON.stringify(input), value: await api.post<PathRoutePreview>(route ? `/api/admin/path-routes/${route.id}/preview` : '/api/admin/path-routes/preview', input) }), onSuccess: setReview })
  const save = useMutation({ mutationFn: () => { if (!review || !reviewCurrent) throw new Error(t('lineImpact.stale')); const body = { ...review.value.draft, preview_revision: review.value.revision }; return route ? api.put<PathRoute>(`/api/admin/path-routes/${route.id}`, body) : api.post<PathRoute>('/api/admin/path-routes', body) }, onSuccess: saved => { toast.ok(t('common.saved')); onClose(); onSaved(saved) }, onError: () => setReview(null) })
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial)
  useSettingsDirty(dirty || save.isPending)
  const close = () => { if (save.isPending) return; if (!dirty) return onClose(); modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose }) }
  const patchBranch = (index: number, patch: Partial<PathBranch>) => setDraft(value => ({ ...value, branches: value.branches.map((branch, i) => i === index ? { ...branch, ...patch } : branch) }))
  return <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={t(route ? 'pathRoutes.editRoute' : 'pathRoutes.createRoute')} size="xl"><form onSubmit={event => { event.preventDefault(); if (valid) { if (reviewCurrent) save.mutate(); else preview.mutate() } }}><Stack>
    <Alert color="blue">{t('pathRoutes.routeHint')}</Alert>
    {save.isError && <Alert color="red">{save.error.message}</Alert>}
    <Box component="fieldset" disabled={save.isPending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}><Stack>
      <TextInput label={t('pathRoutes.name')} required value={draft.name} onChange={event => { const name = event.currentTarget.value; setDraft(value => ({ ...value, name })) }} />
      <Card withBorder><Stack gap="sm"><Title order={5}>{t('pathRoutes.entry')}</Title><NodeHopFields entry nodes={nodes} details={details} value={draft.entry} onChange={patch => setDraft(value => ({ ...value, entry: { ...value.entry, ...patch } }))} /><NumberInput label={t('lines.listenPort')} required min={1} max={65535} allowDecimal={false} value={draft.entry.port || ''} onChange={value => setDraft(draft => ({ ...draft, entry: { ...draft.entry, port: Number(value) } }))} /></Stack></Card>
      <Select label={t('pathRoutes.selection')} allowDeselect={false} value={draft.strategy} data={[{ value: 'failover', label: t('lines.strategies.failover') }, { value: 'roundrobin', label: t('lines.strategies.roundrobin') }]} onChange={value => setDraft(draft => ({ ...draft, strategy: value === 'roundrobin' ? 'roundrobin' : 'failover' }))} />
      {draft.branches.map((branch, index) => <Card withBorder key={branch.uiKey}><Stack gap="sm">
        <Group justify="space-between"><Title order={5}>{t('pathRoutes.branch', { number: index + 1 })}</Title><Reorder index={index} length={draft.branches.length} onMove={to => setDraft(value => ({ ...value, branches: movePathItem(value.branches, index, to) }))} onRemove={() => setDraft(value => ({ ...value, branches: value.branches.filter((_, i) => i !== index) }))} /></Group>
        <SimpleGrid cols={{ base: 1, sm: 2 }}><Select label={t('pathRoutes.pathReference')} required searchable value={branch.path_id ? String(branch.path_id) : null} data={paths.map(path => ({ value: String(path.id), label: path.name }))} onChange={id => patchBranch(index, { path_id: Number(id), path_revision: paths.find(path => path.id === Number(id))?.version || 0, ports: [] })} /><NumberInput label={t('pathRoutes.fixedRevision')} required min={1} allowDecimal={false} value={branch.path_revision || ''} onChange={value => patchBranch(index, { path_revision: Number(value), ports: [] })} /></SimpleGrid>
        {draft.strategy === 'roundrobin' && <NumberInput label={t('lines.weight')} required min={1} max={100} allowDecimal={false} value={branch.weight} onChange={value => patchBranch(index, { weight: Number(value) })} />}
        {branch.path_id > 0 && branch.path_revision > 0 && branchQueries[index].isPending && <Loader size="xs" />}
        {branchQueries[index].isError && <Alert color="red"><Text>{branchQueries[index].error?.message}</Text><Button size="xs" variant="subtle" onClick={() => branchQueries[index].refetch()}>{t('entryFlow.retry')}</Button></Alert>}
        {branchQueries[index].data && <><Text size="xs" c="dimmed">{t('pathRoutes.portsHint')}</Text><SimpleGrid cols={{ base: 1, sm: 2 }}>{branchQueries[index].data.hops.map((hop, hopIndex) => <NumberInput key={hop.key} label={`${hopIndex + 1}. ${nodes.find(node => node.id === hop.node_id)?.name || `#${hop.node_id}`}`} description={hop.address_mode === 'internal' ? t('nodes.internalAddr') : hop.ingress_id ? details.flatMap(detail => detail.ingresses || []).find(ingress => ingress.id === hop.ingress_id)?.name : undefined} min={1} max={65535} allowDecimal={false} required value={branch.ports[hopIndex] || ''} onChange={value => { const ports = Array.from({ length: branchQueries[index].data!.hops.length }, (_, i) => branch.ports[i] || 0); ports[hopIndex] = Number(value); patchBranch(index, { ports }) }} />)}</SimpleGrid></>}
      </Stack></Card>)}
      <Button variant="light" disabled={draft.branches.length >= 8} onClick={() => setDraft(value => ({ ...value, branches: [...value.branches, { uiKey: key(), id: '', path_id: 0, path_revision: 0, weight: 1, ports: [] }] }))}>{t('pathRoutes.addBranch')}</Button>
      <Select label={t('lines.target')} required searchable value={draft.target_inbound_id ? String(draft.target_inbound_id) : null} data={inbounds.map(choice => ({ value: String(choice.ib.ID), label: `${choice.node.name} / ${choice.ib.Tag} (${choice.ib.Protocol}:${choice.ib.Port})`, disabled: !choice.ib.Enabled || !!choice.ib.Settings.reverse || !!choice.ib.Settings.wireguard_slot || !!choice.ib.Settings.accept_proxy_protocol || choice.ib.Protocol === 'wireguard' }))} onChange={value => setDraft(draft => ({ ...draft, target_inbound_id: Number(value) }))} />
      <ForwardAddressModeSelect node={target?.node} value={draft.target_address_mode} onChange={target_address_mode => setDraft(value => ({ ...value, target_address_mode }))} />
      <PathHealthFields value={draft.health} strategy={draft.strategy} onChange={health => setDraft(value => ({ ...value, health }))} />
      {draft.target_inbound_id > 0 && (options.isError || options.data?.error || options.data && options.data.protocol !== 'tcp') && <Alert color="red"><Text>{options.error?.message || options.data?.error || t('pathRoutes.tcpOnly')}</Text><Button variant="subtle" size="xs" onClick={() => options.refetch()}>{t('entryFlow.retry')}</Button></Alert>}
    </Stack></Box>
    {preview.isError && <Alert color="red">{preview.error.message}</Alert>}
    {review && !reviewCurrent && <Alert color="yellow">{t('lineImpact.stale')}</Alert>}
    {reviewCurrent && review && <PathImpact value={review.value} />}
    <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button>{reviewCurrent && <Button variant="default" loading={preview.isPending} disabled={save.isPending} onClick={() => preview.mutate()}>{t('lineImpact.refresh')}</Button>}<Button variant="light" disabled={!valid || save.isPending || preview.isPending} onClick={() => onStage(input)}>{t('lineRollouts.plan')}</Button><Button type="submit" loading={preview.isPending || save.isPending} disabled={!valid}>{reviewCurrent ? t('lineImpact.save') : t('lineImpact.preview')}</Button></Group>
  </Stack></form></Modal>
}
function PathImpact({ value }: { value: PathRoutePreview }) {
  const { t } = useTranslation()
  const name = (id: number) => value.dependencies.find(node => node.node_id === id)?.name || `#${id}`
  return <Card withBorder><Stack><Title order={4}>{t('lineImpact.title')}</Title><Text size="sm" c="dimmed">{t('pathRoutes.impactHint')}</Text><Title order={5}>{t('lineImpact.changes', { count: value.changes.length })}</Title>
    {value.changes.map(change => <Stack key={change.node_id} gap="xs"><Text fw={600}>{change.name}</Text><SimpleGrid cols={{ base: 1, sm: 2 }}>{[change.before, change.after].map((forwards, index) => <Stack key={index} gap={4}><Text size="xs" fw={500}>{index === 0 ? t('lineImpact.before') : t('lineImpact.after')}</Text><Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 260 }}>{JSON.stringify(forwards, null, 2)}</Code></Stack>)}</SimpleGrid></Stack>)}
    <Text fw={600}>{t('lineImpact.endpoints')}</Text><Group>{value.endpoints.map(endpoint => <Code key={endpoint.member_id}>{hostPort(endpoint.host, endpoint.port)}</Code>)}</Group>
    <Text fw={600}>{t('lineImpact.dependencies')}</Text><Text size="sm" c="dimmed">{t('pathRoutes.topologyHint')}</Text>
    <Group gap="xs">{value.dependencies.map((node, index) => <Badge key={`${node.node_id}-${index}`} variant="light">{node.name} · {t(`lineImpact.roles.${node.role}`)}</Badge>)}</Group>
    <Table.ScrollContainer minWidth={360}><Table><Table.Thead><Table.Tr><Table.Th>{t('lineImpact.from')}</Table.Th><Table.Th>{t('lineImpact.to')}</Table.Th><Table.Th>{t('lineImpact.connection')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{value.edges.map((edge, index) => <Table.Tr key={index}><Table.Td>{name(edge.from)}</Table.Td><Table.Td>{name(edge.to)}</Table.Td><Table.Td>{t(`lineImpact.edges.${edge.kind}`)}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
    <Text fw={600}>{t('lineImpact.uses')}</Text>{value.uses?.length ? value.uses.map(use => <Text key={use.entry_id} size="sm">#{use.entry_id} {use.name} · {t(`lineImpact.usage.${use.kind}`)}{!use.enabled && ` · ${t('common.disabled')}`}</Text>) : <Text size="sm" c="dimmed">{t('lineImpact.noUses')}</Text>}
  </Stack></Card>
}
function PathUses({ path, onClose }: { path: ForwardPath; onClose: () => void }) {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['forward-path-uses', path.id], queryFn: () => api.get<PathUse[]>(`/api/admin/forward-paths/${path.id}/uses`) })
  return <Modal opened onClose={onClose} title={`${t('pathRoutes.uses')} · ${path.name}`}><Stack>{query.isPending ? <Loader size="sm" /> : query.isError ? <Alert color="red"><Text>{query.error.message}</Text><Button variant="subtle" onClick={() => query.refetch()}>{t('entryFlow.retry')}</Button></Alert> : query.data.length ? query.data.map(use => <Text key={`${use.kind}-${use.id}-${use.version}`} size="sm">{use.name} · {t(use.kind === 'path' ? 'pathRoutes.paths' : 'pathRoutes.routes')} #{use.id} · {t('pathRoutes.revisionValue', { version: use.version })}</Text>) : <Text c="dimmed">{t('pathRoutes.noUses')}</Text>}</Stack></Modal>
}
