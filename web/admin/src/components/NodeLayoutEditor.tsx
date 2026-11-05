import { ActionIcon, Alert, Button, Card, Checkbox, Group, Modal, Select, Stack, Text, TextInput } from '@mantine/core'
import { DndContext, PointerSensor, KeyboardSensor, closestCenter, pointerWithin, useDroppable, DragOverlay, useSensor, useSensors, type CollisionDetection } from '@dnd-kit/core'
import { SortableContext, useSortable, sortableKeyboardCoordinates, verticalListSortingStrategy, arrayMove } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { IconArrowUp, IconArrowDown, IconGripVertical, IconPencil, IconTrash } from '@tabler/icons-react'
import { useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type Node } from '../lib/api'
import { toast } from '../lib/notify'

import { type NodeLayout } from '../lib/node-layout'
import { localizedName, localizedNameDraft } from '../lib/localized-name'

function SortRow({ id, children, index, count, move, receiving = false }: { receiving?: boolean; id: string; children: ReactNode; index: number; count: number; move: (from: number, to: number) => void }) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, transform, transition, isOver } = useSortable({ id })
  return <Card ref={setNodeRef} p="xs" style={{ transform: CSS.Transform.toString(transform), transition, outline: receiving && isOver ? '2px solid var(--mantine-primary-color-filled)' : undefined, background: receiving && isOver ? 'var(--mantine-primary-color-light)' : undefined }}>
    <Group wrap="nowrap" gap="xs">
      <ActionIcon variant="subtle" style={{ touchAction: 'none', flexShrink: 0 }} aria-label={t('nodeLayout.drag')} {...attributes} {...listeners}><IconGripVertical size={18} /></ActionIcon>
      <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
      <Stack gap={2}><ActionIcon variant="subtle" size="sm" aria-label={t('nodeLayout.up')} disabled={index === 0} onClick={() => move(index, index - 1)}><IconArrowUp size={16} /></ActionIcon><ActionIcon variant="subtle" size="sm" aria-label={t('nodeLayout.down')} disabled={index === count - 1} onClick={() => move(index, index + 1)}><IconArrowDown size={16} /></ActionIcon></Stack>
    </Group>
  </Card>
}
function UngroupedDrop({ active, children }: { active: boolean; children: ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: 'group:' })
  return <Card ref={setNodeRef} p="xs" style={{ outline: active && isOver ? '2px solid var(--mantine-primary-color-filled)' : undefined, background: active && isOver ? 'var(--mantine-primary-color-light)' : undefined }}>{children}</Card>
}
const layoutCollision: CollisionDetection = args => {
  const isNode = String(args.active.id).startsWith('node:')
  const filtered = { ...args, droppableContainers: args.droppableContainers.filter(c => isNode || (String(c.id).startsWith('group:') && c.id !== 'group:')) }
  // Pointer drops must actually hit a card; empty space must not move a node.
  return args.pointerCoordinates ? pointerWithin(filtered) : closestCenter(filtered)
}

