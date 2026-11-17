import { Badge, Button, Card, Checkbox, Group, Modal, MultiSelect, NumberInput, Pagination, Progress, Select, Stack, Table, Text, TextInput } from '@mantine/core'
import { useDebouncedValue, useLocalStorage } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Page, type Plan, type UserRow } from '../lib/api'
import { bytes, money, when } from '../lib/format'
import { toast } from '../lib/notify'

type Filters = { q: string; status: string; access: string; group_id: string; plan_id: string; sort: string; direction: string; per_page: string; from: string; to: string }
const defaults: Filters = { q: '', status: '', access: '', group_id: '', plan_id: '', sort: 'id', direction: 'desc', per_page: '50', from: '', to: '' }
type BulkRow = { id: number; email: string; outcome: string; expires_at?: number }
type BulkJob = { id: string; done: boolean; rows: BulkRow[]; expires_at: string }
const actions = ['ban', 'unban', 'extend', 'reset_usage', 'rotate_subscription', 'delete'] as const

export function UserDirectory({ onOpen, canManage, plans }: { onOpen: (u: UserRow) => void; canManage: boolean; plans: Plan[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [saved, setSaved] = useLocalStorage<Filters>({ key: 'captain.user-filters.v1', defaultValue: defaults })
  const filters = { ...defaults, ...saved, group_id: '' } // Ignore the retired persisted group filter.
  const [columns, setColumns] = useLocalStorage<string[]>({ key: 'captain.user-columns.v1', defaultValue: ['plan', 'usage', 'expires', 'balance', 'status'] })
  const [search] = useDebouncedValue(filters.q, 300)
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<number[]>([])
  const set = (key: keyof Filters, value: string | null) => { setSaved({ ...filters, [key]: value ?? '' }); setPage(1); setSelected([]) }
  const params = new URLSearchParams({ ...filters, q: search, page: String(page) })
  if (filters.from) params.set('expires_from', String(Math.floor(new Date(`${filters.from}T00:00:00`).getTime() / 1000)))
  if (filters.to) params.set('expires_to', String(Math.floor(new Date(`${filters.to}T23:59:59`).getTime() / 1000)))
  const q = useQuery({ queryKey: ['users', params.toString()], queryFn: () => api.get<Page<UserRow>>(`/api/admin/users?${params}`) })
  const [opened, setOpened] = useState(false)
  const [action, setAction] = useState<string>('ban')
  const [plan, setPlan] = useState<string | null>(null)
  const [days, setDays] = useState<string | number>(30)
  const [job, setJob] = useState<BulkJob | null>(null)
  const preview = useMutation({ mutationFn: () => api.post<BulkJob>('/api/admin/users/bulk/preview', { ids: selected, action, plan_id: Number(plan), days: Number(days) }), onSuccess: setJob, onError: toast.err })
  const execute = useMutation({ mutationFn: () => api.post<BulkJob>(`/api/admin/users/bulk/jobs/${job!.id}`, { confirm: true }), onSuccess: (r) => { setJob(r); setSelected([]); qc.invalidateQueries({ queryKey: ['users'] }) }, onError: toast.err })
  const items = q.data?.items ?? []
  const pageSelected = items.length > 0 && items.every(u => selected.includes(u.id))
  const toggle = (id: number) => setSelected(s => s.includes(id) ? s.filter(x => x !== id) : [...s, id].slice(0, 200))
  const pages = Math.max(1, Math.ceil((q.data?.total ?? 0) / Number(filters.per_page)))
  const labels = (keys: string[], prefix: string) => keys.map(value => ({ value, label: t(`${prefix}.${value}`) }))
  return <Card p="md">
    <Stack gap="sm">
      <Group align="flex-start">
        <TextInput label={t('users.email')} value={filters.q} onChange={e => set('q', e.currentTarget.value)} style={{ flex: '1 1 220px' }} />
        <Select label={t('users.status')} clearable data={labels(['active', 'banned'], 'users')} value={filters.status || null} onChange={v => set('status', v)} w={160} />
        <Select label={t('directory.access')} clearable data={['usable', 'expired', 'exhausted', 'none'].map(value => ({ value, label: t(`directory.${value}`) }))} value={filters.access || null} onChange={v => set('access', v)} w={190} />

        <Select label={t('users.plan')} clearable searchable value={filters.plan_id || null} onChange={v => set('plan_id', v)} data={plans.map(p => ({ value: String(p.ID), label: p.Name }))} w={190} />
      </Group>
      <Group align="flex-start">
        <TextInput type="date" label={t('directory.from')} value={filters.from} onChange={e => set('from', e.currentTarget.value)} />
        <TextInput type="date" label={t('directory.to')} value={filters.to} onChange={e => set('to', e.currentTarget.value)} />
        <Select label={t('directory.sort')} allowDeselect={false} value={filters.sort} onChange={v => set('sort', v)} data={['id', 'email', 'balance', 'created', 'expires', 'usage'].map(value => ({ value, label: t(`directory.sorts.${value}`) }))} w={150} />
        <Select label={t('directory.direction')} allowDeselect={false} value={filters.direction} onChange={v => set('direction', v)} data={['desc', 'asc'].map(value => ({ value, label: t(`directory.${value}`) }))} w={150} />
        <Select label={t('directory.pageSize')} allowDeselect={false} value={filters.per_page} onChange={v => set('per_page', v)} data={['25', '50', '100']} w={100} />
        <MultiSelect label={t('directory.columns')} value={columns} onChange={setColumns} data={labels(['plan', 'usage', 'expires', 'balance', 'status'], 'users')} style={{ flex: '1 1 230px' }} />
      </Group>
      <Group justify="space-between">
        <Text size="sm" c="dimmed">{t('common.total', { count: q.data?.total ?? 0 })}</Text>
        <Group><Button variant="subtle" size="xs" onClick={() => { setSaved(defaults); setSelected([]); setPage(1) }}>{t('directory.clear')}</Button>{canManage && <Button size="xs" disabled={!selected.length} onClick={() => { setJob(null); setOpened(true) }}>{t('directory.bulk', { count: selected.length })}</Button>}</Group>
      </Group>
      {q.isError && <Text c="red" size="sm">{t('directory.loadFailed')}</Text>}
      <Table.ScrollContainer minWidth={720}>
        <Table highlightOnHover>
          <Table.Thead><Table.Tr>
            {canManage && <Table.Th><Checkbox aria-label={t('directory.selectPage')} checked={pageSelected} indeterminate={!pageSelected && items.some(u => selected.includes(u.id))} onChange={() => setSelected(s => pageSelected ? s.filter(id => !items.some(u => u.id === id)) : [...new Set([...s, ...items.map(u => u.id)])].slice(0, 200))} /></Table.Th>}
            <Table.Th>{t('users.email')}</Table.Th>{columns.map(c => <Table.Th key={c}>{t(`users.${c}`)}</Table.Th>)}
          </Table.Tr></Table.Thead>
          <Table.Tbody>{items.map(u => <Table.Tr key={u.id} onClick={() => onOpen(u)} style={{ cursor: 'pointer' }}>
            {canManage && <Table.Td onClick={e => e.stopPropagation()}><Checkbox aria-label={u.email} checked={selected.includes(u.id)} disabled={!selected.includes(u.id) && selected.length >= 200} onChange={() => toggle(u.id)} /></Table.Td>}
            <Table.Td><Text fw={600}>{u.email}</Text><Text size="xs" c="dimmed">#{u.id}</Text></Table.Td>
            {columns.map(c => <Table.Td key={c}>
              {c === 'plan' && (u.plan_name ? <Group gap={4}><Badge color={u.sub_usable ? 'teal' : 'orange'}>{u.plan_name}</Badge>{u.sub_count > 1 && <Badge color="gray">+{u.sub_count - 1}</Badge>}</Group> : t('users.noPlan'))}
              {c === 'usage' && (u.plan_name ? <><Text size="xs">{bytes(u.used_bytes)}{u.quota_bytes ? ` / ${bytes(u.quota_bytes)}` : ''}</Text>{u.quota_bytes > 0 && <Progress value={Math.min(100, u.used_bytes / u.quota_bytes * 100)} size="xs" />}</> : '—')}
              {c === 'expires' && (u.plan_name ? u.expires_at ? when(u.expires_at) : '∞' : '—')}
              {c === 'balance' && money(u.balance_cents)}
              {c === 'status' && <Badge color={u.status === 'active' ? 'teal' : 'red'}>{t(`users.${u.status}`)}</Badge>}
            </Table.Td>)}
          </Table.Tr>)}{items.length === 0 && <Table.Tr><Table.Td colSpan={columns.length + 2}><Text ta="center" c="dimmed" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}</Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {pages > 1 && <Group justify="center"><Pagination total={pages} value={page} onChange={setPage} /></Group>}
    </Stack>
    <Modal opened={opened} onClose={() => { if (!execute.isPending) setOpened(false) }} title={t('directory.bulk', { count: selected.length })} size="lg">
      <Stack>
        {!job ? <>
          <Text size="sm" c="dimmed">{t('directory.previewHint')}</Text>
          <Select label={t('directory.action')} value={action} onChange={v => setAction(v!)} allowDeselect={false} data={labels([...actions], 'directory.actions')} />

          {['extend', 'reset_usage'].includes(action) && <Select label={t('users.plan')} value={plan} onChange={setPlan} searchable data={plans.map(p => ({ value: String(p.ID), label: p.Name }))} />}
          {action === 'extend' && <NumberInput label={t('directory.days')} min={1} max={3650} value={days} onChange={setDays} />}
          {action === 'rotate_subscription' && <Text size="sm" c="orange">{t('directory.rotateHint')}</Text>}
          <Button loading={preview.isPending} disabled={['extend', 'reset_usage'].includes(action) && !plan} onClick={() => preview.mutate()}>{t('directory.preview')}</Button>
        </> : <>
          <Text size="sm">{t(`directory.actions.${action}`)} · {job.done ? t('directory.finished') : t('directory.validUntil', { time: when(job.expires_at) })}</Text>
          <Table.ScrollContainer minWidth={420}><Table><Table.Thead><Table.Tr><Table.Th>{t('users.email')}</Table.Th><Table.Th>{t('directory.result')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{job.rows.map(row => <Table.Tr key={row.id}><Table.Td>{row.email || `#${row.id}`}</Table.Td><Table.Td><Badge color={['ready', 'applied'].includes(row.outcome) ? 'teal' : 'orange'}>{t(`directory.results.${row.outcome}`)}</Badge></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
          {!job.done && <Group justify="flex-end"><Button variant="default" disabled={execute.isPending} onClick={() => setJob(null)}>{t('common.cancel')}</Button><Button color={action === 'delete' ? 'red' : undefined} loading={execute.isPending} disabled={!job.rows.some(r => r.outcome === 'ready')} onClick={() => execute.mutate()}>{t('directory.execute')}</Button></Group>}
        </>}
      </Stack>
    </Modal>
  </Card>
}
