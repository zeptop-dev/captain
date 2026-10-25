import { ActionIcon, Alert, Button, Card, Group, Loader, Modal, Stack, Text, Textarea, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { IconArrowDown, IconArrowUp, IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type EntryCategory } from '../lib/api'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

export function EntryCategoriesManager({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['entry-categories'], queryFn: () => api.get<EntryCategory[]>('/api/admin/entry-categories') })
  const [editing, setEditing] = useState<EntryCategory | 'new' | null>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const dirty = editing !== null && (name !== (editing === 'new' ? '' : editing.Name) || description !== (editing === 'new' ? '' : editing.Description))
  const invalidate = () => Promise.all([
    qc.invalidateQueries({ queryKey: ['entry-categories'] }), qc.invalidateQueries({ queryKey: ['entries'] }),
  ])
  const save = useMutation({
    mutationFn: () => editing === 'new' ? api.post('/api/admin/entry-categories', { Name: name.trim(), Description: description.trim() }) : api.patch(`/api/admin/entry-categories/${editing?.ID}`, { Name: name.trim(), Description: description.trim() }),
    onSuccess: async () => { setEditing(null); await invalidate(); toast.ok(t('common.saved')) }, onError: toast.err,
  })
  const remove = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/entry-categories/${id}`), onSuccess: async () => { await invalidate(); toast.ok(t('common.deleted')) }, onError: toast.err })
  const order = useMutation({ mutationFn: (IDs: number[]) => api.put('/api/admin/entry-categories/order', { IDs }), onSuccess: invalidate, onError: (error: Error) => { toast.err(error); invalidate() } })
  const busy = save.isPending || remove.isPending || order.isPending
  useSettingsDirty(dirty || busy)
  const leaveDraft = (next: () => void) => {
    if (busy) return
    if (!dirty) { next(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: next })
  }
  const edit = (category: EntryCategory | 'new') => leaveDraft(() => { setEditing(category); setName(category === 'new' ? '' : category.Name); setDescription(category === 'new' ? '' : category.Description) })
  const move = (index: number, offset: number) => {
    const ids = (q.data ?? []).map(category => category.ID)
    if (index + offset < 0 || index + offset >= ids.length) return
    ;[ids[index], ids[index + offset]] = [ids[index + offset], ids[index]]
    order.mutate(ids)
  }
  return <Modal opened onClose={() => leaveDraft(onClose)} title={t('entryCategories.manage')} size="lg" closeOnClickOutside={false} closeOnEscape={!busy} withCloseButton={!busy}>
    <Stack>
      <Text size="sm" c="dimmed">{t('entryCategories.managerHint')}</Text>
      {q.isLoading && <Loader size="sm" />}
      {q.isError && <Alert color="red"><Text size="sm">{t('entryCategories.loadError')}</Text><Button variant="subtle" size="xs" onClick={() => q.refetch()}>{t('entryFlow.retry')}</Button></Alert>}
      {!q.isLoading && !q.isError && !q.data?.length && <Text c="dimmed" size="sm">{t('entryCategories.empty')}</Text>}
      {(q.data ?? []).map((category, index) => <Card withBorder p="sm" key={category.ID}>
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Stack gap={2} style={{ minWidth: 0 }}><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{category.Name}</Text>{category.Description && <Text size="sm" c="dimmed" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{category.Description}</Text>}</Stack>
          <Group gap={2} wrap="nowrap">
            <ActionIcon variant="subtle" disabled={busy || q.isError || index === 0} aria-label={t('entryCategories.moveUp', { name: category.Name })} onClick={() => move(index, -1)}><IconArrowUp size={16} /></ActionIcon>
            <ActionIcon variant="subtle" disabled={busy || q.isError || index === (q.data?.length ?? 0) - 1} aria-label={t('entryCategories.moveDown', { name: category.Name })} onClick={() => move(index, 1)}><IconArrowDown size={16} /></ActionIcon>
            <ActionIcon variant="subtle" disabled={busy || q.isError} aria-label={`${t('common.edit')} ${category.Name}`} onClick={() => edit(category)}><IconPencil size={16} /></ActionIcon>
            <ActionIcon variant="subtle" color="red" disabled={busy || q.isError || editing !== null} aria-label={`${t('common.delete')} ${category.Name}`} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('entryCategories.deleteHint', { name: category.Name })}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => remove.mutate(category.ID) })}><IconTrash size={16} /></ActionIcon>
          </Group>
        </Group>
      </Card>)}
      {editing === null ? <Button variant="light" leftSection={<IconPlus size={16} />} disabled={busy || q.isLoading || q.isError} onClick={() => edit('new')}>{t('entryCategories.create')}</Button> : <Card withBorder>
        <form onSubmit={event => { event.preventDefault(); if (name.trim() && !busy && !q.isLoading && !q.isError) save.mutate() }}><Stack>
          <Text fw={600}>{t(editing === 'new' ? 'entryCategories.create' : 'entryCategories.edit')}</Text>
          <TextInput label={t('entryCategories.name')} required maxLength={80} value={name} disabled={busy} onChange={event => setName(event.currentTarget.value)} autoFocus />
          <Textarea label={t('entryCategories.description')} maxLength={500} minRows={2} maxRows={5} autosize value={description} disabled={busy} onChange={event => setDescription(event.currentTarget.value)} />
          <Group justify="flex-end"><Button variant="default" disabled={busy} onClick={() => leaveDraft(() => setEditing(null))}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={busy || !name.trim() || q.isError}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Card>}
      <Group justify="flex-end"><Button variant="default" disabled={busy} onClick={() => leaveDraft(onClose)}>{t('entryCategories.close')}</Button></Group>
    </Stack>
  </Modal>
}
