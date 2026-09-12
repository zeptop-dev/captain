import { ActionIcon, Badge, Button, Card, Code, Group, Modal, MultiSelect, NumberInput, Select, Stack, Switch, Table, Text, TextInput } from '@mantine/core'
import { DateInput } from '@mantine/dates'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Coupon, type Plan } from '../lib/api'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'

type Values = { Code: string; Name: string; Kind: string; Value: number; PlanIDs: string[]; MaxUses: number; PerUser: number; StartsAt: Date | null; ExpiresAt: Date | null; Enabled: boolean }
const empty: Values = { Code: '', Name: '', Kind: 'percent', Value: 10, PlanIDs: [], MaxUses: 0, PerUser: 1, StartsAt: null, ExpiresAt: null, Enabled: true }

export default function CouponsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['coupons'], queryFn: () => api.get<Coupon[]>('/api/admin/coupons') })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const [editing, setEditing] = useState<Coupon | 'new' | null>(null)
  const form = useForm<Values>({ initialValues: empty })
  const payload = (v: Values) => ({ Code: v.Code, Name: v.Name, Kind: v.Kind, Value: v.Value, PlanIDs: v.PlanIDs.map(Number), MaxUses: v.MaxUses, PerUser: v.PerUser, StartsAt: v.StartsAt, ExpiresAt: v.ExpiresAt, Enabled: v.Enabled })
  const save = useMutation({
    mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/coupons', payload(v)) : api.patch(`/api/admin/coupons/${(editing as Coupon).ID}`, payload(v)),
    onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); qc.invalidateQueries({ queryKey: ['coupons'] }) }, onError: toast.err,
  })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/coupons/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['coupons'] }) }, onError: toast.err })
  const open = (c: Coupon | 'new') => {
    form.setValues(c === 'new' ? empty : { Code: c.Code, Name: c.Name, Kind: c.Kind, Value: c.Value, PlanIDs: (c.PlanIDs ?? []).map(String), MaxUses: c.MaxUses, PerUser: c.PerUser, StartsAt: c.StartsAt ? new Date(c.StartsAt) : null, ExpiresAt: c.ExpiresAt ? new Date(c.ExpiresAt) : null, Enabled: c.Enabled })
    setEditing(c)
  }
  const label = (c: Coupon) => (c.Kind === 'percent' ? `-${c.Value}%` : `-${money(c.Value)}`)
  return (
    <>
      <PageHeader title={t('coupons.title')} subtitle={t('coupons.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => open('new')}>{t('coupons.create')}</Button>} />
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('coupons.code')}</Table.Th><Table.Th>{t('coupons.name')}</Table.Th><Table.Th>{t('coupons.discount')}</Table.Th><Table.Th>{t('coupons.uses')}</Table.Th><Table.Th>{t('coupons.validity')}</Table.Th><Table.Th>{t('common.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((c) => (
            <Table.Tr key={c.ID}>
              <Table.Td><Group gap={4}><Code>{c.Code}</Code><Copy value={c.Code} /></Group></Table.Td>
              <Table.Td>{c.Name}</Table.Td>
              <Table.Td><Badge color="grape">{label(c)}</Badge>{(c.PlanIDs ?? []).length > 0 && <Text size="xs" c="dimmed">{t('coupons.limitedPlans', { count: c.PlanIDs!.length })}</Text>}</Table.Td>
              <Table.Td><Text size="sm">{c.Used}{c.MaxUses ? ` / ${c.MaxUses}` : ''}</Text></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{c.StartsAt ? when(c.StartsAt).split(',')[0] : '…'} → {c.ExpiresAt ? when(c.ExpiresAt).split(',')[0] : '…'}</Text></Table.Td>
              <Table.Td><Badge color={c.Enabled ? 'teal' : 'gray'}>{c.Enabled ? t('common.enabled') : t('common.disabled')}</Badge></Table.Td>
              <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
                <ActionIcon variant="subtle" color="gray" onClick={() => open(c)}><IconPencil size={16} /></ActionIcon>
                <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(c.ID) })}><IconTrash size={16} /></ActionIcon>
              </Group></Table.Td>
            </Table.Tr>
          ))}
          {(q.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('coupons.create') : t('common.edit')}>
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <Group grow><TextInput label={t('coupons.code')} placeholder={t('coupons.codeHint')} {...form.getInputProps('Code')} /><TextInput label={t('coupons.name')} {...form.getInputProps('Name')} /></Group>
          <Group grow>
            <Select label={t('coupons.kind')} data={[{ value: 'percent', label: t('coupons.percent') }, { value: 'fixed', label: t('coupons.fixed') }]} allowDeselect={false} {...form.getInputProps('Kind')} />
            <NumberInput label={form.values.Kind === 'percent' ? t('coupons.valuePercent') : t('coupons.valueCents')} min={1} {...form.getInputProps('Value')} />
          </Group>
          <MultiSelect label={t('coupons.plans')} description={t('coupons.plansHint')} data={(plans.data ?? []).map((p) => ({ value: String(p.ID), label: p.Name }))} {...form.getInputProps('PlanIDs')} />
          <Group grow>
            <NumberInput label={t('coupons.maxUses')} description={t('coupons.zeroUnlimited')} min={0} {...form.getInputProps('MaxUses')} />
            <NumberInput label={t('coupons.perUser')} description={t('coupons.zeroUnlimited')} min={0} {...form.getInputProps('PerUser')} />
          </Group>
          <Group grow><DateInput label={t('coupons.startsAt')} clearable {...form.getInputProps('StartsAt')} /><DateInput label={t('coupons.expiresAt')} clearable {...form.getInputProps('ExpiresAt')} /></Group>
          <Switch label={t('common.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
