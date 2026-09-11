import { Button, Card, Group, Modal, SegmentedControl, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, type Plan } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money } from '../lib/format'
import { toast } from '../lib/notify'

export default function PlansPage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/portal/plans') })
  const [plan, setPlan] = useState<Plan | null>(null)
  const gateways = me?.gateways ?? ['balance']
  const [gateway, setGateway] = useState(gateways.find((g) => g !== 'balance') ?? 'balance')
  const buy = useMutation({
    mutationFn: () => api.post<{ order_no: string; status: string; pay_url?: string }>('/api/portal/orders', { plan_id: plan!.ID, gateway }),
    onSuccess: (r) => {
      if (r.pay_url) { window.location.href = r.pay_url; return }
      toast.ok(t('plans.paid')); refresh(); nav('/')
    },
    onError: (e) => toast.err(e instanceof ApiError && e.status === 402 ? t('plans.insufficient') : e),
  })
  return (
    <Stack gap="lg">
      <Title order={2}>{t('plans.title')}</Title>
      <SimpleGrid cols={{ base: 1, xs: 2 }}>
        {(q.data ?? []).map((p) => (
          <Card key={p.ID}>
            <Stack gap="xs" h="100%">
              <Text fw={700} fz="lg">{p.Name}</Text>
              <Text fz={30} fw={800}>{money(p.PriceCents)}<Text span c="dimmed" fz="sm"> / {p.PeriodDays ? t('plans.period', { days: p.PeriodDays }) : t('plans.forever')}</Text></Text>
              <Text size="sm" c="dimmed">{p.QuotaBytes ? t('plans.quota', { gb: Math.round(p.QuotaBytes / 2 ** 30) }) : t('plans.unlimited')}{p.DeviceLimit ? ` · ${t('plans.devices', { count: p.DeviceLimit })}` : ''}</Text>
              <Button mt="auto" onClick={() => setPlan(p)}>{t('plans.buy')}</Button>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <Modal opened={plan !== null} onClose={() => setPlan(null)} title={plan?.Name} centered>
        {plan && (
          <Stack>
            <Text size="sm" fw={600}>{t('plans.payWith')}</Text>
            <SegmentedControl fullWidth value={gateway} onChange={setGateway} data={gateways.map((g) => ({ value: g, label: t(`plans.${g}`, { defaultValue: g }) }))} />
            {gateway === 'balance' && <Text size="sm" c="dimmed">{t('home.balance')}: {money(me?.balance_cents ?? 0)}</Text>}
            <Group justify="flex-end"><Button loading={buy.isPending} onClick={() => buy.mutate()}>{t('plans.confirm', { amount: money(plan.PriceCents) })}</Button></Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  )
}
