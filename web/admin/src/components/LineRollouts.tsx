import { Accordion, Alert, Badge, Button, Card, Code, Group, Loader, Modal, NumberInput, Stack, Text, Title } from '@mantine/core'
import { useMediaQuery } from '@mantine/hooks'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { evidenceIsFresh, evidenceLease, type EvidenceClock } from '../lib/line-change-evidence'
import { rolloutHistoryURL, rolloutReviewKey, type LineRollout, type LineRolloutDraft, type LineRolloutNode, type LineRolloutPage, type LineRolloutPreview, type LineRolloutTarget } from '../lib/line-rollouts'

export function LineRolloutPlanner({ draft, remove = false, onClose, onCreated }: { draft: LineRolloutDraft; remove?: boolean; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useTranslation()
  const mobile = useMediaQuery('(max-width: 48em)')
  const [seconds, setSeconds] = useState(300)
  const [review, setReview] = useState<{ key: string; value: LineRolloutPreview } | null>(null)
  const key = rolloutReviewKey(draft, remove, seconds)
  const preview = useMutation({ mutationFn: async () => ({ key, value: await api.post<LineRolloutPreview>('/api/admin/line-rollouts/preview', { line: draft, remove, timeout_seconds: seconds }) }), onSuccess: setReview })
  const qc = useQueryClient()
  const save = useMutation({ mutationFn: async () => {
    if (!review || review.key !== key) throw new Error(t('lineRollouts.reviewAgain'))
    return api.post<LineRollout>('/api/admin/line-rollouts', { line: { ...review.value.draft, preview_revision: review.value.preview_revision }, remove, timeout_seconds: seconds })
  }, onSuccess: plan => {
    for (const queryKey of [['lines'], ['entries'], ['line-rollouts'], ['line-changes']]) void qc.invalidateQueries({ queryKey })
    toast.ok(t('lineRollouts.started')); onCreated(plan.id)
  }, onError: () => setReview(null) })
  const busy = preview.isPending || save.isPending
  const current = review?.key === key
  return <Modal opened onClose={() => { if (!save.isPending) onClose() }} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={t(remove ? 'lineRollouts.remove' : 'lineRollouts.plan')} size="xl" fullScreen={mobile}>
    <Stack>
      <Alert color="blue">{t('lineRollouts.scope')}</Alert>
      <Text fw={600}>{draft.name}</Text>
      <NumberInput label={t('lineRollouts.timeout')} description={t('lineRollouts.timeoutHint')} min={30} max={3600} allowDecimal={false} value={seconds} disabled={busy} onChange={v => setSeconds(typeof v === 'number' ? v : 300)} />
      {preview.isError && <Alert color="red">{preview.error.message}</Alert>}
      {save.isError && <Alert color="red">{save.error.message}</Alert>}
      {review && <>
        {!current && <Alert color="yellow">{t('lineRollouts.reviewAgain')}</Alert>}
        <Title order={5}>{t('lineRollouts.steps')}</Title>
        {review.value.targets.map((step, i) => <Text key={`${step.node_id}-${step.phase}-${i}`} size="sm">{i + 1}. {step.node_name || `#${step.node_id}`} · {t(`lineRollouts.phase.${step.phase}`)}</Text>)}
        {review.value.targets.length === 0 && <Text size="sm">{t('lineRollouts.noSteps')}</Text>}
        <Snapshots nodes={review.value.nodes} />
      </>}
      <Group justify="flex-end"><Button variant="default" onClick={onClose} disabled={save.isPending}>{t('common.cancel')}</Button><Button variant="light" loading={preview.isPending} disabled={save.isPending} onClick={() => preview.mutate()}>{t('lineRollouts.preview')}</Button><Button disabled={!current || busy} loading={save.isPending} onClick={() => save.mutate()}>{t('lineRollouts.start')}</Button></Group>
    </Stack>
  </Modal>
}

const clock = (): EvidenceClock => ({ monotonic: performance.now(), wall: Date.now() })
type Observed = LineRollout & { started: EvidenceClock }

export function LineRolloutHistory({ initialID, lineID, onClose }: { initialID?: string; lineID?: number; onClose: () => void }) {
  const { t } = useTranslation()
  const mobile = useMediaQuery('(max-width: 48em)')
  const qc = useQueryClient()
  const [id, setID] = useState(initialID)
  const [pages, setPages] = useState<(number | undefined)[]>([undefined])
  const before = pages[pages.length - 1]
  const [now, setNow] = useState(clock)
  useEffect(() => {
    const tick = () => setNow(clock()), timer = window.setInterval(tick, 1000)
    window.addEventListener('offline', tick); window.addEventListener('online', tick); window.addEventListener('focus', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('online', tick); window.removeEventListener('focus', tick) }
  }, [])
  const history = useQuery({ queryKey: ['line-rollouts', lineID, before], queryFn: ({ signal }) => api.get<LineRolloutPage>(rolloutHistoryURL(lineID, before), signal), retry: false, refetchInterval: 5000 })
  const activeID = id ?? history.data?.items[0]?.id
  const detail = useQuery({ queryKey: ['line-rollout', activeID], queryFn: async ({ signal }): Promise<Observed> => {
    const started = clock(); return { ...await api.get<LineRollout>(`/api/admin/line-rollouts/${encodeURIComponent(activeID!)}`, signal), started }
  }, enabled: !!activeID, retry: false, refetchInterval: 5000 })
  const connected = navigator.onLine && !detail.isError && detail.fetchStatus !== 'paused'
  const act = useMutation({ mutationFn: (action: string) => api.post<LineRollout>(`/api/admin/line-rollouts/${encodeURIComponent(activeID!)}/${action}`, { version: detail.data!.version }), onSuccess: result => {
    setID(result.id); void qc.invalidateQueries({ queryKey: ['line-rollouts'] }); void qc.invalidateQueries({ queryKey: ['line-rollout'] }); void qc.invalidateQueries({ queryKey: ['lines'] }); void qc.invalidateQueries({ queryKey: ['entries'] })
  } })
  const action = (kind: string) => modals.openConfirmModal({ title: t(`lineRollouts.action.${kind}`), children: <Text size="sm">{t(`lineRollouts.actionHint.${kind}`)}</Text>, labels: { confirm: t(`lineRollouts.action.${kind}`), cancel: t('common.cancel') }, onConfirm: () => act.mutate(kind) })
  const turnPage = (next: (number | undefined)[]) => { setPages(next); setID(undefined) }
  return <Modal opened onClose={onClose} title={t('lineRollouts.history')} size="xl" fullScreen={mobile} closeButtonProps={{ 'aria-label': t('common.cancel') }}>
    <Stack>
      <Alert color="blue">{t('lineRollouts.scope')}</Alert>
      {history.isError && <Alert color="red">{t('lineRollouts.loadError')}</Alert>}
      <Group gap="xs">{history.data?.items.map(plan => <Button key={plan.id} size="xs" variant={activeID === plan.id ? 'filled' : 'light'} onClick={() => setID(plan.id)}>{plan.after?.name ?? plan.before?.name ?? `#${plan.line_id}`} · {t(`lineRollouts.status.${plan.status}`)}</Button>)}</Group>
      <Group justify="space-between"><Button size="xs" variant="default" disabled={pages.length === 1 || history.isFetching} onClick={() => turnPage(pages.slice(0, -1))}>{t('lineChanges.newer')}</Button><Button size="xs" variant="default" disabled={!history.data?.next_before || history.isFetching || history.isError} onClick={() => turnPage([...pages, history.data!.next_before])}>{t('lineChanges.older')}</Button></Group>
      {detail.isLoading && <Loader size="sm" />}
      {activeID && !connected && <Alert color="red">{t('lineRollouts.loadError')}</Alert>}
      {!activeID && !history.isLoading && <Text c="dimmed">{t('lineRollouts.empty')}</Text>}
      {act.isError && <Alert color="red">{act.error.message}</Alert>}
      {detail.data && <>
        <Group justify="space-between"><Title order={4}>{detail.data.after?.name ?? detail.data.before?.name ?? `#${detail.data.line_id}`}</Title><Badge color={connected && detail.data.status === 'completed' ? 'blue' : 'gray'}>{connected ? t(`lineRollouts.status.${detail.data.status}`) : t('nodeConfiguration.status.unknown')}</Badge></Group>
        <Text size="sm">{t('lineRollouts.historical')}</Text>
        <Text size="xs" c="dimmed">{t('lineChanges.actor', { actor: detail.data.actor })} · {when(detail.data.created_at)}</Text>
        {detail.data.published_at && <Text size="sm">{t('lineRollouts.published', { at: when(detail.data.published_at) })}</Text>}
        {detail.data.error_code && <Alert color="yellow">{t('lineRollouts.pausedReason')} <Code>{detail.data.error_code}</Code></Alert>}
        <Group gap="xs">
          <Button variant="default" disabled={!connected || detail.isFetching || act.isPending} onClick={() => { void history.refetch(); void detail.refetch() }}>{t('common.refresh')}</Button>
          {detail.data.status === 'running' && <Button color="orange" disabled={!connected || act.isPending} onClick={() => action('pause')}>{t('lineRollouts.action.pause')}</Button>}
          {detail.data.status === 'paused' && <Button disabled={!connected || act.isPending} onClick={() => action('retry')}>{t('lineRollouts.action.retry')}</Button>}
          {detail.data.status !== 'compensated' && <Button variant="light" color="orange" disabled={!connected || act.isPending} onClick={() => action('compensate')}>{t('lineRollouts.action.compensate')}</Button>}
        </Group>
        <Title order={5}>{t('lineRollouts.steps')}</Title>
        {detail.data.targets.map(target => <Step key={target.id} target={target} fresh={evidenceIsFresh(evidenceLease(detail.data.started, target.fresh_for_ms), now, connected) && target.fresh && target.receipt_applied} connected={connected} />)}
        <Snapshots nodes={detail.data.nodes} />
      </>}
    </Stack>
  </Modal>
}

function Step({ target, fresh, connected }: { target: LineRolloutTarget; fresh: boolean; connected: boolean }) {
  const { t } = useTranslation()
  return <Card withBorder padding="sm"><Stack gap={5}>
    <Group justify="space-between"><Text fw={600}>{target.order}. {target.node_name || `#${target.node_id}`}</Text><Badge color={fresh ? 'teal' : 'gray'}>{!connected ? t('nodeConfiguration.status.unknown') : fresh ? t('lineRollouts.fresh') : t(`lineRollouts.stepStatus.${target.status}`)}</Badge></Group>
    <Text size="sm">{t(`lineRollouts.phase.${target.phase}`)} · {t('lineRollouts.attempt', { count: target.attempt })}</Text>
    <Text size="xs" c="dimmed">{t('lineChanges.confirmed', { at: when(target.confirmed_at) })}</Text>
    {target.deadline_at && <Text size="xs" c="dimmed">{t('lineRollouts.deadline', { at: when(target.deadline_at) })}</Text>}
    <Accordion><Accordion.Item value="expected"><Accordion.Control>{t('lineChanges.details')}</Accordion.Control><Accordion.Panel><Text size="xs" style={{ overflowWrap: 'anywhere' }}>{target.delivery_id || '—'}</Text><Code block style={{ maxHeight: 240, overflow: 'auto' }}>{JSON.stringify(target.expected, null, 2)}</Code></Accordion.Panel></Accordion.Item></Accordion>
  </Stack></Card>
}

function Snapshots({ nodes }: { nodes: LineRolloutNode[] }) {
  const { t } = useTranslation()
  return <Accordion variant="contained">{nodes.map(node => <Accordion.Item key={node.node_id} value={String(node.node_id)}><Accordion.Control>{node.node_name || `#${node.node_id}`} · {t('lineRollouts.snapshot')}</Accordion.Control><Accordion.Panel><Stack>{(['before', 'after'] as const).map(side => <div key={side}><Text size="sm" fw={600}>{t(`lineChanges.${side}`)}</Text><Code block style={{ maxHeight: 240, overflow: 'auto' }}>{JSON.stringify(node[side], null, 2)}</Code></div>)}</Stack></Accordion.Panel></Accordion.Item>)}</Accordion>
}
