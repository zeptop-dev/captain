import { Accordion, Alert, Badge, Button, Card, Group, Loader, Stack, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMediaQuery } from '@mantine/hooks'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'
import { dnsCheckWait, dnsFamily, dnsFamilyAnchor, dnsFamilyLabel, dnsFamilyStatus, dnsRecordTypes, dnsSnapshotStale, dnsStatusColor, type DNSAnswer, type DNSHealth, type DNSRecord } from '../lib/dns-health'
import { useDNSClock, useDNSHealth } from '../lib/use-dns-health'
import { DNSSyncModal } from './DNSSyncModal'

interface DNSChange { id: number; node_id: number | null; source: string; actor: string; zone: string; name: string; action: string; before: DNSRecord | null; wanted: DNSRecord; after: DNSRecord | null; outcome: string; created_at: number; finished_at: number }
const date = (at: number) => at ? new Date(at * 1000).toLocaleString() : '—'
const failure = (status: string) => ['mismatch', 'missing', 'conflict', 'invalid'].includes(status)

function DNSResult({ health, now, unavailable }: { health: DNSHealth; now: number; unavailable: boolean }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const location = useLocation()
  const mobile = useMediaQuery('(max-width: 47.99em)')
  const focused = useRef('')
  const [sync, setSync] = useState(false)
  const stale = unavailable || dnsSnapshotStale(health, now)
  const wait = dnsCheckWait(health.checked_at, now)
  const check = useMutation({ mutationFn: () => api.post<DNSHealth>(`/api/admin/nodes/${health.node_id}/dns-health/check`), onSuccess: result => {
    qc.setQueryData(['dns-health', health.node_id], [result])
    qc.invalidateQueries({ queryKey: ['dns-health'] }); qc.invalidateQueries({ queryKey: ['selfcheck'] })
  }, onError: toast.err })
  useEffect(() => {
    if (!location.hash || focused.current === `${location.hash}:${mobile}`) return
    const element = document.getElementById(location.hash.slice(1))
    if (element) { element.scrollIntoView({ block: 'center' }); focused.current = `${location.hash}:${mobile}` }
  }, [location.hash, health.targets, mobile])
  const answerText = (answer?: DNSAnswer) => answer?.error ? t(`dnsHealth.errors.${answer.error}`, { defaultValue: t('dnsHealth.errors.unavailable') }) : answer?.addresses?.length ? answer.addresses.join(', ') : '—'
  return <Stack gap="sm">
    <Group justify="space-between" align="flex-start"><Group gap="xs"><Text size="xs" c="dimmed">{t('dnsHealth.checked', { at: date(health.checked_at) })}</Text>{stale && <Badge color="gray">{t('dnsHealth.stale')}</Badge>}</Group><Group gap="xs">
      <Button size="xs" variant="light" loading={check.isPending} disabled={wait > 0} onClick={() => check.mutate()}>{t('dnsHealth.recheck')}{wait > 0 ? ` (${wait}s)` : ''}</Button>
      {me?.role === 'admin' && <Button size="xs" variant="outline" onClick={() => setSync(true)}>{t('dnsHealth.sync.title')}</Button>}
    </Group></Group>
    {health.targets.length === 0 && <Text size="sm" c="dimmed">{t('dnsHealth.empty')}</Text>}
    {health.targets.map(target => <Card key={target.key} withBorder padding="sm">
      <Stack gap="xs">
        <Group justify="space-between" align="flex-start"><div style={{ minWidth: 0 }}><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{target.host}</Text><Text size="xs" c="dimmed">{target.label} · {t(`dnsHealth.modes.${target.mode}`)}</Text></div>{['invalid', 'conflict'].includes(target.status) && <Badge color={stale ? 'gray' : 'red'}>{t(`dnsHealth.statuses.${stale ? 'stale' : target.status}`)}</Badge>}</Group>
        {mobile ? <Stack gap="xs">{dnsRecordTypes.map(type => {
          const family = dnsFamily(target, type), status = dnsFamilyStatus(family, stale, target.mode)
          const anchor = dnsFamilyAnchor(health.node_id, target.key, type)
          return <Card key={type} id={anchor} withBorder p="sm" style={{ scrollMarginTop: 100, background: location.hash === `#${anchor}` ? 'var(--mantine-primary-color-light)' : undefined }}><Stack gap="xs">
            <Group gap="xs" justify="space-between"><Text size="sm" fw={600}>{dnsFamilyLabel(type)}</Text><Badge color={dnsStatusColor(status)}>{t(`dnsHealth.statuses.${status}`, { defaultValue: t('dnsHealth.statuses.unknown') })}</Badge></Group>
            <div><Text size="xs" c="dimmed">{t('dnsHealth.expected')}</Text><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{family.expected.length ? family.expected.join(', ') : t(target.mode === 'direct' ? 'nodeAddresses.unregistered' : 'dnsHealth.resolveOnly')}</Text></div>
            {['panel', 'cloudflare'].map(source => <div key={source}><Text size="xs" c="dimmed">{t(`dnsHealth.sources.${source}`)}</Text><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{answerText(family.answers.find(answer => answer.source === source))}</Text></div>)}
          </Stack></Card>
        })}</Stack> : <Table.ScrollContainer minWidth={650}><Table verticalSpacing="sm">
          <Table.Thead><Table.Tr><Table.Th>{t('dnsHealth.record')}</Table.Th><Table.Th>{t('dnsHealth.expected')}</Table.Th><Table.Th>{t('dnsHealth.sources.panel')}</Table.Th><Table.Th>{t('dnsHealth.sources.cloudflare')}</Table.Th><Table.Th>{t('nodes.status')}</Table.Th></Table.Tr></Table.Thead>
          <Table.Tbody>{dnsRecordTypes.map(type => {
            const family = dnsFamily(target, type), status = dnsFamilyStatus(family, stale, target.mode)
            const anchor = dnsFamilyAnchor(health.node_id, target.key, type)
            return <Table.Tr key={type} id={anchor} style={{ scrollMarginTop: 100, background: location.hash === `#${anchor}` ? 'var(--mantine-primary-color-light)' : undefined }}>
              <Table.Td><Text size="sm" style={{ whiteSpace: 'nowrap' }}>{dnsFamilyLabel(type)}</Text></Table.Td>
              <Table.Td style={{ maxWidth: 220 }}><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{family.expected.length ? family.expected.join(', ') : t(target.mode === 'direct' ? 'nodeAddresses.unregistered' : 'dnsHealth.resolveOnly')}</Text></Table.Td>
              {['panel', 'cloudflare'].map(source => <Table.Td key={source} style={{ maxWidth: 220 }}><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{answerText(family.answers.find(answer => answer.source === source))}</Text></Table.Td>)}
              <Table.Td><Badge color={dnsStatusColor(status)}>{t(`dnsHealth.statuses.${status}`, { defaultValue: t('dnsHealth.statuses.unknown') })}</Badge></Table.Td>
            </Table.Tr>
          })}</Table.Tbody>
        </Table></Table.ScrollContainer>}
        {target.propagation_pending && <Text size="xs" c="orange">{t('dnsHealth.propagationPending')}</Text>}
        {target.families?.some(family => family.status === 'inconsistent') && <Text size="xs" c="orange">{t('dnsHealth.inconsistentHint')}</Text>}
        {target.families?.some(family => family.status === 'unexpected') && <Text size="xs" c="orange">{t('dnsHealth.unexpectedHint')}</Text>}
      </Stack>
    </Card>)}
    {sync && <DNSSyncModal nodeID={health.node_id} close={() => setSync(false)} />}
  </Stack>
}