export function NodeLayoutEditor({ initial, nodes, close, onSaved }: { initial: NodeLayout; nodes: Node[]; close: () => void; onSaved: () => void }) {
  const { t, i18n } = useTranslation(), qc = useQueryClient()
  const [draft, setDraft] = useState<NodeLayout>(() => structuredClone(initial))
  const [group, setGroup] = useState('')
  const [selected, setSelected] = useState<number[]>([])
  const [target, setTarget] = useState<string | null>(null)
  const [rename, setRename] = useState<string | null>(null), [name, setName] = useState({ zh: '', en: '' })
  const [conflict, setConflict] = useState(false)
  const [dragged, setDragged] = useState<string | null>(null)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))
  const save = useMutation({ mutationFn: () => api.put<NodeLayout>('/api/admin/nodes/layout', draft), onSuccess: () => { qc.invalidateQueries({ queryKey: ['nodes'] }); qc.invalidateQueries({ queryKey: ['node'] }); qc.invalidateQueries({ queryKey: ['monitoring'] }); qc.invalidateQueries({ queryKey: ['node-layout'] }); toast.ok(t('common.saved')); onSaved(); close() }, onError: e => { if (e instanceof ApiError && e.status === 409) setConflict(true); else toast.err(e) } })
  const groupName = (key: string) => localizedName(i18n.resolvedLanguage || i18n.language, draft.group_names?.[key]?.zh, draft.group_names?.[key]?.en, key)
  const options = [{ value: '', label: t('nodeLayout.ungrouped') }, ...draft.groups.map(g => ({ value: g, label: groupName(g) }))]
  const members = draft.nodes.filter(n => n.group === group)
  const moveGroups = (from: number, to: number) => setDraft(d => ({ ...d, groups: arrayMove(d.groups, from, to) }))
  const moveNodes = (from: number, to: number) => {
    const reordered = arrayMove(members, from, to)
    let i = 0
    setDraft(d => ({ ...d, nodes: d.nodes.map(n => n.group === group ? reordered[i++] : n) }))
  }
  const applyName = () => {
    const names = { zh: name.zh.trim(), en: name.en.trim() }
    const key = rename || names.zh || names.en
    if ((!names.zh && !names.en) || Object.values(names).some(value => new TextEncoder().encode(value).length > 128 || /[\p{Cc}<>&"'`]/u.test(value)) || (rename === '' && draft.groups.includes(key))) { toast.err(t('nodeLayout.invalidName')); return }
    setDraft(d => ({ ...d, groups: rename === '' ? [...d.groups, key] : d.groups, group_names: { ...d.group_names, [key]: names } }))
    setRename(null)
  }
  const removeGroup = (g: string) => {
    setDraft(d => ({ ...d, groups: d.groups.filter(x => x !== g), group_names: Object.fromEntries(Object.entries(d.group_names || {}).filter(([key]) => key !== g)), nodes: d.nodes.map(n => n.group === g ? { ...n, group: '' } : n) }))
    if (group === g) { setGroup(''); setSelected([]) }
    if (target === g) setTarget(null)
  }
  return <Modal opened onClose={() => { if (!save.isPending) close() }} closeOnClickOutside={false} title={t('nodeLayout.title')} size="lg">
    <DndContext sensors={sensors} collisionDetection={layoutCollision} onDragStart={({ active }) => setDragged(String(active.id))} onDragCancel={() => setDragged(null)} onDragEnd={({ active, over }) => {
      setDragged(null)
      if (!over || save.isPending) return
      const source = String(active.id), destination = String(over.id)
      if (source.startsWith('node:') && destination.startsWith('group:')) {
        const id = Number(source.slice(5)), nextGroup = destination.slice(6)
        if (nextGroup === group || (nextGroup && !draft.groups.includes(nextGroup))) return
        setDraft(d => ({ ...d, nodes: [...d.nodes.filter(n => n.id !== id), ...d.nodes.filter(n => n.id === id).map(n => ({ ...n, group: nextGroup }))] }))
        setSelected(ids => ids.filter(value => value !== id))
      } else if (source.startsWith('node:') && destination.startsWith('node:')) {
        const from = members.findIndex(n => n.id === Number(source.slice(5))), to = members.findIndex(n => n.id === Number(destination.slice(5)))
        if (from >= 0 && to >= 0 && from !== to) moveNodes(from, to)
      } else if (source.startsWith('group:') && destination.startsWith('group:')) {
        const from = draft.groups.indexOf(source.slice(6)), to = draft.groups.indexOf(destination.slice(6))
        if (from >= 0 && to >= 0 && from !== to) moveGroups(from, to)
      }
    }}>
    <Stack>
      <Alert>{t('nodeLayout.hint')}</Alert>
      {conflict && <Alert color="orange">{t('nodeLayout.conflict')}</Alert>}
      <fieldset disabled={save.isPending} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack>
      <Group justify="space-between"><Text fw={600}>{t('monitoring.group')}</Text><Button size="xs" variant="light" onClick={() => { setRename(''); setName({ zh: '', en: '' }) }}>{t('nodeLayout.addGroup')}</Button></Group>
      <SortableContext items={draft.groups.map(g => `group:${g}`)} strategy={verticalListSortingStrategy}><Stack gap="xs">{draft.groups.map((g, i) => <SortRow key={g} id={`group:${g}`} receiving={dragged?.startsWith('node:')} index={i} count={draft.groups.length} move={moveGroups}><Group justify="space-between" wrap="nowrap"><Text style={{ overflowWrap: 'anywhere' }}>{groupName(g)} <Text span size="xs" c="dimmed">({draft.nodes.filter(n => n.group === g).length})</Text></Text><Group gap={4} wrap="nowrap"><ActionIcon variant="subtle" aria-label={t('nodeLayout.rename')} onClick={() => { setRename(g); setName(localizedNameDraft(draft.group_names?.[g]?.zh, draft.group_names?.[g]?.en, g)) }}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('nodeLayout.deleteGroup')} onClick={() => removeGroup(g)}><IconTrash size={16} /></ActionIcon></Group></Group></SortRow>)}</Stack></SortableContext>
      <UngroupedDrop active={!!dragged?.startsWith('node:')}><Text>{t('nodeLayout.ungrouped')} <Text span size="xs" c="dimmed">({draft.nodes.filter(n => !n.group).length})</Text></Text></UngroupedDrop>
      <Text size="xs" c="dimmed">{t('nodeLayout.deleteHint')}</Text>
      <Select label={t('nodeLayout.groupNodes')} value={group} onChange={v => { setGroup(v ?? ''); setSelected([]) }} data={options} allowDeselect={false} />
      <Checkbox label={t('nodeLayout.selectAll')} checked={members.length > 0 && selected.length === members.length} indeterminate={selected.length > 0 && selected.length < members.length} onChange={e => setSelected(e.currentTarget.checked ? members.map(n => n.id) : [])} />
      {selected.length > 0 && <Group align="flex-end"><Select style={{ flex: 1 }} label={t('nodeLayout.moveTo')} data={options} value={target} onChange={setTarget} /><Button disabled={target === null || target === group} onClick={() => { if (target === null) return; const chosen = new Set(selected); setDraft(d => ({ ...d, nodes: [...d.nodes.filter(n => !chosen.has(n.id)), ...d.nodes.filter(n => chosen.has(n.id)).map(n => ({ ...n, group: target }))] })); setSelected([]) }}>{t('nodeLayout.move')}</Button></Group>}
      <SortableContext items={members.map(n => `node:${n.id}`)} strategy={verticalListSortingStrategy}><Stack gap="xs">{members.map((n, i) => <SortRow key={n.id} id={`node:${n.id}`} index={i} count={members.length} move={moveNodes}><Checkbox label={nodes.find(x => x.id === n.id)?.name ?? String(n.id)} checked={selected.includes(n.id)} onChange={e => setSelected(e.currentTarget.checked ? [...selected, n.id] : selected.filter(id => id !== n.id))} /></SortRow>)}</Stack></SortableContext>
      {members.length === 0 && <Text c="dimmed" ta="center">{t('common.empty')}</Text>}
      </Stack></fieldset>
      <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button loading={save.isPending} disabled={conflict} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
    </Stack>
    <DragOverlay>{dragged?.startsWith('node:') && <Card p="sm" shadow="md"><Text>{nodes.find(n => n.id === Number(dragged.slice(5)))?.name ?? dragged.slice(5)}</Text></Card>}</DragOverlay>
    </DndContext>
    <Modal opened={rename !== null} onClose={() => setRename(null)} title={t(rename === '' ? 'nodeLayout.addGroup' : 'nodeLayout.rename')}><form onSubmit={e => { e.preventDefault(); applyName() }}><Stack><TextInput data-autofocus label={t('bilingualName.zh')} value={name.zh} onChange={e => setName({ ...name, zh: e.currentTarget.value })} /><TextInput label={t('bilingualName.en')} value={name.en} onChange={e => setName({ ...name, en: e.currentTarget.value })} /><Text size="xs" c="dimmed">{t('bilingualName.hint')}</Text><Button type="submit" disabled={!name.zh.trim() && !name.en.trim()}>{t('common.save')}</Button></Stack></form></Modal>
  </Modal>
}
