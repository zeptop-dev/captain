import { Badge, Button, Card, Group, Progress, Table, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes, money, when } from '../lib/format'
import { toast } from '../lib/notify'

interface Row { user_id: number; email: string; plan_name: string; expires_at: string | null; quota_bytes: number; used_bytes: number; balance_cents: number }

// Renewal view: everyone with an active plan, soonest expiry first, with
// one-click extensions and a usage reset — for batch renewals.
export function RenewalsPanel() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['renewals'], queryFn: () => api.get<Row[]>('/api/admin/renewals') })
  const done = () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['renewals'] }); qc.invalidateQueries({ queryKey: ['users'] }) }
  const extend = useMutation({ mutationFn: (v: { id: number; days: number }) => api.post(`/api/admin/users/${v.id}/subscription`, { AddDays: v.days }), onSuccess: done, onError: toast.err })
  const reset = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/users/${id}/subscription`, { ResetUsage: true }), onSuccess: done, onError: toast.err })
  const daysLeft = (iso: string | null) => (iso ? Math.ceil((new Date(iso).getTime() - Date.now()) / 86400000) : null)
  return (
    <Card p={0}><Table.ScrollContainer minWidth={760}><Table>
      <Table.Thead><Table.Tr><Table.Th>{t('users.email')}</Table.Th><Table.Th>{t('users.plan')}</Table.Th><Table.Th>{t('users.usage')}</Table.Th><Table.Th>{t('users.expires')}</Table.Th><Table.Th>{t('users.balance')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
      <Table.Tbody>
        {(q.data ?? []).map((r) => { const d = daysLeft(r.expires_at); return (
          <Table.Tr key={r.user_id}>
            <Table.Td><Text size="sm" fw={600}>{r.email}</Text></Table.Td>
            <Table.Td><Badge variant="light">{r.plan_name}</Badge></Table.Td>
            <Table.Td w={160}><Text size="xs">{bytes(r.used_bytes)}{r.quota_bytes ? ` / ${bytes(r.quota_bytes)}` : ''}</Text>{r.quota_bytes ? <Progress value={Math.min(100, (r.used_bytes / r.quota_bytes) * 100)} size="xs" mt={4} /> : null}</Table.Td>
            <Table.Td>{r.expires_at ? <Text size="sm" c={d !== null && d <= 0 ? 'red' : d !== null && d <= 7 ? 'orange' : undefined}>{when(r.expires_at).split(',')[0]}{d !== null && <Text span size="xs" c="dimmed"> ({d <= 0 ? t('renewals.lapsed') : t('renewals.days', { count: d })})</Text>}</Text> : '∞'}</Table.Td>
            <Table.Td>{money(r.balance_cents)}</Table.Td>
            <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
              {[30, 60, 90].map((n) => <Button key={n} size="compact-xs" variant="light" onClick={() => extend.mutate({ id: r.user_id, days: n })}>+{n}</Button>)}
              <Button size="compact-xs" variant="subtle" color="gray" onClick={() => reset.mutate(r.user_id)}>{t('renewals.resetUsage')}</Button>
            </Group></Table.Td>
          </Table.Tr>
        ) })}
        {(q.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
      </Table.Tbody>
    </Table></Table.ScrollContainer></Card>
  )
}