function DNSHistory({ nodeID }: { nodeID?: number }) {
  const { t } = useTranslation()
  const [before, setBefore] = useState(0)
  const q = useQuery({ queryKey: ['dns-history', nodeID, before], queryFn: () => api.get<{ items: DNSChange[]; more: boolean }>(`/api/admin/settings/dns/history?node_id=${nodeID ?? 0}&before=${before}`), refetchInterval: 30_000 })
  const record = (r: DNSRecord | null) => r ? <Stack gap={2}><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{r.type} · {r.content}</Text><Text size="xs" c="dimmed">TTL: {r.ttl === 1 ? t('dnsHealth.auto') : r.ttl} · {t('dnsHealth.proxy')}: {t(r.proxied ? 'dnsHealth.on' : 'dnsHealth.off')}</Text>{r.id && <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>ID: {r.id}</Text>}</Stack> : <Text size="sm" c="dimmed">—</Text>
  return <Stack gap="sm">
    <Text size="sm" c="dimmed">{t('dnsHealth.historyHint')}</Text>
    {q.isLoading && <Loader size="sm" />}{q.isError && <Alert color="red">{t('dnsHealth.loadError')}</Alert>}
    {q.data && q.data.items.length === 0 && <Text size="sm" c="dimmed">{t('dnsHealth.noHistory')}</Text>}
    {!!q.data?.items.length && <Table.ScrollContainer minWidth={800}><Table verticalSpacing="sm"><Table.Thead><Table.Tr>{['change', 'before', 'wanted', 'after'].map(key => <Table.Th key={key}>{t(`dnsHealth.${key}`)}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>{q.data.items.map(change => <Table.Tr key={change.id}>
      <Table.Td style={{ maxWidth: 260 }}><Stack gap={3}><Text size="sm" fw={600} style={{ overflowWrap: 'anywhere' }}>{change.name}</Text><Badge color={change.outcome === 'success' ? 'teal' : 'orange'}>{t(`dnsHealth.outcomes.${change.outcome}`)}</Badge><Text size="xs">{t(`dnsHealth.actions.${change.action}`)} · {date(change.created_at)}</Text><Text size="xs" c="dimmed">{t(change.source === 'manual_sync' ? 'dnsHealth.manualSource' : 'dnsHealth.automaticSource')}</Text><Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{change.actor || '—'}</Text>{change.node_id && <Text component={Link} size="xs" to={`/nodes/${change.node_id}`}>{t('dnsHealth.node', { id: change.node_id })}</Text>}</Stack></Table.Td>
      <Table.Td>{record(change.before)}</Table.Td><Table.Td>{record(change.wanted)}</Table.Td><Table.Td>{record(change.after)}</Table.Td>
    </Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>}
    <Group justify="flex-end">{before > 0 && <Button variant="subtle" size="xs" onClick={() => setBefore(0)}>{t('dnsHealth.latest')}</Button>}{q.data?.more && <Button variant="light" size="xs" onClick={() => setBefore(q.data!.items[q.data!.items.length - 1].id)}>{t('dnsHealth.older')}</Button>}</Group>
  </Stack>
}

export function DNSHealthCard({ nodeID }: { nodeID?: number }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const [history, setHistory] = useState(false)
  const q = useDNSHealth(nodeID)
  const clock = useDNSClock()
  const now = Math.max(clock, q.dataUpdatedAt / 1000)
  return <Card mb={nodeID ? 'lg' : 0} style={{ minWidth: 0 }} id={nodeID ? 'dns-health' : undefined}><Stack>
    <Title order={5}>{t('dnsHealth.title')}</Title><Text size="sm" c="dimmed">{t('dnsHealth.hint')}</Text>
    <Text size="xs" c="dimmed">{t('dnsHealth.scopeHint')}</Text>
    {q.isLoading && <Loader size="sm" />}{q.isError && <Alert color="red">{t('dnsHealth.loadError')}</Alert>}
    {q.data?.length === 0 && <Text c="dimmed" size="sm">{t('dnsHealth.empty')}</Text>}
    {nodeID ? q.data?.map(health => <DNSResult key={health.node_id} health={health} now={now} unavailable={q.isError} />) : <Accordion variant="separated">{q.data?.map(health => {
      const status = q.isError || dnsSnapshotStale(health, now) ? 'stale' : health.targets.some(target => failure(target.status)) ? 'problem' : health.targets.length && health.targets.every(target => target.status === 'ok') ? 'ok' : 'unknown'
      return <Accordion.Item key={health.node_id} value={String(health.node_id)}><Accordion.Control><Group gap="xs"><Text fw={600}>{health.node}</Text><Badge color={dnsStatusColor(status)}>{t(`dnsHealth.statuses.${status}`)}</Badge></Group></Accordion.Control><Accordion.Panel><DNSResult health={health} now={now} unavailable={q.isError} /></Accordion.Panel></Accordion.Item>
    })}</Accordion>}
    {me?.role === 'admin' && <><Button variant="subtle" size="sm" style={{ alignSelf: 'flex-start' }} onClick={() => setHistory(!history)}>{t('dnsHealth.history')}</Button>{history && <DNSHistory key={nodeID ?? "all"} nodeID={nodeID} />}</>}
  </Stack></Card>
}
