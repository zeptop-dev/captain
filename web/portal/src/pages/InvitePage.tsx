import { Badge, Button, Card, Code, Group, NumberInput, Select, SimpleGrid, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Invite, type Withdrawal } from '../lib/api'
import { when } from '../lib/format'
import { useAuth } from '../lib/auth'
import { money } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from '../components/Copy'

export default function InvitePage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['invite'], queryFn: () => api.get<Invite>('/api/portal/invite') })
  const [code, setCode] = useState('')
  const { refresh } = useAuth()
  const wds = useQuery({ queryKey: ['withdrawals'], queryFn: () => api.get<Withdrawal[]>('/api/portal/invite/withdrawals'), enabled: q.data?.payout === 'commission' })
  const [amount, setAmount] = useState<number | string>('')
  const [method, setMethod] = useState<string | null>(null)
  const [account, setAccount] = useState('')
  const done = () => { qc.invalidateQueries({ queryKey: ['invite'] }); qc.invalidateQueries({ queryKey: ['withdrawals'] }); refresh() }
  const transfer = useMutation({ mutationFn: () => api.post('/api/portal/invite/transfer', { AmountCents: Math.round(Number(amount) * 100) }), onSuccess: () => { toast.ok(t('invite.transferred')); setAmount(''); done() }, onError: toast.err })
  const withdraw = useMutation({ mutationFn: () => api.post('/api/portal/invite/withdraw', { AmountCents: Math.round(Number(amount) * 100), Method: method ?? '', Account: account }), onSuccess: () => { toast.ok(t('invite.requested')); setAmount(''); setAccount(''); done() }, onError: toast.err })
  const bind = useMutation({ mutationFn: () => api.post('/api/portal/invite/bind', { Code: code }), onSuccess: () => { toast.ok(t('invite.bound')); qc.invalidateQueries({ queryKey: ['invite'] }) }, onError: toast.err })
  const d = q.data
  if (!d) return null
  return (
    <Stack gap="lg">
      <Title order={2}>{t('invite.title')}</Title>
      <Card>
        <Text size="sm" c="dimmed">{d.enabled ? t('invite.hint', { percent: d.percent }) : t('invite.disabledHint')}{d.enabled && d.levels.length > 1 && ' ' + t('invite.levelsHint', { levels: d.levels.slice(1).map((p) => p + '%').join(' / ') })}</Text>
        <Group gap={4} mt="sm" wrap="nowrap"><Code style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>{d.url}</Code><Copy value={d.url} /></Group>
        <Text size="xs" c="dimmed" mt={4}>{t('invite.code')}: <b>{d.code}</b></Text>
      </Card>
      <SimpleGrid cols={2}>
        <Card><Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('invite.invited')}</Text><Text fz="xl" fw={700}>{d.invited}</Text></Card>
        <Card><Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('invite.earned')}</Text><Text fz="xl" fw={700}>{money(d.earned_cents)}</Text></Card>
      </SimpleGrid>
      {d.payout === 'commission' && (
        <Card>
          <Group justify="space-between"><Text size="sm" fw={600}>{t('invite.commission')}</Text><Text fz="xl" fw={700}>{money(d.commission_cents)}</Text></Group>
          <Text size="xs" c="dimmed">{t('invite.commissionHint', { min: money(d.min_withdraw_cents) })}</Text>
          <Group mt="sm" align="flex-end" wrap="wrap">
            <NumberInput label={t('invite.amount')} min={0.01} decimalScale={2} value={amount} onChange={setAmount} style={{ flex: 1, minWidth: 120 }} />
            {d.withdraw_methods.length > 0 && <Select label={t('invite.method')} data={d.withdraw_methods} value={method} onChange={setMethod} style={{ minWidth: 140 }} />}
            <TextInput label={t('invite.account')} placeholder={t('invite.accountHint')} value={account} onChange={(e) => setAccount(e.currentTarget.value)} style={{ flex: 2, minWidth: 160 }} />
          </Group>
          <Group mt="sm" justify="flex-end">
            <Button variant="light" disabled={!Number(amount)} loading={transfer.isPending} onClick={() => transfer.mutate()}>{t('invite.transfer')}</Button>
            <Button disabled={!Number(amount) || !account || (d.withdraw_methods.length > 0 && !method)} loading={withdraw.isPending} onClick={() => withdraw.mutate()}>{t('invite.withdraw')}</Button>
          </Group>
          {(wds.data ?? []).length > 0 && (
            <Table mt="md" fz="sm"><Table.Tbody>
              {wds.data!.map((w) => <Table.Tr key={w.id}><Table.Td>{when(w.created_at).split(',')[0]}</Table.Td><Table.Td>{money(w.amount_cents)}</Table.Td><Table.Td>{w.method} {w.account}</Table.Td><Table.Td><Badge color={w.status === 'paid' ? 'teal' : w.status === 'rejected' ? 'red' : 'orange'}>{t(`invite.wd.${w.status}`)}</Badge>{w.note && <Text size="xs" c="dimmed">{w.note}</Text>}</Table.Td></Table.Tr>)}
            </Table.Tbody></Table>
          )}
        </Card>
      )}
      {!d.invited_by && (
        <Card>
          <Text size="sm" fw={600}>{t('invite.bindTitle')}</Text>
          <Text size="xs" c="dimmed">{t('invite.bindHint')}</Text>
          <Group mt="sm" align="flex-end"><TextInput flex={1} placeholder="ABCD2345" value={code} onChange={(e) => setCode(e.currentTarget.value.toUpperCase())} /><Button disabled={!code} loading={bind.isPending} onClick={() => bind.mutate()}>{t('invite.bind')}</Button></Group>
        </Card>
      )}
    </Stack>
  )
}
