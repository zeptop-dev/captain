import { Alert, Badge, Button, Checkbox, Group, Loader, Modal, NumberInput, Select, Stack, Text } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, ApiError, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useSettingsDirty } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { useNodeServiceControl } from '../lib/use-node-service-control'
import { controlCapabilityKey, controlStatusColor, manualDraft, manualDraftError, manualWrite, serviceControlKey, serviceControlURL, type ManualDraft, type NodeServiceControl } from '../lib/node-service-control'

export function ServiceControlState({ data, compact = false }: { data: NodeServiceControl; compact?: boolean }) {
  const { t, i18n } = useTranslation()
  const date = (iso?: string) => iso ? new Date(iso).toLocaleString(i18n.language, { timeZone: 'UTC', timeZoneName: 'short' }) : '—'
  const automaticRecovery = data.automatic.active && data.policy.auto_resume && data.manual.mode === 'normal'
  return <Stack gap={compact ? 4 : 'xs'} data-testid="service-control-state">
    <Group gap="xs"><Text size="sm" fw={600}>{t('serviceControl.requested')}: {t(`serviceControl.modes.${data.effective.mode}`)}{data.effective.mode === 'limit' ? ` · ${data.effective.limit_mbps} Mbps` : ''}</Text><Badge color={controlStatusColor(data.runtime.status)}>{t(`serviceControl.status.${data.runtime.status}`)}</Badge></Group>
    <Text size="xs" c="dimmed">{t('serviceControl.manualState', { mode: t(`serviceControl.modes.${data.manual.mode}`) })} · {t('serviceControl.automaticState', { mode: data.automatic.exempt_current_period ? t('serviceControl.exempted') : data.automatic.active ? t(`serviceControl.modes.${data.automatic.mode}`) : t('serviceControl.notTriggered') })}</Text>
    {data.automatic.active && data.automatic.triggered_at && <Text size="xs" c="dimmed">{t('serviceControl.triggeredAt', { date: date(data.automatic.triggered_at) })}</Text>}
    {automaticRecovery && <Text size="xs" c="dimmed">{data.traffic.period_end ? t('serviceControl.expectedRecovery', { date: date(data.traffic.period_end) }) : t('serviceControl.nextCycleRecovery')}</Text>}
    {data.manual.mode !== 'normal' && <Text size="xs" c="dimmed">{t('serviceControl.manualPersists')}</Text>}
    {data.runtime.status === 'pending' && <Text size="xs" c="blue">{t('serviceControl.pendingHint')}</Text>}
    {data.runtime.status === 'unknown' && <Text size="xs" c="dimmed">{t('serviceControl.unknownHint')}</Text>}
    {data.runtime.observed_at && <Text size="xs" c="dimmed">{t('serviceControl.reportedAt', { date: date(data.runtime.observed_at) })}</Text>}
    {data.runtime.error && <Text size="xs" c="red" style={{ overflowWrap: 'anywhere' }}>{data.runtime.error}</Text>}
  </Stack>
}

export function NodeServiceControlSummary({ nodeID }: { nodeID: number }) {
  const { me } = useAuth()
  const { t } = useTranslation()
  const q = useNodeServiceControl(nodeID)
  if (me?.role !== 'admin') return null
  return <Stack mt="md" gap="xs">
    {q.isPending ? <Loader size="xs" /> : q.isError || !q.data ? <Alert color="orange"><Text size="xs">{t('serviceControl.loadError')}</Text><Button size="xs" variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert> : <ServiceControlState data={q.data} compact />}
  </Stack>
}

export function NodeServiceControlButton({ node }: { node: Node }) {
  const { me } = useAuth()
  if (me?.role !== 'admin') return null
  return <ServiceControlButton node={node} />
}

