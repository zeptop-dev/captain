import { Button, Group, NumberInput, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

// Per-user subscription edits: extend days, override the quota, set the
// monthly reset day, zero the counters.
export function SubAdjust({ userID, hasPlan, onDone }: { userID: number; hasPlan: boolean; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [days, setDays] = useState<number | string>(30)
  const [quotaGB, setQuotaGB] = useState<number | string>('')
  const [resetDay, setResetDay] = useState<number | string>('')
  const call = useMutation({ mutationFn: (body: Record<string, unknown>) => api.post(`/api/admin/users/${userID}/subscription`, body), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['users'] }); qc.invalidateQueries({ queryKey: ['user', userID] }); onDone() }, onError: toast.err })
  if (!hasPlan) return null
  return (
    <Stack gap="sm">
      <Title order={6}>{t('subAdjust.title')}</Title>
      <Group align="flex-end" gap="xs">
        <NumberInput label={t('subAdjust.days')} w={110} value={days} onChange={setDays} />
        <Button size="xs" mb={2} variant="light" disabled={!Number(days)} onClick={() => call.mutate({ AddDays: Number(days) })}>{t('subAdjust.extend')}</Button>
        {[30, 60, 90].map((n) => <Button key={n} size="xs" mb={2} variant="subtle" onClick={() => call.mutate({ AddDays: n })}>+{n}</Button>)}
      </Group>
      <Group align="flex-end" gap="xs">
        <NumberInput label={t('subAdjust.quota')} description={t('subAdjust.quotaHint')} w={180} min={0} value={quotaGB} onChange={setQuotaGB} placeholder={t('subAdjust.planDefault')} />
        <Button size="xs" mb={2} variant="light" onClick={() => call.mutate({ QuotaOverride: quotaGB === '' ? 0 : Number(quotaGB) === 0 ? -1 : Math.round(Number(quotaGB) * 2 ** 30) })}>{t('common.save')}</Button>
        <NumberInput label={t('subAdjust.resetDay')} description={t('subAdjust.resetDayHint')} w={160} min={0} max={28} value={resetDay} onChange={setResetDay} placeholder={t('subAdjust.planDefault')} />
        <Button size="xs" mb={2} variant="light" onClick={() => call.mutate({ ResetDay: resetDay === '' ? 0 : Number(resetDay) })}>{t('common.save')}</Button>
        <Button size="xs" mb={2} variant="subtle" color="orange" onClick={() => call.mutate({ ResetUsage: true })}>{t('renewals.resetUsage')}</Button>
      </Group>
      <Text size="xs" c="dimmed">{t('subAdjust.hint')}</Text>
    </Stack>
  )
}
