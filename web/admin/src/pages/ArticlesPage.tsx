import { ActionIcon, Badge, Button, Card, Group, Modal, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Article { id: number; title: string; category: string; body: string; lang: string; sort: number; published: boolean; updated_at: string }
type Values = { Title: string; Category: string; Body: string; Lang: string; Sort: number; Published: boolean }
const empty: Values = { Title: '', Category: '', Body: '', Lang: '', Sort: 0, Published: true }

// Knowledge base editor: Markdown articles grouped by category. {{sub_url}},
// {{email}} and {{site_name}} are substituted for the reader.
export default function ArticlesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['articles'], queryFn: () => api.get<Article[]>('/api/admin/articles') })
  const [editing, setEditing] = useState<Article | 'new' | null>(null)
  const form = useForm<Values>({ initialValues: empty })
  const save = useMutation({
    mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/articles', v) : api.patch(`/api/admin/articles/${(editing as Article).id}`, v),
    onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); qc.invalidateQueries({ queryKey: ['articles'] }) }, onError: toast.err,
  })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/articles/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['articles'] }) }, onError: toast.err })
  const open = (a: Article | 'new') => { form.setValues(a === 'new' ? empty : { Title: a.title, Category: a.category, Body: a.body, Lang: a.lang, Sort: a.sort, Published: a.published }); setEditing(a) }
  return (
    <>
      <PageHeader title={t('articles.title')} subtitle={t('articles.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => open('new')}>{t('articles.create')}</Button>} />
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('articles.titleCol')}</Table.Th><Table.Th>{t('articles.category')}</Table.Th><Table.Th>{t('articles.lang')}</Table.Th><Table.Th>{t('articles.sort')}</Table.Th><Table.Th>{t('articles.updated')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((a) => (
            <Table.Tr key={a.id}>
              <Table.Td><Group gap="xs"><Text size="sm" fw={600}>{a.title}</Text>{!a.published && <Badge size="xs" color="gray">{t('articles.draft')}</Badge>}</Group></Table.Td>
              <Table.Td><Text size="sm">{a.category}</Text></Table.Td>
              <Table.Td><Text size="sm" c="dimmed">{a.lang || t('articles.anyLang')}</Text></Table.Td>
              <Table.Td><Text size="sm">{a.sort}</Text></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{when(a.updated_at)}</Text></Table.Td>
              <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
                <ActionIcon variant="subtle" color="gray" onClick={() => open(a)}><IconPencil size={16} /></ActionIcon>
                <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(a.id) })}><IconTrash size={16} /></ActionIcon>
              </Group></Table.Td>
            </Table.Tr>
          ))}
          {(q.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('articles.create') : t('common.edit')} size="xl">
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <Group grow>
            <TextInput label={t('articles.titleCol')} required {...form.getInputProps('Title')} />
            <TextInput label={t('articles.category')} placeholder="Windows / iOS / FAQ" {...form.getInputProps('Category')} />
          </Group>
          <Group grow>
            <Select label={t('articles.lang')} data={[{ value: '', label: t('articles.anyLang') }, { value: 'zh-CN', label: '中文' }, { value: 'en', label: 'English' }]} allowDeselect={false} {...form.getInputProps('Lang')} />
            <NumberInput label={t('articles.sort')} {...form.getInputProps('Sort')} />
            <Switch label={t('articles.published')} mt="xl" {...form.getInputProps('Published', { type: 'checkbox' })} />
          </Group>
          <Textarea label={t('articles.body')} description={t('articles.bodyHint')} autosize minRows={12} maxRows={30} ff="monospace" {...form.getInputProps('Body')} />
          <Group justify="flex-end"><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
