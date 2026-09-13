import { Button, Card, Group, Text, TextInput } from '@mantine/core'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { bytes, money } from '../lib/format'
import { toast } from '../lib/notify'

// Gift / redeem code entry. The server answers with what the code did so the
// toast can say it in words.
export function RedeemCard() {
  const { t } = useTranslation()
  const { refresh } = useAuth()
  const [code, setCode] = useState('')
  const redeem = useMutation({
    mutationFn: () => api.post<{ kind: string; value: number; period_days: number }>('/api/portal/redeem', { Code: code }),
    onSuccess: (r) => {
      const what = r.kind === 'balance' ? t('redeem.gotBalance', { amount: money(r.value) }) : r.kind === 'plan' ? t('redeem.gotPlan') : r.kind === 'traffic' ? t('redeem.gotTraffic', { amount: bytes(r.value) }) : t('redeem.gotDays', { count: r.value })
      toast.ok(what); setCode(''); refresh()
    },
    onError: toast.err,
  })
  return (
    <Card>
      <Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('redeem.title')}</Text>
      <Text size="sm" c="dimmed" mt={4}>{t('redeem.hint')}</Text>
      <Group mt="sm" align="flex-end"><TextInput flex={1} placeholder="GIFT-XXXXXXXXXXXX" value={code} onChange={(e) => setCode(e.currentTarget.value.toUpperCase())} /><Button disabled={!code.trim()} loading={redeem.isPending} onClick={() => redeem.mutate()}>{t('redeem.submit')}</Button></Group>
    </Card>
  )
}