function ServiceControlButton({ node }: { node: Node }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useNodeServiceControl(node.id)
  const [opened, setOpened] = useState(false)
  const [closing, setClosing] = useState(false)
  const [snapshot, setSnapshot] = useState<NodeServiceControl | null>(null)
  const [draft, setDraft] = useState<ManualDraft>({ mode: 'normal', limit_mbps: 10, exempt_current_period: false })
  const [conflict, setConflict] = useState(false)
  const dirty = !!snapshot && JSON.stringify(draft) !== JSON.stringify(manualDraft(snapshot))
  useSettingsDirty(opened && dirty)
  const monitorOnly = node.mode === 'monitor' || node.proxy_allowed === false
  const source = q.data ?? snapshot
  const error = source ? manualDraftError(draft, source) : 'loadError'
  const stale = !!snapshot && !!q.data && snapshot.revision !== q.data.revision
  useEffect(() => {
    // Seed a newly opened modal after its asynchronous query recovers.
    // eslint-disable-next-line react/set-state-in-effect
    if (opened && !snapshot && q.data) { setSnapshot(q.data); setDraft(manualDraft(q.data)) }
  }, [opened, snapshot, q.data])
  const cancelReads = () => qc.cancelQueries({ queryKey: serviceControlKey(node.id) })
  const save = useMutation({ onMutate: cancelReads, mutationFn: () => api.put<NodeServiceControl>(serviceControlURL(node.id), manualWrite(snapshot!.revision, draft)), onSuccess: async result => {
    await cancelReads()
    qc.setQueryData(serviceControlKey(node.id), result)
    setSnapshot(result); setDraft(manualDraft(result)); setOpened(false); setConflict(false)
    toast.ok(t('serviceControl.requestSaved'))
  }, onError: error => { if (error instanceof ApiError && error.status === 409) setConflict(true); else toast.err(error) } })
  const reload = useMutation({ mutationFn: async () => { await cancelReads(); return api.get<NodeServiceControl>(serviceControlURL(node.id)) }, onSuccess: result => {
    qc.setQueryData(serviceControlKey(node.id), result); setSnapshot(result); setConflict(false)
  }, onError: toast.err })
  const close = () => { if (save.isPending || reload.isPending) return; if (dirty) setClosing(true); else setOpened(false) }
  const busy = save.isPending || reload.isPending
  return <>
    <Button size="xs" variant="light" disabled={q.isPending || !node.paired} onClick={() => { if (q.data) { setSnapshot(q.data); setDraft(manualDraft(q.data)); setConflict(false) }; setOpened(true) }}>{t('serviceControl.title')}</Button>
    <Modal opened={opened} onClose={close} title={t('serviceControl.title')} closeButtonProps={{ 'aria-label': t('serviceControl.close') }} centered size="lg">
      <Stack>
        <Text size="sm" c="dimmed">{t('serviceControl.scope')}</Text>
        {q.isError || !source ? <Alert color="red"><Text size="sm">{t('serviceControl.loadError')}</Text><Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert> : <>
          <ServiceControlState data={source} />
          {(monitorOnly || !source.capability.supported) && <Alert color="orange">{t(`serviceControl.${monitorOnly ? 'monitorOnly' : controlCapabilityKey(source.capability.reason)}`)}</Alert>}
          <Select label={t('serviceControl.manualAction')} value={draft.mode} allowDeselect={false} disabled={monitorOnly || busy || !snapshot} onChange={value => { if (value) setDraft(current => ({ ...current, mode: value as ManualDraft['mode'], limit_mbps: current.limit_mbps || 10 })) }} data={[
            { value: 'normal', label: t('serviceControl.restore') },
            { value: 'limit', label: t('serviceControl.modes.limit'), disabled: !source.capability.supported },
            { value: 'pause', label: t('serviceControl.modes.pause'), disabled: !source.capability.supported },
          ]} />
          {draft.mode === 'limit' && <><NumberInput label={t('serviceControl.rate')} description={t('serviceControl.rateHint')} min={1} max={1_000_000} allowDecimal={false} value={draft.limit_mbps} disabled={monitorOnly || busy} onChange={value => setDraft(current => ({ ...current, limit_mbps: value }))} /><Text size="xs" c="dimmed">{t('serviceControl.policingHint')}</Text></>}
          {draft.mode === 'pause' && <Alert color="orange">{t('serviceControl.pauseHint')}</Alert>}
          {draft.mode === 'normal' && (source.automatic.active || source.automatic.exempt_current_period) && <Checkbox label={t('serviceControl.exempt')} description={t('serviceControl.exemptHint')} checked={draft.exempt_current_period} disabled={monitorOnly || busy} onChange={event => setDraft(current => ({ ...current, exempt_current_period: event.currentTarget.checked }))} />}
          {error === 'exemptionRequired' && <Alert color="yellow"><Text size="sm">{t('serviceControl.exemptionRequired')}</Text><Button component={Link} to={`/nodes/${node.id}/information`} size="xs" variant="subtle">{t('serviceControl.editQuota')}</Button></Alert>}
          {error && error !== 'exemptionRequired' && error !== 'unavailable' && <Text size="sm" c="red">{t(`serviceControl.${error}`)}</Text>}
          {(conflict || stale) && <Alert color="yellow"><Text size="sm">{t('serviceControl.conflict')}</Text><Button size="xs" variant="subtle" loading={reload.isPending} onClick={() => reload.mutate()}>{t('serviceControl.reload')}</Button></Alert>}
          <Group justify="flex-end"><Button variant="default" disabled={busy} onClick={close}>{t('common.cancel')}</Button><Button disabled={monitorOnly || !snapshot || !!error || conflict || stale || !dirty || busy || q.isError} loading={save.isPending} onClick={() => save.mutate()}>{t('serviceControl.apply')}</Button></Group>
        </>}
      </Stack>
    </Modal>
    <Modal opened={closing} onClose={() => setClosing(false)} title={t('workspace.unsaved')} closeButtonProps={{ 'aria-label': t('serviceControl.close') }} centered>
      <Text size="sm">{t('workspace.leaveHint')}</Text><Group justify="flex-end" mt="md"><Button variant="default" onClick={() => setClosing(false)}>{t('workspace.stay')}</Button><Button color="red" onClick={() => { setClosing(false); setOpened(false) }}>{t('workspace.discard')}</Button></Group>
    </Modal>
  </>
}
