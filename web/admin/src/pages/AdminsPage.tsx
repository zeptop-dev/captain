import { ActionIcon, Badge, Button, Card, Group, Modal, PasswordInput, Select, Stack, Table, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Staff { id: number; email: string; role: string; status: string; created_at: string }
const roleColor: Record<string, string> = { admin: 'red', operator: 'blue', support: 'teal' }

// Console accounts and their roles. The last admin cannot be demoted,
// disabled or deleted; the server enforces that too.
export default function AdminsPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['admins'], queryFn: () => api.get<Staff[]>('/api/admin/admins') })
  const [editing, setEditing] = useState<Staff | 'new' | null>(null)
  const form = useForm({ initialValues: { Email: '', Password: '', Role: 'operator', Status: 'active' } })
  const done = () => { toast.ok(t('common.saved')); setEditing(null); qc.invalidateQueries({ queryKey: ['admins'] }) }
  const create = useMutation({ mutationFn: (v: typeof form.values) => api.post('/api/admin/admins', { Email: v.Email, Password: v.Password, Role: v.Role }), onSuccess: done, onError: toast.err })
  const update = useMutation({ mutationFn: (v: typeof form.values) => api.patch(`/api/admin/admins/${(editing as Staff).id}`, { Role: v.Role, Status: v.Status, Password: v.Password }), onSuccess: done, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/admins/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['admins'] }) }, onError: toast.err })
  const open = (s: Staff | 'new') => { form.setValues(s === 'new' ? { Email: '', Password: '', Role: 'operator', Status: 'active' } : { Email: s.email, Password: '', Role: s.role, Status: s.status }); setEditing(s) }
  const roles = ['admin', 'operator', 'support'].map((r) => ({ value: r, label: t(`admins.roles.${r}`) }))
  return (
    <>
      <PageHeader title={t('admins.title')} subtitle={t('admins.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => open('new')}>{t('admins.create')}</Button>} />
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('admins.email')}</Table.Th><Table.Th>{t('admins.role')}</Table.Th><Table.Th>{t('admins.status')}</Table.Th><Table.Th>{t('admins.created')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((s) => (
            <Table.Tr key={s.id}>
              <Table.Td><Text size="sm" fw={600}>{s.email}</Text>{s.id === me?.id && <Text size="xs" c="dimmed">{t('admins.you')}</Text>}</Table.Td>
              <Table.Td><Badge color={roleColor[s.role]} variant="light">{t(`admins.roles.${s.role}`)}</Badge></Table.Td>
              <Table.Td><Badge color={s.status === 'active' ? 'teal' : 'gray'}>{s.status === 'active' ? t('common.enabled') : t('common.disabled')}</Badge></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{when(s.created_at)}</Text></Table.Td>
              <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
                <ActionIcon variant="subtle" color="gray" onClick={() => open(s)}><IconPencil size={16} /></ActionIcon>
                {s.id !== me?.id && <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(s.id) })}><IconTrash size={16} /></ActionIcon>}
              </Group></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table></Card>
      <Card mt="md"><Text size="sm" fw={600} mb={4}>{t('admins.matrix')}</Text><Text size="xs" c="dimmed" style={{ whiteSpace: 'pre-line' }}>{t('admins.matrixText')}</Text></Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('admins.create') : t('common.edit')}>
        <form onSubmit={form.onSubmit((v) => (editing === 'new' ? create : update).mutate(v))}><Stack>
          <TextInput label={t('admins.email')} required disabled={editing !== 'new'} {...form.getInputProps('Email')} />
          <PasswordInput label={t('admins.password')} placeholder={editing === 'new' ? '' : t('mail.keep')} required={editing === 'new'} {...form.getInputProps('Password')} />
          <Select label={t('admins.role')} data={roles} allowDeselect={false} {...form.getInputProps('Role')} />
          {editing !== 'new' && <Select label={t('admins.status')} data={[{ value: 'active', label: t('common.enabled') }, { value: 'banned', label: t('common.disabled') }]} allowDeselect={false} {...form.getInputProps('Status')} />}
          <Group justify="flex-end"><Button type="submit" loading={create.isPending || update.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
