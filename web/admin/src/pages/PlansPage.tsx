import { ActionIcon, Badge, Button, Card, Group, Modal, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Group as UGroup, type Plan } from '../lib/api'
import { bytes, money } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

type Values = { Name: string; PriceCents: number; PeriodDays: number; ResetDays: number; ResetMode: string; Prices: string; QuotaGiB: number; DeviceLimit: number; SpeedLimitMbps: number; GroupID: string; Enabled: boolean }
const empty: Values = { Name: '', PriceCents: 1000, PeriodDays: 30, ResetDays: 0, ResetMode: '', Prices: '', QuotaGiB: 100, DeviceLimit: 0, SpeedLimitMbps: 0, GroupID: '', Enabled: true }
// Extra periods are typed as "days:cents" lines, e.g. "90:2700".
const parsePrices = (s: string) => s.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => { const [d, c] = l.split(':'); return { period_days: Number(d), price_cents: Number(c) } }).filter((p) => p.period_days > 0 && p.price_cents >= 0)
const formatPrices = (p: { period_days: number; price_cents: number }[] | null) => (p ?? []).map((x) => `${x.period_days}:${x.price_cents}`).join('\n')

export default function PlansPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const [editing, setEditing] = useState<Plan | 'new' | null>(null)
  const form = useForm<Values>({ initialValues: empty })
  const payload = (v: Values) => ({ Name: v.Name, PriceCents: v.PriceCents, PeriodDays: v.PeriodDays, ResetDays: v.ResetMode === 'days' ? v.ResetDays : 0, ResetMode: v.ResetMode, Prices: parsePrices(v.Prices), QuotaBytes: Math.round(v.QuotaGiB * 2 ** 30), DeviceLimit: v.DeviceLimit, SpeedLimitMbps: v.SpeedLimitMbps, GroupID: v.GroupID ? Number(v.GroupID) : null, Enabled: v.Enabled })
  const save = useMutation({ mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/plans', payload(v)) : api.patch(`/api/admin/plans/${(editing as Plan).ID}`, payload(v)), onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); qc.invalidateQueries({ queryKey: ['plans'] }) }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/plans/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['plans'] }) }, onError: toast.err })
  const openEdit = (p: Plan | 'new') => { form.setValues(p === 'new' ? empty : { Name: p.Name, PriceCents: p.PriceCents, PeriodDays: p.PeriodDays, ResetDays: p.ResetDays ?? 0, ResetMode: p.ResetMode || (p.ResetDays ? 'days' : ''), Prices: formatPrices(p.Prices), QuotaGiB: p.QuotaBytes / 2 ** 30, DeviceLimit: p.DeviceLimit, SpeedLimitMbps: p.SpeedLimitMbps, GroupID: p.GroupID ? String(p.GroupID) : '', Enabled: p.Enabled }); setEditing(p) }
  return (
    <>
      <PageHeader title={t('plans.title')} subtitle={t('plans.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => openEdit('new')}>{t('plans.create')}</Button>} />
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('plans.name')}</Table.Th><Table.Th>{t('plans.price')}</Table.Th><Table.Th>{t('plans.period')}</Table.Th><Table.Th>{t('plans.quota')}</Table.Th><Table.Th>{t('plans.group')}</Table.Th><Table.Th>{t('plans.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((p) => (
            <Table.Tr key={p.ID}>
              <Table.Td><Text fw={600}>{p.Name}</Text></Table.Td>
              <Table.Td>{money(p.PriceCents)}</Table.Td>
              <Table.Td>{p.PeriodDays || '∞'}</Table.Td>
              <Table.Td>{p.QuotaBytes ? bytes(p.QuotaBytes) : '∞'}</Table.Td>
              <Table.Td>{p.GroupID ? groups.data?.find((g) => g.ID === p.GroupID)?.Name ?? p.GroupID : '—'}</Table.Td>
              <Table.Td>{p.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
              <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => openEdit(p)}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(p.ID) })}><IconTrash size={16} /></ActionIcon></Group></Table.Td>
            </Table.Tr>
          ))}
          {q.data?.length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('plans.create') : t('common.edit')}>
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <TextInput label={t('plans.name')} required {...form.getInputProps('Name')} />
          <Group grow><NumberInput label={t('plans.price')} min={0} {...form.getInputProps('PriceCents')} /><NumberInput label={t('plans.period')} description={t('plans.periodHint')} min={0} {...form.getInputProps('PeriodDays')} /></Group>
          <Group grow align="flex-start">
            <Select label={t('plans.resetMode')} data={[{ value: '', label: t('plans.resetNever') }, { value: 'days', label: t('plans.resetDaysMode') }, { value: 'monthly', label: t('plans.resetMonthly') }, { value: 'yearly', label: t('plans.resetYearly') }]} allowDeselect={false} {...form.getInputProps('ResetMode')} />
            {form.values.ResetMode === 'days' && <NumberInput label={t('plans.resetDays')} description={t('plans.resetHint')} min={1} {...form.getInputProps('ResetDays')} />}
          </Group>
          <Textarea label={t('plans.prices')} description={t('plans.pricesHint')} placeholder={'90:2700\n365:9900'} autosize minRows={2} {...form.getInputProps('Prices')} />
          <Group grow><NumberInput label={t('plans.quota')} description={t('plans.quotaHint')} min={0} decimalScale={2} {...form.getInputProps('QuotaGiB')} /><NumberInput label={t('plans.deviceLimit')} description={t('plans.deviceHint')} min={0} {...form.getInputProps('DeviceLimit')} /><NumberInput label={t('plans.speedLimit')} min={0} {...form.getInputProps('SpeedLimitMbps')} /></Group>
          <Select label={t('plans.group')} data={[{ value: '', label: t('common.none') }, ...(groups.data ?? []).map((g) => ({ value: String(g.ID), label: g.Name }))]} allowDeselect={false} {...form.getInputProps('GroupID')} />
          <Switch label={t('plans.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
