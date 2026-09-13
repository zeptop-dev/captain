import { Badge, Button, Card, Group, SegmentedControl, Table, Text, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Withdrawal { id: number; user_id: number; email: string; amount_cents: number; method: string; account: string; status: string; note: string; created_at: string; updated_at: string }
const colors: Record<string, string> = { pending: 'orange', paid: 'teal', rejected: 'red' }

// Commission payout requests: pay outside Captain, then mark paid; rejecting
// returns the amount to the user's commission balance.
export default function WithdrawalsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [status, setStatus] = useState('pending')
  const q = useQuery({ queryKey: ['withdrawals', status], queryFn: () => api.get<Withdrawal[]>(`/api/admin/withdrawals?status=${status === 'all' ? '' : status}`), refetchInterval: 30_000 })
  const [note, setNote] = useState('')
  const decide = useMutation({ mutationFn: (v: { id: number; status: string }) => api.post(`/api/admin/withdrawals/${v.id}/status`, { Status: v.status, Note: note }), onSuccess: () => { toast.ok(t('common.saved')); setNote(''); qc.invalidateQueries({ queryKey: ['withdrawals'] }) }, onError: toast.err })
  const ask = (w: Withdrawal, s: string) => modals.openConfirmModal({
    title: s === 'paid' ? t('withdrawals.markPaid') : t('withdrawals.reject'),
    children: <><Text size="sm">{w.email} · {money(w.amount_cents)} · {w.method} {w.account}</Text><TextInput mt="sm" label={t('withdrawals.note')} placeholder={s === 'paid' ? t('withdrawals.noteHintPaid') : t('withdrawals.noteHintReject')} onChange={(e) => setNote(e.currentTarget.value)} /></>,
    labels: { confirm: s === 'paid' ? t('withdrawals.markPaid') : t('withdrawals.reject'), cancel: t('common.cancel') }, confirmProps: { color: s === 'paid' ? 'teal' : 'red' },
    onConfirm: () => decide.mutate({ id: w.id, status: s }),
  })
  return (
    <>
      <PageHeader title={t('withdrawals.title')} subtitle={t('withdrawals.subtitle')} actions={<SegmentedControl value={status} onChange={setStatus} data={[{ value: 'pending', label: t('withdrawals.status.pending') }, { value: 'paid', label: t('withdrawals.status.paid') }, { value: 'rejected', label: t('withdrawals.status.rejected') }, { value: 'all', label: t('common.all') }]} />} />
      <Card p={0}><Table.ScrollContainer minWidth={720}><Table>
        <Table.Thead><Table.Tr><Table.Th>#</Table.Th><Table.Th>{t('withdrawals.user')}</Table.Th><Table.Th>{t('withdrawals.amount')}</Table.Th><Table.Th>{t('withdrawals.payTo')}</Table.Th><Table.Th>{t('withdrawals.statusCol')}</Table.Th><Table.Th>{t('withdrawals.when')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((w) => (
            <Table.Tr key={w.id}>
              <Table.Td><Text size="sm" c="dimmed">{w.id}</Text></Table.Td>
              <Table.Td><Text size="sm">{w.email}</Text></Table.Td>
              <Table.Td><Text size="sm" fw={700}>{money(w.amount_cents)}</Text></Table.Td>
              <Table.Td><Text size="sm">{w.method}</Text><Text size="xs" ff="monospace" style={{ wordBreak: 'break-all' }}>{w.account}</Text></Table.Td>
              <Table.Td><Badge color={colors[w.status]}>{t(`withdrawals.status.${w.status}`)}</Badge>{w.note && <Text size="xs" c="dimmed">{w.note}</Text>}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{when(w.created_at)}</Text></Table.Td>
              <Table.Td>{w.status === 'pending' && <Group gap={4} justify="flex-end" wrap="nowrap"><Button size="compact-xs" color="teal" onClick={() => ask(w, 'paid')}>{t('withdrawals.markPaid')}</Button><Button size="compact-xs" variant="light" color="red" onClick={() => ask(w, 'rejected')}>{t('withdrawals.reject')}</Button></Group>}</Table.Td>
            </Table.Tr>
          ))}
          {(q.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Table.ScrollContainer></Card>
    </>
  )
}
