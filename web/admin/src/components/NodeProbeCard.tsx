import { NodeStorageFields } from './NodeStorageFields'
import { storageDraft, type ProbeStorage, type ProbeStorageCandidate } from '../lib/probe-storage'
import { NodeBandwidthFields, emptyBandwidth, type NodeBandwidth } from './NodeBandwidthFields'
import { NodeRegionSelect, type RegionDetection } from './NodeRegionSelect'
import { ReturnRoutesCard } from './ReturnRoutesCard'
import { AvailabilityCard } from './AvailabilityCard'
import { NetworkQualityCard } from './NetworkQualityCard'
import { NetworkDiagnosticsCard } from './NetworkDiagnosticsCard'
import { ResourceHistoryCard } from './ResourceHistoryCard'
import { ResourcesCard } from './ResourcesCard'
import { ResourceOptionsFields } from './ResourceOptionsFields'
import type { ResourceHost, ResourceOptions } from '../lib/resources'
import { Button, Card, Group, NumberInput, Progress, Select, SimpleGrid, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes } from '../lib/format'
import { trafficLimitBytes, trafficLimitFactors, trafficLimitInput, validTrafficLimit, type TrafficLimitUnit } from '../lib/traffic-limit'
import { toast } from '../lib/notify'

type Cost = { amount: string; currency: string; cycle: string; years?: string }
const currencies = ["USD", "CNY", "EUR", "GBP", "JPY", "HKD", "TWD", "SGD", "AUD", "CAD", "CHF", "KRW", "INR", "RUB", "TRY", "BRL", "CZK", "PLN", "SEK", "NOK", "DKK", "NZD", "ZAR", "MXN", "THB", "IDR", "MYR", "VND", "AED", "SAR", "PHP", "UAH"]
interface NodeProbe {
  storage_candidates?: ProbeStorageCandidate[]
  region_countries: string[]
  probe: { storage?: ProbeStorage; region_mode: string; region_detection: RegionDetection; resources?: ResourceOptions; node_id: number; hidden: boolean; info: { bandwidth?: NodeBandwidth; billing?: Cost; traffic_reset?: { mode: string }; region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }; limit_bytes: number; reset_day: number; mode: string; period_start: string; used_up: number; used_down: number; prev_used: number }
  billed: number
  live?: { at: string; host: ResourceHost }
}

// Per-node probe settings: display facts for the status page, hide flag,
// and the monthly NIC traffic allowance with its current usage.
export function NodeProbeCard(props: { nodeID: number; reportedHost?: ResourceHost | null }) {
  return <NodeProbeForm key={props.nodeID} {...props} />
}

