import { Button, Card, Group, Modal, SegmentedControl, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, type Plan, type Quote } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money } from '../lib/format'
import { toast } from '../lib/notify'

export default function PlansPage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const nav = useNavigate()
  const q = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/portal/plans') })
  const [plan, setPlan] = useState<Plan | null>(null)
  const [period, setPeriod] = useState(0)
  const [coupon, setCoupon] = useState('')
  const [quote, setQuote] = useState<Quote | null>(null)
  const [couponError, setCouponError] = useState('')
  const gateways = me?.gateways ?? ['balance']
  const [gateway, setGateway] = useState(gateways.find((g) => g !== 'balance') ?? 'balance')
  const periods = (p: Plan) => [{ period_days: p.PeriodDays, price_cents: p.PriceCents }, ...(p.Prices ?? [])].sort((a, b) => a.period_days - b.period_days)
  const periodLabel = (d: number) => (d === 0 ? t('plans.forever') : d % 365 === 0 ? t('plans.years', { count: d / 365 }) : d % 30 === 0 ? t('plans.months', { count: d / 30 }) : t('plans.period', { days: d }))
  const cheapest = (p: Plan) => periods(p)[0]
  useEffect(() => {
    if (!plan) return
    let cancelled = false
    api.post<Quote>('/api/portal/orders/quote', { plan_id: plan.ID, period_days: period, coupon }).then((r) => { if (!cancelled) { setQuote(r); setCouponError('') } }).catch((e) => { if (!cancelled) { setQuote(null); setCouponError(e instanceof Error ? e.message : String(e)) } })
    return () => { cancelled = true }
  }, [plan, period, coupon])
  const buy = useMutation({
    mutationFn: () => api.post<{ order_no: string; status: string; pay_url?: string }>('/api/portal/orders', { plan_id: plan!.ID, period_days: period, coupon, gateway }),
    onSuccess: (r) => {
      if (r.pay_url) { window.location.href = r.pay_url; return }
      toast.ok(t('plans.paid')); refresh(); nav('/')
    },
    onError: (e) => toast.err(e instanceof ApiError && e.status === 402 ? t('plans.insufficient') : e),
  })
  const openPlan = (p: Plan) => { setPlan(p); setPeriod(p.PeriodDays); setCoupon(''); setQuote(null); setCouponError('') }
  return (
    <Stack gap="lg">
      <Title order={2}>{t('plans.title')}</Title>
      <SimpleGrid cols={{ base: 1, xs: 2 }}>
        {(q.data ?? []).map((p) => (
          <Card key={p.ID}>
            <Stack gap="xs" h="100%">
              <Text fw={700} fz="lg">{p.Name}</Text>
              <Text fz={30} fw={800}>{money(cheapest(p).price_cents)}<Text span c="dimmed" fz="sm"> / {periodLabel(cheapest(p).period_days)}</Text></Text>
              {periods(p).length > 1 && <Text size="xs" c="dimmed">{t('plans.morePeriods', { count: periods(p).length })}</Text>}
              <Text size="sm" c="dimmed">{p.QuotaBytes ? t('plans.quota', { gb: Math.round(p.QuotaBytes / 2 ** 30) }) : t('plans.unlimited')}{p.DeviceLimit ? ` · ${t('plans.devices', { count: p.DeviceLimit })}` : ''}</Text>
              <Button mt="auto" onClick={() => openPlan(p)}>{t('plans.buy')}</Button>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <Modal opened={plan !== null} onClose={() => setPlan(null)} title={plan?.Name} centered>
        {plan && (
          <Stack>
            {periods(plan).length > 1 && (<>
              <Text size="sm" fw={600}>{t('plans.choosePeriod')}</Text>
              <SegmentedControl fullWidth value={String(period)} onChange={(v) => setPeriod(Number(v))} data={periods(plan).map((pp) => ({ value: String(pp.period_days), label: `${periodLabel(pp.period_days)} · ${money(pp.price_cents)}` }))} />
            </>)}
            <TextInput label={t('plans.coupon')} placeholder={t('plans.couponHint')} value={coupon} onChange={(e) => setCoupon(e.currentTarget.value.toUpperCase())} error={couponError || undefined} />
            <Text size="sm" fw={600}>{t('plans.payWith')}</Text>
            <SegmentedControl fullWidth value={gateway} onChange={setGateway} data={gateways.map((g) => ({ value: g, label: t(`plans.${g}`, { defaultValue: g }) }))} />
            {gateway === 'balance' && <Text size="sm" c="dimmed">{t('home.balance')}: {money(me?.balance_cents ?? 0)}</Text>}
            {quote && quote.discount_cents > 0 && <Text size="sm" c="teal">{t('plans.discount', { amount: money(quote.discount_cents) })}</Text>}
            <Group justify="flex-end"><Button loading={buy.isPending} disabled={!quote} onClick={() => buy.mutate()}>{t('plans.confirm', { amount: money(quote?.amount_cents ?? 0) })}</Button></Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  )
}
