import { Alert, Button, Card, Group, Loader, NumberInput, Select, SimpleGrid, Stack, Switch, Text, Title } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { bytes } from '../lib/format'
import { applyDraftSave } from '../lib/node-infrastructure'
import { useSettingsForm } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { useNodeServiceControl } from '../lib/use-node-service-control'
import { controlCapabilityKey, emptyPolicy, policyDraft, policyDraftError, policyWrite, serviceControlKey, serviceControlURL, type NodeServiceControl, type PolicyDraft } from '../lib/node-service-control'

export function NodeTrafficControlPolicy({ node }: { node: Node }) {
  const { me } = useAuth()
  if (me?.role !== 'admin') return null
  return <TrafficControlPolicy node={node} />
}
function TrafficControlPolicy({ node }: { node: Node }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const q = useNodeServiceControl(node.id)
  const [snapshot, setSnapshot] = useState<NodeServiceControl | null>(null)
  const [conflict, setConflict] = useState(false)
  const form = useSettingsForm<PolicyDraft>({ initialValues: emptyPolicy })
  const cancelReads = () => qc.cancelQueries({ queryKey: serviceControlKey(node.id) })
  const save = useMutation({ onMutate: cancelReads, mutationFn: (draft: PolicyDraft) => api.put<NodeServiceControl>(serviceControlURL(node.id), policyWrite(snapshot!.revision, draft)), onSuccess: async (result, submitted) => {
    await cancelReads()
    const saved = policyDraft(result.policy, result.offline_enabled)
    form.setValues(applyDraftSave(form.getValues(), submitted, saved)); form.resetDirty(saved)
    setSnapshot(result); setConflict(false); qc.setQueryData(serviceControlKey(node.id), result)
    toast.ok(t('serviceControl.policySaved'))
  }, onError: error => { if (error instanceof ApiError && error.status === 409) setConflict(true); else toast.err(error) } })
  useEffect(() => {
    if (!q.data || q.isError || form.isDirty() || save.isPending || conflict) return
    // Adopt a new server snapshot only while the editor has no local draft.
    // eslint-disable-next-line react/set-state-in-effect
    setSnapshot(q.data); form.hydrate(policyDraft(q.data.policy, q.data.offline_enabled))
  }, [q.data, q.isError, conflict]) // eslint-disable-line react-hooks/exhaustive-deps
  const reload = useMutation({ mutationFn: async () => { await cancelReads(); return api.get<NodeServiceControl>(serviceControlURL(node.id)) }, onSuccess: result => {
    setSnapshot(result); form.resetDirty(policyDraft(result.policy, result.offline_enabled)); setConflict(false); qc.setQueryData(serviceControlKey(node.id), result)
  }, onError: toast.err })
  const data = q.data ?? snapshot
  const monitorOnly = node.mode === 'monitor' || node.proxy_allowed === false
  const busy = save.isPending || reload.isPending
  const disabled = !snapshot || q.isError || busy || monitorOnly || !node.paired
  const error = data ? policyDraftError(form.values, data) : null
  const stale = !!snapshot && !!q.data && snapshot.revision !== q.data.revision
  const date = (iso?: string) => iso ? new Date(iso).toLocaleString(i18n.language, { timeZone: 'UTC', timeZoneName: 'short' }) : '—'
  return <Card mb="lg" data-testid="traffic-control-policy">
    <Stack>
      <Stack gap={4}><Title order={4}>{t('serviceControl.policyTitle')}</Title><Text size="sm" c="dimmed">{t('serviceControl.policyHint')}</Text></Stack>
      {q.isPending && <Loader size="sm" />}
      {q.isError && <Alert color="red"><Text size="sm">{t('serviceControl.loadError')}</Text><Button size="xs" variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
      {data && <>
        <Text size="sm">{t('serviceControl.savedQuota', { used: bytes(data.traffic.used_bytes), limit: data.traffic.limit_bytes > 0 ? bytes(data.traffic.limit_bytes) : t('serviceControl.noQuota') })}</Text>
        <Text size="xs" c="dimmed">{t('serviceControl.cycle', { start: date(data.traffic.period_start), end: date(data.traffic.period_end) })}</Text>
        {(monitorOnly || !data.capability.supported) && <Alert color="orange">{t(`serviceControl.${monitorOnly ? 'monitorOnly' : controlCapabilityKey(data.capability.reason)}`)}</Alert>}
        {!data.traffic.fresh && <Text size="xs" c="orange">{t('serviceControl.staleTraffic')}</Text>}
      </>}
      <form onSubmit={form.onSubmit(values => { if (!disabled && !error && !stale && !conflict) save.mutate(values) })}>
        <Stack>
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Select label={t('serviceControl.policyAction')} allowDeselect={false} disabled={disabled} value={form.values.action} onChange={value => { if (value) form.setValues({ action: value as PolicyDraft['action'], ...(value === 'alert' ? { offline_enabled: false } : {}), ...(value === 'limit' && !form.values.limit_mbps ? { limit_mbps: 10 } : {}) }) }} data={[
              { value: 'alert', label: t('serviceControl.alert') },
              { value: 'limit', label: t('serviceControl.modes.limit'), disabled: !data?.capability.supported },
              { value: 'pause', label: t('serviceControl.modes.pause'), disabled: !data?.capability.supported },
            ]} />
            {form.values.action !== 'alert' && <NumberInput label={t('serviceControl.threshold')} suffix=" %" min={1} max={100} allowDecimal={false} disabled={disabled} {...form.getInputProps('threshold_percent')} />}
            {form.values.action === 'limit' && <NumberInput label={t('serviceControl.rate')} description={t('serviceControl.rateHint')} min={1} max={1_000_000} allowDecimal={false} disabled={disabled} {...form.getInputProps('limit_mbps')} />}
          </SimpleGrid>
          {form.values.action === 'alert' ? <Text size="xs" c="dimmed">{t('serviceControl.alertHint')}</Text> : <>
            <Switch label={t('serviceControl.autoResume')} description={t('serviceControl.autoResumeHint')} disabled={disabled} {...form.getInputProps('auto_resume', { type: 'checkbox' })} />
            <Switch label={t('serviceControl.offlineProtection')} description={t('serviceControl.offlineProtectionHint')} disabled={disabled || (!form.values.offline_enabled && !data?.capability.offline_supported)} {...form.getInputProps('offline_enabled', { type: 'checkbox' })} />
            {!data?.capability.offline_supported && <Text size="xs" c="dimmed">{t('serviceControl.offlineUpgradeRequired')}</Text>}
            <Text size="xs" c="dimmed">{t('serviceControl.scope')}</Text>
            {form.values.action === 'limit' && <Text size="xs" c="dimmed">{t('serviceControl.policingHint')}</Text>}
          </>}
          {error && <Alert color="yellow">{t(`serviceControl.${error}`)}</Alert>}
          {(conflict || stale) && <Alert color="yellow"><Text size="sm">{t('serviceControl.conflict')}</Text><Button variant="subtle" size="xs" loading={reload.isPending} onClick={() => reload.mutate()}>{t('serviceControl.reload')}</Button></Alert>}
          <Group justify="flex-end"><Button variant="default" disabled={busy || !form.isDirty()} onClick={() => { if (data) { setSnapshot(data); form.setValues(policyDraft(data.policy, data.offline_enabled)); form.resetDirty(policyDraft(data.policy, data.offline_enabled)); setConflict(false) } }}>{t('serviceControl.discard')}</Button><Button type="submit" disabled={disabled || !!error || stale || conflict || !form.isDirty()} loading={save.isPending}>{t('serviceControl.savePolicy')}</Button></Group>
        </Stack>
      </form>
    </Stack>
  </Card>
}
