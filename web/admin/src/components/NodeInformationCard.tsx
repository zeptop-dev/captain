import { latestResourceHost } from '../lib/resource-host'
import { LocationFields } from './LocationFields'
import { locationDraft, nodeLocationDraft, locationDraftError, locationSettings } from '../lib/location'
import { NodeStorageFields } from './NodeStorageFields'
import { storageDraft } from '../lib/probe-storage'
import { NodeBandwidthFields, emptyBandwidth } from './NodeBandwidthFields'
import { NodeRegionSelect } from './NodeRegionSelect'
import { ResourceOptionsFields } from './ResourceOptionsFields'
import type { ResourceHost, ResourceOptions } from '../lib/resources'
import { Alert, Button, Card, Divider, Group, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useSettingsForm as useForm } from '../lib/settings-draft'
import { useAuth } from '../lib/auth'
import { NodeBillingLink, NodeSupplierFields } from './NodeInfrastructureFields'
import { applyDraftSave, financeDraft, reloadFinance as mergeFinance, sameInfrastructureSnapshot, nodeInfrastructureKey, nodeInfrastructureURL, suppliersKey, type NodeInfrastructure, type SupplierOption } from '../lib/node-infrastructure'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { useNodeProbe, type NodeProbe } from '../lib/node-probe'
import { NodeProbeCostSummary, NodeInformationSummary } from './NodeInformationSummary'
import { api, ApiError } from '../lib/api'
import { trafficLimitBytes, trafficLimitFactors, trafficLimitInput, validTrafficLimit, type TrafficLimitUnit } from '../lib/traffic-limit'
import { toast } from '../lib/notify'
import { renewalAnchorDay, renewalPeriodMonths, renewalUnavailable } from '../lib/node-renewal'
import { currencyOptions } from '../lib/currency-options'

