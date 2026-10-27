import { LocationFields } from './LocationFields'
import { locationDraft, nodeLocationDraft, locationDraftError, locationSettings, type GeoLocation, type LocationSettings } from '../lib/location'
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
import { Alert, Button, Card, Group, NumberInput, Progress, Select, SimpleGrid, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { SettingsDraftBoundary, useSettingsForm as useForm } from '../lib/settings-draft'
import { useAuth } from '../lib/auth'
import { NodeBillingLink, NodeSupplierFields } from './NodeInfrastructureFields'
import { applyDraftSave, financeDraft, reloadFinance as mergeFinance, sameInfrastructureSnapshot, nodeInfrastructureKey, nodeInfrastructureURL, suppliersKey, type NodeCost, type NodeInfrastructure, type SupplierOption } from '../lib/node-infrastructure'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { bytes } from '../lib/format'
import { trafficLimitBytes, trafficLimitFactors, trafficLimitInput, validTrafficLimit, type TrafficLimitUnit } from '../lib/traffic-limit'
import { toast } from '../lib/notify'
import { renewalPeriodMonths, renewalUnavailable, type NodeAutoRenewal } from '../lib/node-renewal'
import { currencyOptions } from '../lib/currency-options'

interface NodeProbe {
  infrastructure_revision?: string
  infrastructure_linked?: boolean
  infrastructure_supplier_linked?: boolean
  storage_candidates?: ProbeStorageCandidate[]
  region_countries: string[]
  probe: { storage?: ProbeStorage; region_mode: string; region_detection: RegionDetection; resources?: ResourceOptions; node_id: number; hidden: boolean; info: { location_settings?: LocationSettings; location?: GeoLocation; bandwidth?: NodeBandwidth; billing?: NodeCost; auto_renewal?: NodeAutoRenewal; traffic_reset?: { mode: string }; region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }; limit_bytes: number; reset_day: number; mode: string; period_start: string; used_up: number; used_down: number; prev_used: number }
  billed: number
  live?: { at: string; host: ResourceHost }
}

// Per-node probe settings: display facts for the status page, hide flag,
// and the monthly NIC traffic allowance with its current usage.
export function NodeProbeCard(props: { nodeID: number; reportedHost?: ResourceHost | null }) {
  return <SettingsDraftBoundary key={props.nodeID}><NodeProbeForm {...props} /></SettingsDraftBoundary>
}

function NodeProbeForm({ nodeID, reportedHost }: { nodeID: number; reportedHost?: ResourceHost | null }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['node-probe', nodeID], queryFn: ({ signal }) => api.get<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`, signal), refetchInterval: 10_000 })
  const { me } = useAuth()
  const admin = me?.role === 'admin'
  const infra = useQuery({ queryKey: nodeInfrastructureKey(nodeID), queryFn: ({ signal }) => api.get<NodeInfrastructure>(nodeInfrastructureURL(nodeID), signal), enabled: admin, refetchInterval: 10_000 })
  const suppliers = useQuery({ queryKey: suppliersKey, queryFn: () => api.get<SupplierOption[]>('/api/admin/infrastructure/suppliers'), enabled: admin })
  const [loaded, setLoaded] = useState(false)
  const [expiryConflict, setExpiryConflict] = useState(false)
  const [financeConflict, setFinanceConflict] = useState(false)
  const form = useForm({ initialValues: { ...financeDraft(), Location: locationDraft(), Storage: storageDraft(), Bandwidth: emptyBandwidth, Resources: {} as ResourceOptions, Hidden: false, region: '', resetPolicy: 'manual', note: '', Limit: trafficLimitInput(0), ResetDay: 1, Mode: 'sum' }, validate: { Limit: v => validTrafficLimit(v) ? null : t('nodeProbe.limitInvalid') } })
  const [baseline, setBaseline] = useState(() => form.getValues())
  const draftFromProbe = (data: NodeProbe, supplierID: number) => {
    const p = data.probe
    return { ...financeDraft(p.info, data.infrastructure_revision, supplierID), Location: nodeLocationDraft(p.info.location_settings, p.region_mode), Storage: storageDraft(p.storage), Bandwidth: p.info.bandwidth ?? emptyBandwidth, Resources: p.resources ?? {}, Hidden: p.hidden, region: p.region_mode === 'auto' ? 'auto' : p.region_mode === 'off' ? 'off' : p.info.region || 'off', resetPolicy: p.info.traffic_reset?.mode ?? 'manual', note: p.info.note ?? '', Limit: trafficLimitInput(p.limit_bytes), ResetDay: p.reset_day, Mode: p.mode }
  }
  const coherent = !admin || sameInfrastructureSnapshot(q.data?.infrastructure_revision, infra.data)
  const sourceError = q.isError || admin && (infra.isError || suppliers.isError)
  const cancelReads = () => Promise.all([qc.cancelQueries({ queryKey: ['node-probe', nodeID] }), qc.cancelQueries({ queryKey: nodeInfrastructureKey(nodeID) })])
  const save = useMutation({ onMutate: cancelReads, mutationFn: (v: typeof form.values) => api.put<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`, { ExpectedExpiresAt: baseline.expires_at, ...(admin ? { InfrastructureRevision: v.InfrastructureRevision, ...(v.SupplierID !== baseline.SupplierID ? { SupplierID: v.SupplierID } : {}) } : {}), Hidden: v.Hidden, RegionMode: v.region === 'auto' || v.region === 'off' ? v.region : 'manual', Info: { location_settings: locationSettings(v.Location), bandwidth: v.Bandwidth, region: v.region === 'auto' || v.region === 'off' ? '' : v.region.toUpperCase(), provider: v.provider, provider_url: v.provider_url, price: v.price, billing: v.costMode === 'structured' ? { amount: String(v.amount), currency: v.currency, cycle: v.cycle, years: v.cycle === 'custom' ? String(v.years) : '' } : undefined, traffic_reset: { mode: v.resetPolicy }, auto_renewal: { enabled: v.AutoRenewal }, expires_at: v.expires_at, note: v.note }, LimitBytes: v.Limit.bytes, ResetDay: v.ResetDay, Mode: v.Mode, Resources: v.Resources, Storage: storageDraft(v.Storage) }), onSuccess: async (data, submitted) => {
    await cancelReads()
    const saved = draftFromProbe(data, submitted.SupplierID)
    const current = applyDraftSave(form.getValues(), submitted, saved)
    setBaseline(saved); form.setValues(current); form.resetDirty(saved)
    setExpiryConflict(false); setFinanceConflict(false)
    qc.setQueryData(['node-probe', nodeID], data)
    qc.invalidateQueries({ queryKey: nodeInfrastructureKey(nodeID) }); qc.invalidateQueries({ queryKey: ['infrastructure'] })
    toast.ok(t('common.saved'))
  }, onError: error => {
    if (error instanceof ApiError && error.status === 409) { setExpiryConflict(true); if (admin) setFinanceConflict(true) }
    else toast.err(error)
  } })
  useEffect(() => {
    if (!q.data || !coherent || sourceError || save.isPending || form.isDirty()) return
    const draft = draftFromProbe(q.data, admin ? infra.data?.supplier?.id ?? 0 : 0)
    setLoaded(true); setBaseline(draft); form.hydrate(draft)
  }, [q.data, infra.data, coherent, sourceError]) // eslint-disable-line react-hooks/exhaustive-deps
  const reload = useMutation({ mutationFn: async () => {
    await cancelReads()
    // Separate endpoints may straddle a ledger write. Never combine revisions.
    for (let attempt = 0; attempt < 2; attempt++) {
      const [probe, relation] = await Promise.all([api.get<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`), admin ? api.get<NodeInfrastructure>(nodeInfrastructureURL(nodeID)) : Promise.resolve(undefined)])
      if (!admin || sameInfrastructureSnapshot(probe.infrastructure_revision, relation)) return { probe, relation }
    }
    throw new Error(t('infraLink.snapshotMismatch'))
  }, onSuccess: async ({ probe, relation }) => {
    await cancelReads()
    const finance = financeDraft(probe.probe.info, probe.infrastructure_revision, relation?.supplier?.id ?? 0)
    const nextBaseline = mergeFinance(baseline, finance)
    setBaseline(nextBaseline)
    form.setValues(mergeFinance(form.getValues(), finance)); form.resetDirty(nextBaseline)
    qc.setQueryData(['node-probe', nodeID], probe)
    if (relation) qc.setQueryData(nodeInfrastructureKey(nodeID), relation)
    setExpiryConflict(false); setFinanceConflict(false)
  }, onError: toast.err })
  const reloadFinance = async () => { try { await reload.mutateAsync(); return true } catch { return false } }
  const expectedExpiresAt = baseline.expires_at
  const staleFinance = admin && loaded && !!infra.data && form.values.InfrastructureRevision !== infra.data.revision
  const editingDisabled = !loaded || sourceError || save.isPending || reload.isPending
  const billingReadOnly = !admin && !!q.data?.infrastructure_linked
  const detect = useMutation({ mutationFn: () => api.post<NodeProbe>(`/api/admin/nodes/${nodeID}/region-detect`), onSuccess: data => qc.setQueryData(['node-probe', nodeID], data), onError: toast.err })
  const reset = useMutation({ mutationFn: () => api.post(`/api/admin/nodes/${nodeID}/probe/reset-traffic`), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['node-probe', nodeID] }) }, onError: toast.err })
  const p = q.data?.probe
  const renewalReason = renewalUnavailable(form.values.expires_at, form.values.costMode, form.values.cycle, form.values.years)
  const draftMonths = renewalPeriodMonths(form.values.cycle, form.values.years)
  const savedMonths = p?.info.billing ? renewalPeriodMonths(p.info.billing.cycle, p.info.billing.years ?? '') : null
  const samePeriod = form.values.costMode === 'structured' && draftMonths !== null && draftMonths === savedMonths
  const resetDate = form.values.expires_at === expectedExpiresAt && samePeriod ? p?.info.auto_renewal?.anchor_date || form.values.expires_at : form.values.expires_at
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
        {(sourceError || !coherent && !!q.data && !!infra.data) && <Alert color="red"><Text size="sm">{t(sourceError ? 'workspace.loadError' : 'infraLink.snapshotMismatch')}</Text><Button size="xs" variant="subtle" onClick={() => { q.refetch(); if (admin) { infra.refetch(); suppliers.refetch() } }}>{t('workspace.retry')}</Button></Alert>}
      <form onSubmit={form.onSubmit((v) => { if (!p || editingDisabled || !coherent || staleFinance || financeConflict || locationDraftError(v.Location)) return; if (v.resetPolicy !== 'manual' && !v.expires_at) { form.setFieldError('expires_at', t('nodeCost.dateRequired')); return }; if (v.AutoRenewal && renewalReason) { form.setFieldError('AutoRenewal', t(renewalReason)); return }; save.mutate(v) })}><fieldset disabled={!loaded} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack gap="sm">
        <ResourceOptionsFields disabled={!host?.resources} value={form.values.Resources} onChange={(v) => form.setFieldValue("Resources", v)} />
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          <NumberInput label={t('nodeProbe.limit')} description={t('nodeProbe.limitHint')} min={0} allowNegative={false} value={form.values.Limit.amount} error={form.errors.Limit} onChange={amount => form.setFieldValue('Limit', { ...form.values.Limit, amount, bytes: trafficLimitBytes(amount, form.values.Limit.unit) })} />
          <Select label={t('nodeProbe.limitUnit')} description={t('nodeProbe.limitUnitHint')} data={Object.keys(trafficLimitFactors)} value={form.values.Limit.unit} disabled={!validTrafficLimit(form.values.Limit)} allowDeselect={false} onChange={unit => { if (unit) form.setFieldValue('Limit', trafficLimitInput(form.values.Limit.bytes, unit as TrafficLimitUnit)) }} />
          <Stack gap="xs"><Select label={t('nodeProbe.resetDay')} allowDeselect={false} data={['expiry', 'expiry_next', 'manual'].map(value => ({ value, label: t(`nodeCost.reset_${value}`) }))} {...form.getInputProps('resetPolicy')} />
          {form.values.resetPolicy === 'manual' ? <NumberInput aria-label={t('nodeProbe.resetDay')} min={1} max={31} allowDecimal={false} {...form.getInputProps('ResetDay')} /> : <Text size="xs" c="dimmed">{form.values.expires_at ? t(form.values.resetPolicy === 'expiry_next' ? 'nodeCost.resetNextHint' : 'nodeCost.resetHint', { day: Number(resetDate.slice(8,10)) }) : t('nodeCost.dateRequired')}</Text>}</Stack>
          <Select label={t('nodeProbe.mode')} description={t('nodeProbe.modeHint')} data={[{ value: 'sum', label: t('nodeProbe.modes.sum') }, { value: 'up', label: t('nodeProbe.modes.up') }, { value: 'down', label: t('nodeProbe.modes.down') }, { value: 'max', label: t('nodeProbe.modes.max') }]} allowDeselect={false} {...form.getInputProps('Mode')} />
        </SimpleGrid>
        <NodeStorageFields value={form.values.Storage} candidates={q.data?.storage_candidates ?? []} onChange={v => form.setFieldValue('Storage', v)} disabled={!p || q.isError || save.isPending} />
        <NodeBandwidthFields value={form.values.Bandwidth} onChange={v => form.setFieldValue('Bandwidth', v)} />
        <NodeRegionSelect value={form.values.region || 'off'} onChange={v => form.setFieldValue('region', v)} countries={q.data?.region_countries ?? []} detection={p?.region_detection} detect={() => detect.mutate()} pending={detect.isPending} disabled={editingDisabled} />
        <NodeSupplierFields value={form.values} options={suppliers.data ?? []} selected={infra.data?.supplier} canManage={admin} disabled={editingDisabled || admin && (!coherent || suppliers.isPending)} linked={!admin && !!q.data?.infrastructure_supplier_linked} billingLinked={!!q.data?.infrastructure_linked} onChange={value => form.setValues(value)} />
        <LocationFields value={form.values.Location} onChange={v => form.setFieldValue('Location', v)} effective={p?.info.location} countries={q.data?.region_countries} disabled={!p || q.isError || save.isPending} />
        {form.values.Location.mode === 'auto' && <Stack gap={4}>{p?.region_detection.last_success_at ? <Text size="xs" c="dimmed">{t('mapLocation.lastSuccess', { time: new Date(p.region_detection.last_success_at * 1000).toLocaleString() })}</Text> : null}{p?.region_detection.stale && <Text size="xs" c="orange">{t('mapLocation.stale')}</Text>}</Stack>}
        {admin && infra.data && <NodeBillingLink nodeID={nodeID} infra={infra.data} disabled={editingDisabled || !coherent || staleFinance || financeConflict} dirty={form.isDirty()} onApplied={reloadFinance} />}
        {billingReadOnly && <Alert color="blue">{t('infraLink.operatorBilling')}</Alert>}
        <fieldset disabled={billingReadOnly || editingDisabled} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack gap="sm">
        {form.values.costMode === 'legacy' ? <Stack gap="xs"><TextInput label={t('nodeProbe.price')} {...form.getInputProps('price')} /><Group><Text size="xs" c="dimmed">{t('nodeCost.legacyHint')}</Text><Button variant="light" size="xs" onClick={() => form.setFieldValue('costMode', 'structured')}>{t('nodeCost.convert')}</Button></Group></Stack> : <>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <NumberInput label={t('nodeCost.amount')} description={t(q.data?.infrastructure_linked ? 'infraLink.linkedAmountHint' : 'nodeCost.amountHint')} min={0} max={999999999999} decimalScale={4} allowNegative={false} {...form.getInputProps('amount')} />
          <Select label={t('nodeCost.currency')} searchable allowDeselect={false} data={currencyOptions(i18n.language, form.values.currency)} {...form.getInputProps('currency')} />
          <Select label={t('nodeCost.cycle')} allowDeselect={false} data={['monthly','quarterly','semiannual','annual','custom'].map(value => ({ value, label: t(`nodeCost.${value}`) }))} {...form.getInputProps('cycle')} />
        </SimpleGrid>
        {form.values.cycle === 'custom' && <NumberInput label={t('nodeCost.years')} description={t('nodeCost.yearsHint')} min={0.0001} max={999} decimalScale={4} allowNegative={false} {...form.getInputProps('years')} />}
        </>}
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <Stack gap="xs">
            <TextInput type="date" label={t('nodeProbe.expires')} description={t('nodeCost.expiryHint')} min="0001-01-01" max="9999-12-31" {...form.getInputProps('expires_at')} />
            <Switch label={t('nodeCost.autoRenewal')} description={t(renewalReason ?? 'nodeCost.autoRenewalHint')} {...form.getInputProps('AutoRenewal', { type: 'checkbox' })} disabled={billingReadOnly || !p || q.isError || save.isPending || (!form.values.AutoRenewal && !!renewalReason)} error={form.errors.AutoRenewal && renewalReason ? t(renewalReason) : undefined} />
          </Stack>
        </SimpleGrid>
        </Stack></fieldset>
        <TextInput label={t('nodeProbe.note')} {...form.getInputProps('note')} />
        {(expiryConflict || financeConflict || staleFinance) && <Alert color="yellow"><Stack gap="xs"><Text size="sm">{t(admin ? 'infraLink.conflict' : 'nodeCost.renewalConflict')}</Text><Button variant="light" size="xs" loading={reload.isPending} onClick={() => reload.mutate()}>{t(admin ? 'infraLink.reloadFinance' : 'nodeCost.reloadExpiry')}</Button></Stack></Alert>}
        <Group justify="space-between">
          <Switch label={t('nodeProbe.hidden')} {...form.getInputProps('Hidden', { type: 'checkbox' })} />
          <Group gap="xs"><Button size="xs" variant="subtle" color="gray" onClick={() => reset.mutate()}>{t('nodeProbe.reset')}</Button><Button type="submit" size="xs" disabled={!p || editingDisabled || !coherent || staleFinance || financeConflict || !!locationDraftError(form.values.Location)} loading={save.isPending}>{t('common.save')}</Button></Group>
        </Group>
      </Stack></fieldset></form>
    </Card></>
  )
}