function NodeProbeForm({ nodeID, reportedHost }: { nodeID: number; reportedHost?: ResourceHost | null }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['node-probe', nodeID], queryFn: () => api.get<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`), refetchInterval: 10_000 })
  const loaded = useRef(false)
  const form = useForm({ initialValues: { Storage: storageDraft(), Bandwidth: emptyBandwidth, Resources: {} as ResourceOptions, Hidden: false, region: '', provider: '', provider_url: '', price: '', costMode: 'structured', amount: '' as string | number, currency: 'USD', cycle: 'annual', years: '1' as string | number, resetPolicy: 'manual', expires_at: '', note: '', Limit: trafficLimitInput(0), ResetDay: 1, Mode: 'sum' }, validate: { Limit: v => validTrafficLimit(v) ? null : t('nodeProbe.limitInvalid') } })
  useEffect(() => { const p = q.data?.probe; if (p && !loaded.current) { loaded.current = true; form.setValues({ Storage: storageDraft(p.storage), Bandwidth: p.info.bandwidth ?? emptyBandwidth, Resources: p.resources ?? {}, Hidden: p.hidden, region: p.region_mode === 'auto' ? 'auto' : p.region_mode === 'off' ? 'off' : p.info.region || 'off', provider: p.info.provider ?? '', provider_url: p.info.provider_url ?? '', price: p.info.price ?? '', costMode: !p.info.billing && p.info.price ? 'legacy' : 'structured', amount: p.info.billing?.amount ?? '', currency: p.info.billing?.currency ?? 'USD', cycle: p.info.billing?.cycle ?? 'annual', years: p.info.billing?.years ?? '1', resetPolicy: p.info.traffic_reset?.mode ?? 'manual', expires_at: p.info.expires_at ?? '', note: p.info.note ?? '', Limit: trafficLimitInput(p.limit_bytes), ResetDay: p.reset_day, Mode: p.mode }) } }, [q.data, loaded]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put(`/api/admin/nodes/${nodeID}/probe`, { Hidden: v.Hidden, RegionMode: v.region === 'auto' || v.region === 'off' ? v.region : 'manual', Info: { bandwidth: v.Bandwidth, region: v.region === 'auto' || v.region === 'off' ? '' : v.region.toUpperCase(), provider: v.provider, provider_url: v.provider_url, price: v.price, billing: v.costMode === 'structured' ? { amount: String(v.amount), currency: v.currency, cycle: v.cycle, years: v.cycle === 'custom' ? String(v.years) : '' } : undefined, traffic_reset: { mode: v.resetPolicy }, expires_at: v.expires_at, note: v.note }, LimitBytes: v.Limit.bytes, ResetDay: v.ResetDay, Mode: v.Mode, Resources: v.Resources, Storage: storageDraft(v.Storage) }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['node-probe', nodeID] }) }, onError: toast.err })
  const detect = useMutation({ mutationFn: () => api.post<NodeProbe>(`/api/admin/nodes/${nodeID}/region-detect`), onSuccess: data => qc.setQueryData(['node-probe', nodeID], data), onError: toast.err })
  const reset = useMutation({ mutationFn: () => api.post(`/api/admin/nodes/${nodeID}/probe/reset-traffic`), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['node-probe', nodeID] }) }, onError: toast.err })
  const p = q.data?.probe
  const used = q.data?.billed ?? 0
  const pct = p && p.limit_bytes > 0 ? Math.min(100, Math.round((used / p.limit_bytes) * 100)) : 0
  const liveHost = q.data?.live?.host
  const host = (liveHost?.resources?.at ?? 0) >= (reportedHost?.resources?.at ?? 0) ? liveHost ?? reportedHost : reportedHost
  return (
    <><ResourcesCard host={host} />
    <AvailabilityCard nodeID={nodeID} />
    <NetworkQualityCard nodeID={nodeID} />
    <ReturnRoutesCard key={`routes-${nodeID}`} nodeID={nodeID} />
    <NetworkDiagnosticsCard key={nodeID} nodeID={nodeID} />
    <ResourceHistoryCard nodes={[{ id: nodeID, name: t('monitoring.kinds.host') }]} detail />
    <Card mb="lg">
      <Title order={5} mb="xs">{t('nodeProbe.title')}</Title>
      {p && (
        <Stack gap={4} mb="sm">
          <Group justify="space-between"><Text size="sm">{t('nodeProbe.used')}: <b>{bytes(used)}</b>{p.limit_bytes > 0 && <> / {bytes(p.limit_bytes)}</>}</Text><Text size="xs" c="dimmed">{t('nodeProbe.prev')}: {bytes(p.prev_used)}</Text></Group>
          {p.limit_bytes > 0 && <Progress value={pct} color={pct > 90 ? 'red' : pct > 70 ? 'orange' : 'teal'} />}
          <Text size="xs" c="dimmed">{t('nodeProbe.outbound')} {bytes(p.used_up)} · {t('nodeProbe.inbound')} {bytes(p.used_down)} · {t('nodeProbe.since')} {p.period_start ? new Date(p.period_start).toLocaleDateString() : '—'}</Text>
        </Stack>
      )}
      <form onSubmit={form.onSubmit((v) => { if (v.resetPolicy !== 'manual' && !v.expires_at) { form.setFieldError('expires_at', t('nodeCost.dateRequired')); return }; save.mutate(v) })}><Stack gap="sm">
        <ResourceOptionsFields disabled={!host?.resources} value={form.values.Resources} onChange={(v) => form.setFieldValue("Resources", v)} />
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          <NumberInput label={t('nodeProbe.limit')} description={t('nodeProbe.limitHint')} min={0} allowNegative={false} value={form.values.Limit.amount} error={form.errors.Limit} onChange={amount => form.setFieldValue('Limit', { ...form.values.Limit, amount, bytes: trafficLimitBytes(amount, form.values.Limit.unit) })} />
          <Select label={t('nodeProbe.limitUnit')} description={t('nodeProbe.limitUnitHint')} data={Object.keys(trafficLimitFactors)} value={form.values.Limit.unit} disabled={!validTrafficLimit(form.values.Limit)} allowDeselect={false} onChange={unit => { if (unit) form.setFieldValue('Limit', trafficLimitInput(form.values.Limit.bytes, unit as TrafficLimitUnit)) }} />
          <Stack gap="xs"><Select label={t('nodeProbe.resetDay')} allowDeselect={false} data={['expiry', 'expiry_next', 'manual'].map(value => ({ value, label: t(`nodeCost.reset_${value}`) }))} {...form.getInputProps('resetPolicy')} />
          {form.values.resetPolicy === 'manual' ? <NumberInput aria-label={t('nodeProbe.resetDay')} min={1} max={31} allowDecimal={false} {...form.getInputProps('ResetDay')} /> : <Text size="xs" c="dimmed">{form.values.expires_at ? t(form.values.resetPolicy === 'expiry_next' ? 'nodeCost.resetNextHint' : 'nodeCost.resetHint', { day: Number(form.values.expires_at.slice(8,10)) }) : t('nodeCost.dateRequired')}</Text>}</Stack>
          <Select label={t('nodeProbe.mode')} description={t('nodeProbe.modeHint')} data={[{ value: 'sum', label: t('nodeProbe.modes.sum') }, { value: 'up', label: t('nodeProbe.modes.up') }, { value: 'down', label: t('nodeProbe.modes.down') }, { value: 'max', label: t('nodeProbe.modes.max') }]} allowDeselect={false} {...form.getInputProps('Mode')} />
        </SimpleGrid>
        <NodeStorageFields value={form.values.Storage} candidates={q.data?.storage_candidates ?? []} onChange={v => form.setFieldValue('Storage', v)} disabled={!p || q.isError || save.isPending} />
        <NodeBandwidthFields value={form.values.Bandwidth} onChange={v => form.setFieldValue('Bandwidth', v)} />
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <NodeRegionSelect value={form.values.region || 'off'} onChange={v => form.setFieldValue('region', v)} countries={q.data?.region_countries ?? []} detection={p?.region_detection} detect={() => detect.mutate()} pending={detect.isPending} disabled={!p || q.isError || save.isPending} />
          <TextInput label={t('nodeProbe.provider')} placeholder="Vultr" {...form.getInputProps('provider')} />
          <TextInput label={t('nodeProbe.providerURL')} placeholder="https://…" {...form.getInputProps('provider_url')} />
        </SimpleGrid>
        {form.values.costMode === 'legacy' ? <Stack gap="xs"><TextInput label={t('nodeProbe.price')} {...form.getInputProps('price')} /><Group><Text size="xs" c="dimmed">{t('nodeCost.legacyHint')}</Text><Button variant="light" size="xs" onClick={() => form.setFieldValue('costMode', 'structured')}>{t('nodeCost.convert')}</Button></Group></Stack> : <>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <NumberInput label={t('nodeCost.amount')} description={t('nodeCost.amountHint')} min={0} max={999999999999} decimalScale={4} allowNegative={false} {...form.getInputProps('amount')} />
          <Select label={t('nodeCost.currency')} searchable allowDeselect={false} data={[...new Set([...currencies, form.values.currency])].map(value => ({ value, label: `${value} · ${new Intl.DisplayNames([i18n.language], { type: 'currency' }).of(value) ?? value}` }))} {...form.getInputProps('currency')} />
          <Select label={t('nodeCost.cycle')} allowDeselect={false} data={['monthly','quarterly','semiannual','annual','custom'].map(value => ({ value, label: t(`nodeCost.${value}`) }))} {...form.getInputProps('cycle')} />
        </SimpleGrid>
        {form.values.cycle === 'custom' && <NumberInput label={t('nodeCost.years')} description={t('nodeCost.yearsHint')} min={0.0001} max={999} decimalScale={4} allowNegative={false} {...form.getInputProps('years')} />}
        </>}
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <TextInput type="date" label={t('nodeProbe.expires')} description={t('nodeCost.expiryHint')} min="0001-01-01" max="9999-12-31" {...form.getInputProps('expires_at')} />
          <TextInput label={t('nodeProbe.note')} {...form.getInputProps('note')} />
        </SimpleGrid>
        <Group justify="space-between">
          <Switch label={t('nodeProbe.hidden')} {...form.getInputProps('Hidden', { type: 'checkbox' })} />
          <Group gap="xs"><Button size="xs" variant="subtle" color="gray" onClick={() => reset.mutate()}>{t('nodeProbe.reset')}</Button><Button type="submit" size="xs" disabled={!p || q.isError} loading={save.isPending}>{t('common.save')}</Button></Group>
        </Group>
      </Stack></form>
    </Card></>
  )
}
