import { Badge, Button, Progress, Select, Stack, Table, Text, Title } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes, when } from '../lib/format'
import { toast } from '../lib/notify'
import { SubAdjust } from './SubAdjust'

export interface UserSub { id: number; plan_id: number; plan_name: string; status: 'active' | 'queued'; starts_at: string; expires_at: string | null; reset_at: string | null; quota_bytes: number; used_bytes: number; usable: boolean; period_days: number }

// Every plan a user holds (active ones first, then queued), with the
// per-subscription adjustments below aimed at the selected row.
export function UserSubs({ userID, subs, onDone }: { userID: number; subs: UserSub[]; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [pick, setPick] = useState<string | null>(null)
  const cancel = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/users/${userID}/subscriptions/${id}`), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['user', userID] }); qc.invalidateQueries({ queryKey: ['users'] }) }, onError: toast.err })
  const active = subs.filter((s) => s.status === 'active')
  const selected = pick ?? (active[0] ? String(active[0].id) : null)
  return (
    <Stack gap="sm">
      <Title order={6}>{t('userSubs.title')}</Title>
      {subs.length === 0 ? <Text size="sm" c="dimmed">{t('users.noPlan')}</Text> : (
        <Table fz="xs" verticalSpacing={4}><Table.Tbody>
          {subs.map((s) => (
            <Table.Tr key={s.id}>
              <Table.Td><Text size="xs" fw={600}>{s.plan_name}</Text><Text size="xs" c="dimmed">#{s.id}</Text></Table.Td>
              <Table.Td><Badge size="xs" variant="light" color={s.status === 'queued' ? 'gray' : s.usable ? 'teal' : 'orange'}>{s.status === 'queued' ? t('userSubs.queued') : s.usable ? t('userSubs.active') : t('userSubs.lapsed')}</Badge></Table.Td>
              <Table.Td w={160}>{s.status === 'queued' ? <Text size="xs" c="dimmed">{s.period_days ? t('userSubs.queuedDays', { days: s.period_days }) : t('userSubs.queuedForever')}</Text> : <><Text size="xs">{bytes(s.used_bytes)}{s.quota_bytes ? ` / ${bytes(s.quota_bytes)}` : ''}</Text>{s.quota_bytes ? <Progress size="xs" value={Math.min(100, (s.used_bytes / s.quota_bytes) * 100)} /> : null}</>}</Table.Td>
              <Table.Td><Text size="xs">{s.status === 'queued' ? '—' : s.expires_at ? when(s.expires_at) : '∞'}</Text></Table.Td>
              <Table.Td>{s.status === 'queued' && <Button size="compact-xs" variant="subtle" color="gray" loading={cancel.isPending} onClick={() => cancel.mutate(s.id)}>{t('common.cancel')}</Button>}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
      {active.length > 1 && <Select size="xs" label={t('userSubs.adjustTarget')} data={active.map((s) => ({ value: String(s.id), label: `${s.plan_name} #${s.id}` }))} value={selected} onChange={setPick} allowDeselect={false} />}
      <SubAdjust userID={userID} hasPlan={active.length > 0} subID={selected ? Number(selected) : 0} onDone={onDone} />
    </Stack>
  )
}
