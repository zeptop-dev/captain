import { Alert, Badge, Button, Card, Checkbox, Group, Loader, Modal, NumberInput, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { pathCandidateEvidence, pathHealthElapsed, pathHealthStatus, validPathHealthPolicy, type PathHealthPolicy, type PathHealthView } from '../lib/path-health'
import type { EvidenceClock } from '../lib/line-change-evidence'

export function PathHealthFields({ value, strategy, onChange }: { value: PathHealthPolicy; strategy: string; onChange: (value: PathHealthPolicy) => void }) {
  const { t } = useTranslation()
  const patch = (next: Partial<PathHealthPolicy>) => onChange({ ...value, ...next })
  const valid = validPathHealthPolicy(value, strategy)
  return <Card withBorder><Stack gap="sm">
    <Title order={5}>{t('pathHealth.title')}</Title>
    <Text size="sm" c="dimmed">{t('pathHealth.scope')}</Text>
    <Checkbox label={t('pathHealth.enable')} checked={value.enabled} onChange={event => patch({ enabled: event.currentTarget.checked })} />
    {!valid && <Alert color="red">{t('pathHealth.invalid')}</Alert>}
    {(value.enabled || !valid) && <>
      <Text size="xs" c="dimmed">{t('pathHealth.requirement')}</Text>
      <Checkbox label={t('pathHealth.auto')} description={t('pathHealth.autoHint')} checked={value.auto_failover} disabled={strategy !== 'failover' && !value.auto_failover} onChange={event => patch({ auto_failover: event.currentTarget.checked })} />
      {value.auto_failover && <Alert color={strategy === 'failover' ? 'yellow' : 'red'}>{t(strategy === 'failover' ? 'pathHealth.allDown' : 'pathHealth.failoverOnly')}</Alert>}
      <TextInput label={t('pathHealth.serverName')} description={t('pathHealth.serverNameHint')} placeholder="example.com" value={value.server_name || ''} onChange={event => patch({ server_name: event.currentTarget.value })} />
      <SimpleGrid cols={{ base: 1, sm: 2 }}>
        <NumberInput label={t('pathHealth.interval')} min={10} max={300} allowDecimal={false} value={value.interval_seconds || ''} onChange={v => patch({ interval_seconds: Number(v) })} />
        <NumberInput label={t('pathHealth.timeout')} min={1} max={10} allowDecimal={false} value={value.timeout_seconds || ''} onChange={v => patch({ timeout_seconds: Number(v) })} />
        <NumberInput label={t('pathHealth.failures')} min={1} max={10} allowDecimal={false} value={value.failure_threshold || ''} onChange={v => patch({ failure_threshold: Number(v) })} />
        <NumberInput label={t('pathHealth.recovery')} min={1} max={10} allowDecimal={false} value={value.recovery_threshold || ''} onChange={v => patch({ recovery_threshold: Number(v) })} />
      </SimpleGrid>
      <Text size="xs" c="dimmed">{t('pathHealth.accounting')}</Text>
    </>}
  </Stack></Card>
}

const color = (status: string) => status === 'healthy' || status === 'up' ? 'green' : status === 'down' ? 'red' : status === 'degraded' ? 'yellow' : 'gray'
const healthClock = (): EvidenceClock => ({ monotonic: performance.now(), wall: Date.now() })

export function PathHealthModal({ routeID, name, onClose }: { routeID: number; name: string; onClose: () => void }) {
  const { t } = useTranslation()
  const [clock, setClock] = useState(healthClock)
  useEffect(() => {
    const tick = () => setClock(healthClock()), timer = window.setInterval(tick, 1000)
    window.addEventListener('offline', tick); window.addEventListener('online', tick); window.addEventListener('focus', tick)
    return () => { window.clearInterval(timer); window.removeEventListener('offline', tick); window.removeEventListener('online', tick); window.removeEventListener('focus', tick) }
  }, [])
  const query = useQuery({
    queryKey: ['path-health', routeID],
    queryFn: async ({ signal }) => {
      const requestedAt = healthClock()
      const view = await api.get<PathHealthView>(`/api/admin/path-routes/${routeID}/health`, signal)
      if (view.route_id !== routeID || !Array.isArray(view.candidates)) throw new Error(t('pathHealth.unavailable'))
      return { view, requestedAt, receivedAt: healthClock() }
    },
    refetchInterval: 10000, refetchIntervalInBackground: false, retry: false, staleTime: 0,
  })
  const view = query.data?.view
  const elapsed = query.data ? pathHealthElapsed(query.data.requestedAt, clock, query.data.receivedAt) : 0
  const available = !!view && navigator.onLine && !query.isError && !query.isPending && query.fetchStatus !== 'paused'
  const status = view ? pathHealthStatus(view, elapsed, available) : 'unknown'
  return <Modal opened onClose={onClose} title={`${t('pathHealth.title')} · ${name}`} size="lg"><Stack>
    <Text size="sm" c="dimmed">{t('pathHealth.scope')}</Text>
    <Group justify="space-between"><Badge color={color(status)}>{t(`pathHealth.states.${status}`)}</Badge><Button variant="default" size="xs" loading={query.isFetching} onClick={() => query.refetch()}>{t('pathHealth.refresh')}</Button></Group>
    {query.isPending && <Loader size="sm" />}
    {query.isError && <Alert color="red">{t('pathHealth.unavailable')}</Alert>}
    {view && <>
      {view.enabled && !view.capable && <Alert color="yellow">{t('pathHealth.requirement')}</Alert>}
      <Text size="sm">{t('pathHealth.mode')}: {t(view.auto_failover ? 'pathHealth.auto' : 'pathHealth.observe')}</Text>
      {view.enabled && <Text size="sm">{t('pathHealth.handshake')}: {t(`pathHealth.protocols.${view.protocol}`, { defaultValue: t('pathHealth.states.unsupported') })}{view.server_name ? ` · ${view.server_name}` : ''}</Text>}
      {view.auto_failover && view.enabled && <Alert color="yellow">{t('pathHealth.allDown')}</Alert>}
      {view.enabled && !view.candidates.length && <Text c="dimmed">{t('pathHealth.noSamples')}</Text>}
      {view.candidates.map((candidate, index) => {
        const evidence = pathCandidateEvidence(candidate, elapsed, available && view.enabled && view.capable && !['paused', 'unknown', 'disabled', 'unsupported'].includes(view.status))
        return <Card key={`${candidate.id}-${candidate.tag}`} withBorder><Stack gap={4}>
          <Group justify="space-between"><Text fw={600}>{t('pathHealth.candidate', { number: index + 1 })}</Text><Badge color={color(evidence.status)}>{t(`pathHealth.states.${evidence.status}`)}</Badge></Group>
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{candidate.target}</Text>
          <Text size="xs" c="dimmed">{t('pathHealth.observed')}: {when(candidate.observed_at)}</Text>
          {evidence.fresh && Number.isFinite(candidate.rtt_ms) && candidate.result === 'success' && <Text size="sm">{t('pathHealth.latency', { value: candidate.rtt_ms })}</Text>}
          {view.enabled && view.auto_failover && <Text size="xs">{t(evidence.eligible ? 'pathHealth.eligible' : 'pathHealth.notEligible')}</Text>}
          {!evidence.fresh && <Text size="xs" c="dimmed">{t('pathHealth.stale')}</Text>}
          {evidence.fresh && candidate.error && <Text size="xs" c="red">{t('pathHealth.handshakeFailed')}</Text>}
        </Stack></Card>
      })}
      <Text size="xs" c="dimmed">{t('pathHealth.accounting')}</Text>
    </>}
  </Stack></Modal>
}
