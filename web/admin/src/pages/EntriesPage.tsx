import { ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Select, Stack, Switch, Table, TagsInput, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconGripVertical, IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Entry, type Inbound, type Node } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { REGIONS, flag } from '../lib/regions'

type Values = { Name: string; InboundID: string; DisplayHost: string; DisplayPort: number; Rate: number; Sort: number; Enabled: boolean; Tags: string[]; Region: string }
const empty: Values = { Name: '', InboundID: '', DisplayHost: '', DisplayPort: 443, Rate: 1, Sort: 0, Enabled: true, Tags: [], Region: '' }

export default function EntriesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries') })
  const tags = useQuery({ queryKey: ['entry-tags'], queryFn: () => api.get<string[]>('/api/admin/entries/tags') })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const details = useQueries({ queries: (nodes.data ?? []).map((n) => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<{ node: Node; inbounds: Inbound[] }>(`/api/admin/nodes/${n.id}`) })) })
  const inbounds = details.flatMap((d) => (d.data ? d.data.inbounds.map((ib) => ({ ib, node: d.data!.node })) : []))
  const label = (id: number) => { const x = inbounds.find((i) => i.ib.ID === id); return x ? `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol}:${x.ib.Port})` : `#${id}` }
  const [editing, setEditing] = useState<Entry | 'new' | null>(null)
  const [filter, setFilter] = useState<string | null>(null)
  const form = useForm<Values>({ initialValues: empty })
  const payload = (v: Values) => ({ Name: v.Name, InboundID: Number(v.InboundID), DisplayHost: v.DisplayHost, DisplayPort: v.DisplayPort, Rate: v.Rate, Sort: v.Sort, Enabled: v.Enabled, Tags: v.Tags, Region: v.Region })
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['entry-tags'] }) }
  const save = useMutation({ mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/entries', payload(v)) : api.patch(`/api/admin/entries/${(editing as Entry).ID}`, payload(v)), onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); invalidate() }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/entries/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const reorder = useMutation({ mutationFn: (ids: number[]) => api.put('/api/admin/entries/order', { IDs: ids }), onSuccess: invalidate, onError: (e: Error) => { toast.err(e); invalidate() } })
  const openEdit = (e: Entry | 'new') => {
    if (e === 'new') form.setValues(empty)
    else form.setValues({ Name: e.Name, InboundID: String(e.InboundID), DisplayHost: e.DisplayHost, DisplayPort: e.DisplayPort, Rate: e.Rate, Sort: e.Sort, Enabled: e.Enabled, Tags: e.Tags ?? [], Region: e.Region ?? '' })
    setEditing(e)
  }
  // Picking an inbound pre-fills the display address from the node.
  const onInbound = (id: string | null) => { form.setFieldValue('InboundID', id ?? ''); const x = inbounds.find((i) => String(i.ib.ID) === id); if (x && !form.values.DisplayHost) { const tls = x.ib.Settings?.tls as { mode?: number; server_name?: string } | undefined; form.setValues({ DisplayHost: (tls?.mode === 1 && tls.server_name) || x.node.public_addr || '', DisplayPort: x.ib.Port }) } }

  // Local order for drag-and-drop; the server is told the new id order on drop.
  const [rows, setRows] = useState<Entry[]>([])
  useEffect(() => { setRows(q.data ?? []) }, [q.data])
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }))
  const onDragEnd = (ev: DragEndEvent) => {
    const { active, over } = ev
    if (!over || active.id === over.id) return
    const from = rows.findIndex((r) => r.ID === active.id), to = rows.findIndex((r) => r.ID === over.id)
    const next = arrayMove(rows, from, to)
    setRows(next)
    reorder.mutate(next.map((r) => r.ID))
  }
  const visible = filter ? rows.filter((r) => (r.Tags ?? []).includes(filter)) : rows
  return (
    <>
      <PageHeader title={t('entries.title')} subtitle={t('entries.subtitle')} actions={<Group gap="xs">{(tags.data?.length ?? 0) > 0 && <Select size="xs" w={160} clearable placeholder={t('entries.filterTag')} data={tags.data ?? []} value={filter} onChange={setFilter} />}<Button leftSection={<IconPlus size={16} />} onClick={() => openEdit('new')}>{t('entries.create')}</Button></Group>} />
      <Card p={0}>
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={visible.map((r) => r.ID)} strategy={verticalListSortingStrategy}>
            <Table>
              <Table.Thead><Table.Tr><Table.Th w={32} /><Table.Th>{t('entries.name')}</Table.Th><Table.Th>{t('entries.inbound')}</Table.Th><Table.Th>{t('entries.displayHost')}</Table.Th><Table.Th>{t('entries.rate')}</Table.Th><Table.Th>{t('entries.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
              <Table.Tbody>
                {visible.map((e) => <Row key={e.ID} e={e} label={label} onEdit={() => openEdit(e)} onDelete={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(e.ID) })} draggable={!filter} />)}
                {visible.length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
              </Table.Tbody>
            </Table>
          </SortableContext>
        </DndContext>
      </Card>
      {!filter && rows.length > 1 && <Text size="xs" c="dimmed" mt="xs">{t('entries.dragHint')}</Text>}
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('entries.create') : t('common.edit')}>
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <TextInput label={t('entries.name')} required placeholder="Tokyo IPLC" {...form.getInputProps('Name')} />
          <Select label={t('entries.inbound')} required searchable data={inbounds.map((i) => ({ value: String(i.ib.ID), label: label(i.ib.ID) }))} value={form.values.InboundID} onChange={onInbound} />
          <Group grow><TextInput label={t('entries.displayHost')} placeholder={t('entries.displayHostHint')} {...form.getInputProps('DisplayHost')} /><NumberInput label={t('entries.displayPort')} required min={1} max={65535} {...form.getInputProps('DisplayPort')} /></Group>
          <Group grow>
            <Select label={t('entries.region')} description={t('entries.regionHint')} searchable clearable data={REGIONS.map((r) => ({ value: r.code, label: `${flag(r.code)} ${r.code} ${r.name}` }))} value={form.values.Region || null} onChange={(v) => form.setFieldValue('Region', v ?? '')} />
            <NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} {...form.getInputProps('Rate')} />
          </Group>
          <TagsInput label={t('entries.tags')} description={t('entries.tagsHint')} data={tags.data ?? []} {...form.getInputProps('Tags')} />
          <Switch label={t('entries.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}

function Row({ e, label, onEdit, onDelete, draggable }: { e: Entry; label: (id: number) => string; onEdit: () => void; onDelete: () => void; draggable: boolean }) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: e.ID, disabled: !draggable })
  return (
    <Table.Tr ref={setNodeRef} style={{ transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.6 : 1, position: 'relative', zIndex: isDragging ? 1 : undefined }}>
      <Table.Td><ActionIcon variant="subtle" color="gray" size="sm" style={{ cursor: draggable ? 'grab' : 'default', touchAction: 'none' }} {...attributes} {...listeners}><IconGripVertical size={14} /></ActionIcon></Table.Td>
      <Table.Td><Group gap={6}><Text fw={600}>{e.Region ? `${flag(e.Region)} ` : ''}{e.Name}</Text>{(e.Tags ?? []).map((tg) => <Badge key={tg} size="xs" variant="light" color="grape">{tg}</Badge>)}</Group></Table.Td>
      <Table.Td><Text size="sm">{label(e.InboundID)}</Text></Table.Td>
      <Table.Td><Code>{e.DisplayHost}:{e.DisplayPort}</Code></Table.Td>
      <Table.Td>×{e.Rate}</Table.Td>
      <Table.Td>{e.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
      <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={onEdit}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={onDelete}><IconTrash size={16} /></ActionIcon></Group></Table.Td>
    </Table.Tr>
  )
}