// One draft and one save keep visibility, accounting and infrastructure revisions coherent.
export function NodeInformationCard({ nodeID, reportedHost }: { nodeID: number; reportedHost?: ResourceHost | null }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useNodeProbe(nodeID)
  const location = useLocation()
  const [editing, setEditing] = useState(location.hash === '#billing-link')
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
    if (JSON.stringify(current) === JSON.stringify(saved)) setEditing(false)
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
  const resetDay = renewalAnchorDay(form.values.expires_at === expectedExpiresAt && samePeriod ? p?.info.auto_renewal : undefined, form.values.expires_at)
  const liveHost = q.data?.live?.host
  const host = latestResourceHost(liveHost, reportedHost)
  useEffect(() => {
    if (location.hash !== '#billing-link') return
    const frame = requestAnimationFrame(() => setEditing(true))
    return () => cancelAnimationFrame(frame)
  }, [location.key, location.hash])
  useEffect(() => {
    if (!editing || !loaded || location.hash !== '#billing-link') return
    const frame = requestAnimationFrame(() => document.getElementById('billing-link')?.scrollIntoView({ block: 'center' }))
    return () => cancelAnimationFrame(frame)
  }, [editing, loaded, location.hash])
  const cancelEditing = () => {
    const saved = q.data && coherent && !sourceError ? draftFromProbe(q.data, admin ? infra.data?.supplier?.id ?? 0 : 0) : baseline
    setBaseline(saved); form.setValues(saved); form.resetDirty(saved); form.clearErrors()
    setExpiryConflict(false); setFinanceConflict(false); setEditing(false)
  }
  return (
    <Card mb="lg" data-testid="node-information">
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <Stack gap={4}><Title order={4}>{t('nodeInformation.title')}</Title><Text size="sm" c="dimmed">{t('nodeInformation.hint')}</Text></Stack>
          {!editing && <Button variant="light" disabled={editingDisabled || !coherent} onClick={() => setEditing(true)}>{t('nodeInformation.edit')}</Button>}
        </Group>
        <Switch label={t('nodeInformation.showOnProbe')} description={t('nodeInformation.visibilityHint')} checked={loaded && !form.values.Hidden} disabled={!editing || editingDisabled} onChange={event => form.setFieldValue('Hidden', !event.currentTarget.checked)} />
        {(sourceError || !coherent && !!q.data && !!infra.data) && <Alert color="red"><Text size="sm">{t(sourceError ? 'workspace.loadError' : 'infraLink.snapshotMismatch')}</Text><Button size="xs" variant="subtle" onClick={() => { q.refetch(); if (admin) { infra.refetch(); suppliers.refetch() } }}>{t('workspace.retry')}</Button></Alert>}
        {!loaded && !sourceError && <Text size="sm" c="dimmed">{t('nodeInformation.loading')}</Text>}
        {!editing && q.data && <>
          <NodeInformationSummary data={q.data} host={host} />
          <Divider />
          <Title order={5}>{t('nodeInformation.billing')}</Title>
          <NodeProbeCostSummary data={q.data} />
          {admin && infra.data?.asset && <Text size="sm">{t('infraLink.linkedTitle')}: {infra.data.asset.name}</Text>}
          {admin && infra.data?.asset && !infra.data.asset.active && <Text size="sm" c="orange">{t('infraLink.archivedLinked')}</Text>}
          {admin && <Button variant="subtle" size="sm" style={{ alignSelf: 'flex-start' }} disabled={editingDisabled || !coherent} onClick={() => { setEditing(true); requestAnimationFrame(() => document.getElementById('billing-link')?.scrollIntoView({ block: 'center' })) }}>{t('nodeInformation.editBilling')}</Button>}
          <Divider />
          <Stack gap={4}><Title order={5}>{t('nodeProbe.note')}</Title><Text size="sm" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{p?.info.note || t('nodeInformation.notSet')}</Text></Stack>
        </>}
      {editing && <form data-testid="node-information-editor" onSubmit={form.onSubmit((v) => { if (!p || editingDisabled || !coherent || staleFinance || financeConflict || locationDraftError(v.Location)) return; if (v.resetPolicy !== 'manual' && !v.expires_at) { form.setFieldError('expires_at', t('nodeCost.dateRequired')); return }; if (v.AutoRenewal && renewalReason) { form.setFieldError('AutoRenewal', t(renewalReason)); return }; save.mutate(v) })}><fieldset disabled={!loaded} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack gap="sm">
        <Title order={5}>{t('nodeInformation.traffic')}</Title>
        <ResourceOptionsFields disabled={!host?.resources} value={form.values.Resources} onChange={(v) => form.setFieldValue("Resources", v)} />
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          <NumberInput label={t('nodeProbe.limit')} description={t('nodeProbe.limitHint')} min={0} allowNegative={false} value={form.values.Limit.amount} error={form.errors.Limit} onChange={amount => form.setFieldValue('Limit', { ...form.values.Limit, amount, bytes: trafficLimitBytes(amount, form.values.Limit.unit) })} />
          <Select label={t('nodeProbe.limitUnit')} description={t('nodeProbe.limitUnitHint')} data={Object.keys(trafficLimitFactors)} value={form.values.Limit.unit} disabled={!validTrafficLimit(form.values.Limit)} allowDeselect={false} onChange={unit => { if (unit) form.setFieldValue('Limit', trafficLimitInput(form.values.Limit.bytes, unit as TrafficLimitUnit)) }} />
          <Stack gap="xs"><Select label={t('nodeProbe.resetDay')} allowDeselect={false} data={['expiry', 'expiry_next', 'manual'].map(value => ({ value, label: t(`nodeCost.reset_${value}`) }))} {...form.getInputProps('resetPolicy')} />
          {form.values.resetPolicy === 'manual' ? <NumberInput aria-label={t('nodeProbe.resetDay')} min={1} max={31} allowDecimal={false} {...form.getInputProps('ResetDay')} /> : <Text size="xs" c="dimmed">{resetDay !== null ? t(form.values.resetPolicy === 'expiry_next' ? 'nodeCost.resetNextHint' : 'nodeCost.resetHint', { day: resetDay }) : t('nodeCost.dateRequired')}</Text>}</Stack>
          <Select label={t('nodeProbe.mode')} description={t('nodeProbe.modeHint')} data={[{ value: 'sum', label: t('nodeProbe.modes.sum') }, { value: 'up', label: t('nodeProbe.modes.up') }, { value: 'down', label: t('nodeProbe.modes.down') }, { value: 'max', label: t('nodeProbe.modes.max') }]} allowDeselect={false} {...form.getInputProps('Mode')} />
        </SimpleGrid>
        <Divider my="xs" />
        <Title order={5}>{t('nodeInformation.specifications')}</Title>
        <NodeStorageFields value={form.values.Storage} candidates={q.data?.storage_candidates ?? []} onChange={v => form.setFieldValue('Storage', v)} disabled={!p || q.isError || save.isPending} />
        <NodeBandwidthFields value={form.values.Bandwidth} onChange={v => form.setFieldValue('Bandwidth', v)} />
        <Divider my="xs" />
        <Title order={5}>{t('nodeInformation.location')}</Title>
        <NodeRegionSelect value={form.values.region || 'off'} onChange={v => form.setFieldValue('region', v)} countries={q.data?.region_countries ?? []} detection={p?.region_detection} detect={() => detect.mutate()} pending={detect.isPending} disabled={editingDisabled} />
        <LocationFields value={form.values.Location} onChange={v => form.setFieldValue('Location', v)} effective={p?.info.location} countries={q.data?.region_countries} disabled={!p || q.isError || save.isPending} />
        {form.values.Location.mode === 'auto' && <Stack gap={4}>{p?.region_detection.last_success_at ? <Text size="xs" c="dimmed">{t('mapLocation.lastSuccess', { time: new Date(p.region_detection.last_success_at * 1000).toLocaleString() })}</Text> : null}{p?.region_detection.stale && <Text size="xs" c="orange">{t('mapLocation.stale')}</Text>}</Stack>}
        <Divider my="xs" />
        <Title order={5}>{t('nodeInformation.billing')}</Title>
        <NodeSupplierFields value={form.values} options={suppliers.data ?? []} selected={infra.data?.supplier} canManage={admin} disabled={editingDisabled || admin && (!coherent || suppliers.isPending)} linked={!admin && !!q.data?.infrastructure_supplier_linked} billingLinked={!!q.data?.infrastructure_linked} onChange={value => form.setValues(value)} />
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
          <Button type="button" size="xs" variant="default" disabled={save.isPending || reload.isPending} onClick={cancelEditing}>{t('nodeInformation.cancel')}</Button>
          <Group gap="xs"><Button size="xs" variant="subtle" color="gray" onClick={() => reset.mutate()}>{t('nodeProbe.reset')}</Button><Button type="submit" size="xs" disabled={!p || editingDisabled || !coherent || staleFinance || financeConflict || !!locationDraftError(form.values.Location)} loading={save.isPending}>{t('common.save')}</Button></Group>
        </Group>
      </Stack></fieldset></form>}
      </Stack>
    </Card>
  )
}
