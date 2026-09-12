import { ActionIcon, Badge, Button, Card, Code, Drawer, Group, Modal, NumberInput, Pagination, PasswordInput, Progress, Select, Stack, Table, Text, TextInput, Title, Divider } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDebouncedValue } from '@mantine/hooks'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconSearch, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Group as UGroup, type OnlineDevice, type Page, type Plan, type UserRow } from '../lib/api'
import { bytes, money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'

export default function UsersPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [debounced] = useDebouncedValue(search, 300)
  const [page, setPage] = useState(1)
  const q = useQuery({ queryKey: ['users', debounced, page], queryFn: () => api.get<Page<UserRow>>(`/api/admin/users?q=${encodeURIComponent(debounced)}&page=${page}`) })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const [sel, setSel] = useState<UserRow | null>(null)
  const [creating, setCreating] = useState(false)
  const invalidate = () => qc.invalidateQueries({ queryKey: ['users'] })

  const createForm = useForm({ initialValues: { Email: '', Password: '' } })
  const create = useMutation({ mutationFn: (v: typeof createForm.values) => api.post('/api/admin/users', v), onSuccess: () => { toast.ok(t('common.saved')); setCreating(false); createForm.reset(); invalidate() }, onError: toast.err })

  const editForm = useForm({ initialValues: { Status: 'active', GroupID: '', Password: '' } })
  const update = useMutation({ mutationFn: (v: typeof editForm.values) => api.patch(`/api/admin/users/${sel!.id}`, { Status: v.Status, GroupID: v.GroupID ? Number(v.GroupID) : null, Password: v.Password }), onSuccess: () => { toast.ok(t('common.saved')); invalidate() }, onError: toast.err })
  const detail = useQuery({ queryKey: ['user', sel?.id], queryFn: () => api.get<{ devices: OnlineDevice[] }>(`/api/admin/users/${sel!.id}`), enabled: sel !== null, refetchInterval: 15000 })
  const [grantPlan, setGrantPlan] = useState<string | null>(null)
  const grant = useMutation({ mutationFn: () => api.post(`/api/admin/users/${sel!.id}/grant`, { PlanID: Number(grantPlan) }), onSuccess: () => { toast.ok(t('common.saved')); invalidate(); setSel(null) }, onError: toast.err })
  const [delta, setDelta] = useState<number | string>(0)
  const topUp = useMutation({ mutationFn: () => api.post(`/api/admin/users/${sel!.id}/balance`, { DeltaCents: Number(delta) }), onSuccess: () => { toast.ok(t('common.saved')); invalidate(); setSel(null) }, onError: toast.err })
  const rotate = useMutation({ mutationFn: () => api.post<{ sub_token: string; sub_url: string }>(`/api/admin/users/${sel!.id}/rotate-token`), onSuccess: (r) => { toast.ok(t('common.saved')); setSel({ ...sel!, sub_token: r.sub_token, sub_url: r.sub_url }); invalidate() }, onError: toast.err })
  const del = useMutation({ mutationFn: () => api.del(`/api/admin/users/${sel!.id}`), onSuccess: () => { toast.ok(t('common.deleted')); setSel(null); invalidate() }, onError: toast.err })

  const open = (u: UserRow) => { setSel(u); editForm.setValues({ Status: u.status, GroupID: u.group_id ? String(u.group_id) : '', Password: '' }); setGrantPlan(null); setDelta(0) }
  const subURL = sel ? (sel.sub_url || `${window.location.origin}/sub/${sel.sub_token}`) : ''
  const pages = q.data ? Math.max(1, Math.ceil(q.data.total / q.data.per_page)) : 1

  return (
    <>
      <PageHeader title={t('users.title')} subtitle={t('users.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>{t('users.create')}</Button>} />
      <Card p={0}>
        <Group p="md" pb="xs"><TextInput placeholder={t('users.filterPlaceholder')} leftSection={<IconSearch size={14} />} value={search} onChange={(e) => { setSearch(e.currentTarget.value); setPage(1) }} w={300} /><Text size="sm" c="dimmed">{t('common.total', { count: q.data?.total ?? 0 })}</Text></Group>
        <Table.ScrollContainer minWidth={760}>
          <Table>
            <Table.Thead><Table.Tr><Table.Th>{t('users.email')}</Table.Th><Table.Th>{t('users.plan')}</Table.Th><Table.Th>{t('users.usage')}</Table.Th><Table.Th>{t('users.expires')}</Table.Th><Table.Th>{t('users.balance')}</Table.Th><Table.Th>{t('users.status')}</Table.Th></Table.Tr></Table.Thead>
            <Table.Tbody>
              {(q.data?.items ?? []).map((u) => (
                <Table.Tr key={u.id} onClick={() => open(u)} style={{ cursor: 'pointer' }}>
                  <Table.Td><Text fw={600}>{u.email}</Text><Text size="xs" c="dimmed">#{u.id}</Text></Table.Td>
                  <Table.Td>{u.plan_name ? <Badge color={u.sub_usable ? 'teal' : 'orange'}>{u.plan_name}</Badge> : <Text size="sm" c="dimmed">{t('users.noPlan')}</Text>}</Table.Td>
                  <Table.Td w={180}>{u.plan_name ? <><Text size="xs">{bytes(u.used_bytes)}{u.quota_bytes ? ` / ${bytes(u.quota_bytes)}` : ''}</Text>{u.quota_bytes ? <Progress value={Math.min(100, (u.used_bytes / u.quota_bytes) * 100)} size="xs" mt={4} /> : null}</> : '—'}</Table.Td>
                  <Table.Td>{u.plan_name ? (u.expires_at ? when(u.expires_at) : '∞') : '—'}</Table.Td>
                  <Table.Td>{money(u.balance_cents)}</Table.Td>
                  <Table.Td>{u.status === 'active' ? <Badge color="teal">{t('users.active')}</Badge> : <Badge color="red">{t('users.banned')}</Badge>}</Table.Td>
                </Table.Tr>
              ))}
              {q.data?.items.length === 0 && <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
        {pages > 1 && <Group justify="center" p="md"><Pagination total={pages} value={page} onChange={setPage} /></Group>}
      </Card>

      <Modal opened={creating} onClose={() => setCreating(false)} title={t('users.create')}>
        <form onSubmit={createForm.onSubmit((v) => create.mutate(v))}><Stack>
          <TextInput label={t('users.email')} type="email" required {...createForm.getInputProps('Email')} />
          <PasswordInput label={t('users.password')} required minLength={8} {...createForm.getInputProps('Password')} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setCreating(false)}>{t('common.cancel')}</Button><Button type="submit" loading={create.isPending}>{t('common.create')}</Button></Group>
        </Stack></form>
      </Modal>

      <Drawer opened={sel !== null} onClose={() => setSel(null)} position="right" size="lg" title={sel?.email}>
        {sel && (
          <Stack gap="lg">
            <Group gap="xl">
              <div><Text size="xs" c="dimmed">{t('users.uuid')}</Text><Group gap={4}><Code>{sel.uuid}</Code><Copy value={sel.uuid} /></Group></div>
              <div><Text size="xs" c="dimmed">{t('users.createdAt')}</Text><Text size="sm">{when(sel.created_at)}</Text></div>
            </Group>
            <div>
              <Text size="xs" c="dimmed">{t('users.subUrl')}</Text>
              <Group gap={4} wrap="nowrap"><Code style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{subURL}</Code><Copy value={subURL} /></Group>
              <Button size="xs" variant="subtle" color="orange" mt={4} onClick={() => modals.openConfirmModal({ title: t('users.rotate'), children: <Text size="sm">{t('users.rotateHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => rotate.mutate() })}>{t('users.rotate')}</Button>
            </div>
            <div>
              <Text size="xs" c="dimmed">{t('users.devices')}</Text>
              {detail.data?.devices?.length ? <Group gap={6} mt={4}>{detail.data.devices.map((d) => <Badge key={d.ip} variant="light" color="teal" title={when(d.last_seen_at)}>{d.ip}</Badge>)}</Group> : <Text size="sm" c="dimmed">{t('users.noDevices')}</Text>}
            </div>
            <Divider />
            <form onSubmit={editForm.onSubmit((v) => update.mutate(v))}><Stack gap="sm">
              <Title order={6}>{t('common.edit')}</Title>
              <Group grow>
                <Select label={t('users.status')} data={[{ value: 'active', label: t('users.active') }, { value: 'banned', label: t('users.banned') }]} allowDeselect={false} {...editForm.getInputProps('Status')} />
                <Select label={t('users.group')} data={[{ value: '', label: t('common.none') }, ...(groups.data ?? []).map((g) => ({ value: String(g.ID), label: g.Name }))]} allowDeselect={false} {...editForm.getInputProps('GroupID')} />
              </Group>
              <PasswordInput label={t('users.newPassword')} {...editForm.getInputProps('Password')} />
              <Group justify="flex-end"><Button type="submit" size="xs" loading={update.isPending}>{t('common.save')}</Button></Group>
            </Stack></form>
            <Divider />
            <Stack gap="sm">
              <Title order={6}>{t('users.grant')}</Title>
              <Text size="xs" c="dimmed">{t('users.grantHint')}</Text>
              <Group align="flex-end"><Select flex={1} data={(plans.data ?? []).map((p) => ({ value: String(p.ID), label: `${p.Name} · ${money(p.PriceCents)}` }))} value={grantPlan} onChange={setGrantPlan} placeholder={t('users.plan')} /><Button size="xs" disabled={!grantPlan} loading={grant.isPending} onClick={() => grant.mutate()}>{t('users.grant')}</Button></Group>
            </Stack>
            <Stack gap="sm">
              <Title order={6}>{t('users.topUp')}</Title>
              <Text size="xs" c="dimmed">{t('users.topUpHint')} {t('users.balance')}: {money(sel.balance_cents)}</Text>
              <Group align="flex-end"><NumberInput flex={1} value={delta} onChange={setDelta} /><Button size="xs" disabled={!Number(delta)} loading={topUp.isPending} onClick={() => topUp.mutate()}>{t('common.save')}</Button></Group>
            </Stack>
            <Divider />
            <Group justify="flex-end"><Button color="red" variant="light" size="xs" leftSection={<IconTrash size={14} />} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate() })}>{t('common.delete')}</Button></Group>
          </Stack>
        )}
      </Drawer>
      <ActionIcon style={{ display: 'none' }} />
    </>
  )
}
