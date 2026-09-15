import { Badge, Button, Card, Group, Modal, SegmentedControl, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, type Plan, type Quote } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, when } from '../lib/format'
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
  // How the new plan meets what the user already holds: decided in a small
  // dialog before the purchase form opens.
  const [activation, setActivation] = useState<'' | 'queue'>('')
  const [ask, setAsk] = useState<{ plan: Plan; kind: 'renew' | 'stack' | 'replace' } | null>(null)
  const held = (me?.subscriptions ?? []).filter((s) => s.status === 'active' && s.usable)
  const queued = (me?.subscriptions ?? []).filter((s) => s.status === 'queued')
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
    mutationFn: () => api.post<{ order_no: string; status: string; pay_url?: string }>('/api/portal/orders', { plan_id: plan!.ID, period_days: period, coupon, gateway, activation }),
    onSuccess: (r) => {
      if (r.pay_url) { window.location.href = r.pay_url; return }
      toast.ok(t('plans.paid')); refresh(); nav('/')
    },
    onError: (e) => toast.err(e instanceof ApiError && e.status === 402 ? t('plans.insufficient') : e),
  })
  const openPlan = (p: Plan, act: '' | 'queue' = '') => { setActivation(act); setAsk(null); setPlan(p); setPeriod(p.PeriodDays); setCoupon(''); setQuote(null); setCouponError('') }
  const choose = (p: Plan) => {
    if (held.length === 0) return openPlan(p)
    if (held.some((s) => s.plan_id === p.ID)) return setAsk({ plan: p, kind: 'renew' })
    setAsk({ plan: p, kind: me?.single_plan ? 'replace' : 'stack' })
  }
  const activationNote = () => {
    if (activation === 'queue') return t('plans.noteQueue')
    if (held.some((s) => s.plan_id === plan?.ID)) return t('plans.noteRenew')
    if (held.length > 0) return me?.single_plan ? t('plans.noteReplace') : t('plans.noteStack')
    return ''
  }
  return (
    <Stack gap="lg">
      <Title order={2}>{t('plans.title')}</Title>
      {(held.length > 0 || queued.length > 0) && (
        <Card>
          <Text size="xs" tt="uppercase" c="dimmed" fw={700} mb="xs">{t('plans.mine')}</Text>
          <Stack gap={6}>
            {[...held, ...queued].map((s) => (
              <Group key={s.id} justify="space-between" wrap="nowrap">
                <Group gap="xs"><Text size="sm" fw={600}>{s.plan_name}</Text><Badge size="xs" variant="light" color={s.status === 'queued' ? 'gray' : 'teal'}>{s.status === 'queued' ? t('home.statusQueued') : t('home.statusActive')}</Badge></Group>
                <Text size="xs" c="dimmed">{s.status === 'queued' ? t('plans.mineQueued') : s.expires_at ? t('plans.mineExpires', { date: when(s.expires_at).split(',')[0] }) : t('home.never')}</Text>
              </Group>
            ))}
          </Stack>
          <Text size="xs" c="dimmed" mt="sm">{me?.single_plan ? t('plans.mineHintSingle') : t('plans.mineHint')}</Text>
        </Card>
      )}
      <SimpleGrid cols={{ base: 1, xs: 2 }}>
        {(q.data ?? []).map((p) => (
          <Card key={p.ID}>
            <Stack gap="xs" h="100%">
              <Text fw={700} fz="lg">{p.Name}</Text>
              <Text fz={30} fw={800}>{money(cheapest(p).price_cents)}<Text span c="dimmed" fz="sm"> / {periodLabel(cheapest(p).period_days)}</Text></Text>
              {periods(p).length > 1 && <Text size="xs" c="dimmed">{t('plans.morePeriods', { count: periods(p).length })}</Text>}
              <Text size="sm" c="dimmed">{p.QuotaBytes ? t('plans.quota', { gb: Math.round(p.QuotaBytes / 2 ** 30) }) : t('plans.unlimited')}{p.DeviceLimit ? ` · ${t('plans.devices', { count: p.DeviceLimit })}` : ''}</Text>
              <Button mt="auto" onClick={() => choose(p)}>{held.some((s) => s.plan_id === p.ID) ? t('plans.renew') : t('plans.buy')}</Button>
            </Stack>
          </Card>
        ))}
      </SimpleGrid>
      <Modal opened={ask !== null} onClose={() => setAsk(null)} title={ask ? t(`plans.ask.${ask.kind}Title`, { plan: ask.plan.Name }) : ''} centered>
        {ask && (
          <Stack>
            <Text size="sm">{t(`plans.ask.${ask.kind}Body`, { plan: ask.plan.Name })}</Text>
            {ask.kind === 'stack' ? (
              <Stack gap="xs">
                <Button onClick={() => openPlan(ask.plan, '')}>{t('plans.ask.stackNow')}</Button>
                <Button variant="light" onClick={() => openPlan(ask.plan, 'queue')}>{t('plans.ask.stackLater')}</Button>
                <Button variant="subtle" color="gray" onClick={() => setAsk(null)}>{t('common.cancel')}</Button>
              </Stack>
            ) : (
              <Group justify="flex-end">
                <Button variant="default" onClick={() => setAsk(null)}>{t('common.cancel')}</Button>
                <Button onClick={() => openPlan(ask.plan, '')}>{t(ask.kind === 'renew' ? 'plans.ask.renewGo' : 'plans.ask.replaceGo')}</Button>
              </Group>
            )}
          </Stack>
        )}
      </Modal>
      <Modal opened={plan !== null} onClose={() => setPlan(null)} title={plan?.Name} centered>
        {plan && (
          <Stack>
            {activationNote() && <Text size="sm" c="dimmed">{activationNote()}</Text>}
            {periods(plan).length > 1 && (<>
              <Text size="sm" fw={600}>{t('plans.choosePeriod')}</Text>
              <SegmentedControl fullWidth value={String(period)} onChange={(v) => setPeriod(Number(v))} data={periods(plan).map((pp) => ({ value: String(pp.period_days), label: `${periodLabel(pp.period_days)} · ${money(pp.price_cents)}` }))} />
            </>)}
            <TextInput label={t('plans.coupon')} placeholder={t('plans.couponHint')} value={coupon} onChange={(e) => setCoupon(e.currentTarget.value.toUpperCase())} error={couponError || undefined} />
            <Text size="sm" fw={600}>{t('plans.payWith')}</Text>
            <SegmentedControl fullWidth value={gateway} onChange={setGateway} data={gateways.map((g) => ({ value: g, label: t(`plans.${g}`, { defaultValue: g }) }))} />
            {gateway === 'balance' && <Text size="sm" c="dimmed">{t('home.balance')}: {money(me?.balance_cents ?? 0)}</Text>}
            {quote && quote.discount_cents > 0 && <Text size="sm" c="teal">{t('plans.discount', { amount: money(quote.discount_cents) })}</Text>}
            {quote && quote.surplus_cents > 0 && <Text size="sm" c="teal">{t('plans.surplus', { amount: money(quote.surplus_cents) })}</Text>}
            <Group justify="flex-end"><Button loading={buy.isPending} disabled={!quote} onClick={() => buy.mutate()}>{t('plans.confirm', { amount: money(quote?.amount_cents ?? 0) })}</Button></Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  )
}
