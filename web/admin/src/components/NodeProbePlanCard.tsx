import { Alert, Badge, Button, Card, Group, Select, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { carrierProvinceOptions } from '../../../shared/carrier-provinces'
import { api, ApiError } from '../lib/api'
import { carrierProbeDraft, carrierTargetPlaceholder, type CarrierFamily } from '../lib/carrier-probe'
import { nodeProbePlanDraft, nodeProbePlanPayload, type NodeProbePlan, type ProbePlanMode } from '../lib/node-probe-plan'
import { useSettingsForm } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { CarrierProbeFields } from './CarrierProbeFields'
import { SettingsLoadState } from './SettingsLoadState'

export function ProbePlanSummary({ plan, targets = false }: { plan: { effective: NodeProbePlan['effective']; monitoring_enabled?: boolean }; targets?: boolean }) {
  const { t, i18n } = useTranslation()
  const effective = plan.effective
  const probe = effective.config
  const custom = (['ipv4', 'ipv6'] as CarrierFamily[]).filter(family => probe.family === 'dual' || probe.family === family).some(family => probe[`${probe.protocol}_${family}`]?.length)
  const province = carrierProvinceOptions(i18n.language).find(p => p.value === (probe.tcp_province || 'bj'))?.label
  return <Stack gap={4}>
    <Group gap="xs"><Badge color={effective.enabled ? 'blue' : 'gray'}>{t(`nodeProbePlan.mode.${effective.source}`)}</Badge><Text size="sm">{plan.monitoring_enabled === false ? t('nodeProbePlan.masterOff') : !effective.enabled ? t('nodeProbePlan.off') : `${probe.protocol.toUpperCase()} · ${probe.family === 'dual' ? 'IPv4 + IPv6' : probe.family === 'ipv6' ? 'IPv6' : 'IPv4'}${custom ? ` · ${t('probe.carrierCustomActive')}` : probe.protocol === 'tcp' ? ` · ${province}` : ''}`}</Text></Group>
    {targets && effective.enabled && (['ipv4', 'ipv6'] as CarrierFamily[]).filter(family => probe.family === 'dual' || probe.family === family).map(family => <Text key={family} size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{family === 'ipv4' ? 'IPv4' : 'IPv6'}: {(probe[`${probe.protocol}_${family}`] ?? []).map(target => `${target.name}: ${target.addr}`).join(' · ') || carrierTargetPlaceholder(probe.protocol, family, probe.tcp_province).replaceAll('\n', ' · ')}</Text>)}
  </Stack>
}

export function NodeProbePlanCard({ nodeID }: { nodeID: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['node-probe-plan', nodeID], queryFn: () => api.get<NodeProbePlan>(`/api/admin/nodes/${nodeID}/probe-plan`) })
  const form = useSettingsForm({ initialValues: { mode: 'inherit' as ProbePlanMode, revision: '', initialized: false, ...carrierProbeDraft() } })
  useEffect(() => { if (q.data) form.hydrate(nodeProbePlanDraft(q.data)) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (values: typeof form.values) => api.put<NodeProbePlan>(`/api/admin/settings/nodes/${nodeID}/probe-plan`, nodeProbePlanPayload(values)), onSuccess: plan => {
    const values = nodeProbePlanDraft(plan)
    form.setValues(values); form.resetDirty(values)
    qc.setQueryData(['node-probe-plan', nodeID], plan)
    qc.invalidateQueries({ queryKey: ['network-quality', nodeID] }); qc.invalidateQueries({ queryKey: ['node', String(nodeID)] })
    toast.ok(t('common.saved'))
  }, onError: error => { if (error instanceof ApiError && error.status === 409) { toast.err(new Error(t('nodeProbePlan.conflict'))); qc.invalidateQueries({ queryKey: ['node-probe-plan', nodeID] }) } else toast.err(error) } })
  if (!q.data) return <SettingsLoadState query={q} />
  return <Card mb="lg"><Stack gap="sm">
    <Title order={5}>{t('nodeProbePlan.title')}</Title>
    <Text size="sm" c="dimmed">{t('nodeProbePlan.hint')}</Text>
    <Text fw={500} size="sm">{t('nodeProbePlan.effective')}</Text>
    <ProbePlanSummary plan={q.data} targets />
    {save.error instanceof ApiError && save.error.status === 409 && <Alert color="orange">{t('nodeProbePlan.conflict')}<Button variant="subtle" size="compact-xs" ml="xs" onClick={async () => { const result = await q.refetch(); if (result.data && !result.isError) { const values = nodeProbePlanDraft(result.data); form.setValues(values); form.resetDirty(values); save.reset() } }}>{t('nodeProbePlan.reload')}</Button></Alert>}
    {q.isError && <Alert color="orange">{t('nodeProbePlan.loadError')}</Alert>}
    <form onSubmit={form.onSubmit(values => save.mutate(values))}><Stack gap="sm">
      <Select label={t('nodeProbePlan.modeLabel')} allowDeselect={false} data={(['inherit', 'custom', 'disabled'] as const).map(value => ({ value, label: t(`nodeProbePlan.mode.${value}`) }))} value={form.values.mode} onChange={value => { if (!value) return; form.setFieldValue('mode', value as ProbePlanMode); if (value === 'custom') form.setFieldValue('initialized', true) }} />
      <Text size="sm" c="dimmed">{t(`nodeProbePlan.modeHint.${form.values.mode}`)}</Text>
      {!q.data.monitoring_enabled && <Alert color="orange">{t('nodeProbePlan.masterOffHint')}</Alert>}
      {form.values.mode === 'custom' && <CarrierProbeFields probe={form.values.probe} texts={form.values.texts} onProbeChange={value => form.setFieldValue('probe', value)} onTextsChange={value => form.setFieldValue('texts', value)} />}
      <Group justify="flex-end"><Button variant="default" disabled={save.isPending || !form.isDirty()} onClick={() => { const values = nodeProbePlanDraft(q.data!); form.setValues(values); form.resetDirty(values); form.clearErrors(); save.reset() }}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={!form.isDirty() || q.isError}>{t('common.save')}</Button></Group>
    </Stack></form>
  </Stack></Card>
}
