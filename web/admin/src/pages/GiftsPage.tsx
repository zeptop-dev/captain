import { ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Select, Stack, Table, Text, TextInput } from '@mantine/core'
import { DateInput } from '@mantine/dates'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Plan } from '../lib/api'
import { bytes, money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'

interface Batch { batch: string; kind: string; total: number; redeemed: number; created_at: string }
interface Gift { id: number; code: string; batch: string; kind: string; value: number; plan_id: number | null; period_days: number; expires_at: string | null; redeemed_email?: string; redeemed_at: string | null }
type Values = { Batch: string; Prefix: string; Kind: string; Count: number; Value: number; PlanID: string | null; PeriodDays: number; ExpiresAt: Date | null }
const empty: Values = { Batch: '', Prefix: 'GIFT-', Kind: 'balance', Count: 10, Value: 10, PlanID: null, PeriodDays: 30, ExpiresAt: null }

// Gift / redeem codes are generated in batches; the codes are shown once
// after generation and can be copied as a list.
export default function GiftsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const batches = useQuery({ queryKey: ['gift-batches'], queryFn: () => api.get<Batch[]>('/api/admin/gifts/batches') })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const [batch, setBatch] = useState<string | null>(null)
  const codes = useQuery({ queryKey: ['gifts', batch], queryFn: () => api.get<{ items: Gift[] }>(`/api/admin/gifts?batch=${encodeURIComponent(batch ?? '')}&limit=1000`), enabled: batch !== null })
  const [creating, setCreating] = useState(false)
  const [generated, setGenerated] = useState<string[] | null>(null)
  const form = useForm<Values>({ initialValues: empty })
  const create = useMutation({
    mutationFn: (v: Values) => api.post<{ codes: string[] }>('/api/admin/gifts', {
      Batch: v.Batch, Prefix: v.Prefix, Kind: v.Kind, Count: v.Count, PlanID: v.PlanID ? Number(v.PlanID) : null, PeriodDays: v.PeriodDays, ExpiresAt: v.ExpiresAt,
      Value: v.Kind === 'balance' ? Math.round(v.Value * 100) : v.Kind === 'traffic' ? Math.round(v.Value * 1024 ** 3) : v.Value,
    }),
    onSuccess: (r) => { setGenerated(r.codes); setCreating(false); qc.invalidateQueries({ queryKey: ['gift-batches'] }) }, onError: toast.err,
  })
  const del = useMutation({ mutationFn: (b: string) => api.del(`/api/admin/gifts/batches/${encodeURIComponent(b)}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['gift-batches'] }); setBatch(null) }, onError: toast.err })
  const describe = (g: { kind: string; value: number; plan_id: number | null; period_days: number }) =>
    g.kind === 'balance' ? money(g.value) : g.kind === 'traffic' ? `+${bytes(g.value)}` : g.kind === 'days' ? t('gifts.days', { count: g.value }) : `${plans.data?.find((p) => p.ID === g.plan_id)?.Name ?? `#${g.plan_id}`} · ${g.period_days || '-'}d`
  const k = form.values.Kind
  return (
    <>
      <PageHeader title={t('gifts.title')} subtitle={t('gifts.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => { form.setValues(empty); setCreating(true) }}>{t('gifts.create')}</Button>} />
      <Card p={0}><Table highlightOnHover>
        <Table.Thead><Table.Tr><Table.Th>{t('gifts.batch')}</Table.Th><Table.Th>{t('gifts.kind')}</Table.Th><Table.Th>{t('gifts.redeemed')}</Table.Th><Table.Th>{t('gifts.createdAt')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(batches.data ?? []).map((b) => (
            <Table.Tr key={b.batch + b.kind} style={{ cursor: 'pointer' }} onClick={() => setBatch(b.batch)}>
              <Table.Td><Text size="sm" fw={600}>{b.batch}</Text></Table.Td>
              <Table.Td><Badge variant="light">{t(`gifts.kinds.${b.kind}`)}</Badge></Table.Td>
              <Table.Td><Text size="sm">{b.redeemed} / {b.total}</Text></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{when(b.created_at)}</Text></Table.Td>
              <Table.Td><ActionIcon variant="subtle" color="red" onClick={(e) => { e.stopPropagation(); modals.openConfirmModal({ title: t('gifts.deleteBatch'), children: <Text size="sm">{t('gifts.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(b.batch) }) }}><IconTrash size={16} /></ActionIcon></Table.Td>
            </Table.Tr>
          ))}
          {(batches.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={5}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>

      <Modal opened={batch !== null} onClose={() => setBatch(null)} title={batch ?? ''} size="lg">
        <Group justify="flex-end" mb="sm"><Copy value={(codes.data?.items ?? []).filter((g) => !g.redeemed_at).map((g) => g.code).join('\n')} /><Text size="xs" c="dimmed">{t('gifts.copyUnused')}</Text></Group>
        <Table.ScrollContainer minWidth={480} mah={420}><Table>
          <Table.Thead><Table.Tr><Table.Th>{t('gifts.code')}</Table.Th><Table.Th>{t('gifts.value')}</Table.Th><Table.Th>{t('gifts.status')}</Table.Th></Table.Tr></Table.Thead>
          <Table.Tbody>
            {(codes.data?.items ?? []).map((g) => (
              <Table.Tr key={g.id}>
                <Table.Td><Code>{g.code}</Code></Table.Td>
                <Table.Td><Text size="sm">{describe(g)}</Text></Table.Td>
                <Table.Td>{g.redeemed_at ? <Text size="xs">{g.redeemed_email} · {when(g.redeemed_at)}</Text> : g.expires_at && new Date(g.expires_at) < new Date() ? <Badge color="gray">{t('gifts.expired')}</Badge> : <Badge color="teal">{t('gifts.unused')}</Badge>}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table></Table.ScrollContainer>
      </Modal>

      <Modal opened={creating} onClose={() => setCreating(false)} title={t('gifts.create')}>
        <form onSubmit={form.onSubmit((v) => create.mutate(v))}><Stack>
          <Group grow><TextInput label={t('gifts.batch')} placeholder={t('gifts.batchHint')} {...form.getInputProps('Batch')} /><TextInput label={t('gifts.prefix')} {...form.getInputProps('Prefix')} /></Group>
          <Group grow>
            <Select label={t('gifts.kind')} data={['balance', 'plan', 'traffic', 'days'].map((v) => ({ value: v, label: t(`gifts.kinds.${v}`) }))} allowDeselect={false} {...form.getInputProps('Kind')} />
            <NumberInput label={t('gifts.count')} min={1} max={1000} {...form.getInputProps('Count')} />
          </Group>
          {k === 'balance' && <NumberInput label={t('gifts.amount')} min={0.01} step={1} decimalScale={2} {...form.getInputProps('Value')} />}
          {k === 'traffic' && <NumberInput label={t('gifts.trafficGB')} min={0.1} step={1} decimalScale={1} {...form.getInputProps('Value')} />}
          {k === 'days' && <NumberInput label={t('gifts.daysLabel')} min={1} {...form.getInputProps('Value')} />}
          {k === 'plan' && <Group grow>
            <Select label={t('gifts.plan')} data={(plans.data ?? []).map((p) => ({ value: String(p.ID), label: p.Name }))} required {...form.getInputProps('PlanID')} />
            <NumberInput label={t('gifts.periodDays')} min={0} {...form.getInputProps('PeriodDays')} />
          </Group>}
          <DateInput label={t('gifts.expires')} clearable {...form.getInputProps('ExpiresAt')} />
          <Group justify="flex-end"><Button type="submit" loading={create.isPending}>{t('gifts.generate')}</Button></Group>
        </Stack></form>
      </Modal>

      <Modal opened={generated !== null} onClose={() => setGenerated(null)} title={t('gifts.generated', { count: generated?.length ?? 0 })}>
        <Group justify="flex-end" mb="xs"><Copy value={(generated ?? []).join('\n')} /></Group>
        <Code block mah={360} style={{ overflowY: 'auto' }}>{(generated ?? []).join('\n')}</Code>
      </Modal>
    </>
  )
}
