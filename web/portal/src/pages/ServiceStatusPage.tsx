import { Alert, Badge, Button, Card, Group, Skeleton, Stack, Text, Title } from '@mantine/core'
import { IconChevronDown, IconRefresh } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { currentEntries, fetchServiceStatus, headlineKey, snapshotExpired, statusCategories, statusColors, statusEvidence, statusStates, summarize, type StatusState } from '../lib/service-status'
import classes from './ServiceStatusPage.module.css'

export default function ServiceStatusPage() {
  const { t, i18n } = useTranslation()
  const [now, setNow] = useState(() => Date.now() / 1000)
  const [online, setOnline] = useState(() => navigator.onLine)
  const [category, setCategory] = useState('all')
  const query = useQuery({
    queryKey: ['service-status'], queryFn: ({ signal }) => fetchServiceStatus('/api/portal/service-status', signal),
    retry: false, staleTime: 0, gcTime: 0, refetchInterval: 60_000, refetchOnWindowFocus: 'always', refetchOnReconnect: 'always',
    meta: { handlesError: true },
  })
  const { refetch } = query
  useEffect(() => {
    const tick = () => setNow(Date.now() / 1000)
    const visible = () => { tick(); if (document.visibilityState === 'visible') void refetch() }
    const connection = () => { setOnline(navigator.onLine); tick() }
    const interval = window.setInterval(tick, 10_000)
    document.addEventListener('visibilitychange', visible)
    window.addEventListener('online', connection)
    window.addEventListener('offline', connection)
    return () => { window.clearInterval(interval); document.removeEventListener('visibilitychange', visible); window.removeEventListener('online', connection); window.removeEventListener('offline', connection) }
  }, [refetch])
  // A completed response advances the clock immediately, without waiting for the next timer tick.
  const at = Math.max(now, query.dataUpdatedAt / 1000)
  useEffect(() => {
    if (!query.data) return
    const current = Date.now() / 1000
    const deadlines = [query.data.generated_at + 180, ...query.data.entries.flatMap(entry => [entry.observed_at ? entry.observed_at + 180 : 0, entry.maintenance?.ends_at ?? 0])].filter(value => Number.isFinite(value) && value > current)
    if (!deadlines.length) return
    const timer = window.setTimeout(() => { setNow(Date.now() / 1000); void refetch() }, Math.min(2_147_483_647, Math.max(1, (Math.min(...deadlines) - current) * 1000 + 50)))
    return () => window.clearTimeout(timer)
  }, [query.data, refetch, now])
  const unavailable = query.isError || !online
  const data = !unavailable ? query.data : undefined
  const entries = data ? currentEntries(data, at) : []
  const summary = summarize(entries)
  const categories = statusCategories(entries)
  const options = [{ value: 'all', name: t('serviceStatus.all'), description: '' }, ...categories.map(value => ({ value: String(value.id), name: value.name, description: value.description }))]
  if (entries.some(entry => !entry.category)) options.push({ value: '0', name: t('servers.uncategorized'), description: '' })
  const activeCategory = options.some(option => option.value === category) ? category : 'all'
  const visibleEntries = activeCategory === 'all' ? entries : entries.filter(entry => String(entry.category?.id ?? 0) === activeCategory)
  const description = options.find(option => option.value === activeCategory)?.description
  const colorStyle = (state: StatusState) => ({ '--status-color': `var(--mantine-color-${statusColors[state]}-6)` } as CSSProperties)
  const time = (timestamp: number | null) => timestamp && Number.isFinite(timestamp) ? new Date(timestamp * 1000).toLocaleString(i18n.language) : t('serviceStatus.never')
  return <Stack gap="xl">
    <Group justify="space-between" align="center">
      <Title order={1} fz={30}>{t('serviceStatus.title')}</Title>
      <Button variant="default" onClick={() => void refetch()} loading={query.isFetching} leftSection={<IconRefresh size={16} />}>{t('serviceStatus.refresh')}</Button>
    </Group>
    <Text c="dimmed" size="sm">{t('serviceStatus.scope')}</Text>
    {unavailable && <Alert color="orange" title={t('serviceStatus.unavailable')} role="status">{t(online ? 'serviceStatus.fetchError' : 'serviceStatus.offline')}</Alert>}
    {query.isPending && online && <Skeleton height={220} radius="lg" />}
    {data && <>
      {snapshotExpired(data, at) && <Alert color="orange" role="status">{t('serviceStatus.expired')}</Alert>}
      <section className={classes.hero} aria-label={t('serviceStatus.overview')}>
        <Text size="xs" tt="uppercase" fw={650} c="dimmed" mb="sm">{t('serviceStatus.overview')}</Text>
        <Title order={2} className={classes.heading}>{t(`serviceStatus.${headlineKey(summary)}`)}</Title>
        {summary.total > 0 && <>
          <div className={classes.summary} aria-live="polite">{statusStates.map(state => <span key={state} className={classes.legend} style={colorStyle(state)}><span className={classes.dot} aria-hidden />{t(`serviceStatus.states.${state}`)} <strong>{summary[state]}</strong></span>)}</div>
          <div className={classes.meter} aria-hidden>{statusStates.filter(state => summary[state] > 0).map(state => <span key={state} style={{ ...colorStyle(state), flex: summary[state] }} />)}</div>
        </>}
        <Text size="xs" c="dimmed" mt="lg">{t('serviceStatus.updated', { time: time(data.generated_at) })} · {t('serviceStatus.autoRefresh')}</Text>
      </section>
      {summary.total === 0 ? <Card><Text c="dimmed">{t('serviceStatus.empty')}</Text></Card> : <section aria-label={t('serviceStatus.nodes')}>
        <div className={classes.categories} role="group" aria-label={t('serviceStatus.categories')}>
          {options.map(option => {
            const members = option.value === 'all' ? entries : entries.filter(entry => String(entry.category?.id ?? 0) === option.value)
            return <button type="button" key={option.value} className={classes.category} aria-pressed={activeCategory === option.value} onClick={() => setCategory(option.value)}>{option.name}<small>{members.filter(entry => entry.state === 'healthy').length}/{members.length}</small></button>
          })}
        </div>
        {description && <Text size="sm" c="dimmed" mb="sm" style={{ overflowWrap: 'anywhere' }}>{description}</Text>}
        <Text size="xs" c="dimmed" mb="xs">{t('serviceStatus.categoryHint')}</Text>
        <div className={classes.grid}>
          {visibleEntries.map(entry => <details key={entry.id} className={classes.entry}>
            <summary>
              <span className={classes.name}>{entry.name}<span className={classes.rate}>×{Number.isFinite(entry.rate) ? entry.rate : '—'}</span></span>
              <span className={classes.state} style={colorStyle(entry.state)}><span className={classes.dot} aria-hidden />{t(`serviceStatus.states.${entry.state}`)}</span>
              <IconChevronDown className={classes.chevron} size={14} aria-hidden />
            </summary>
            <Stack gap="xs" className={classes.detail}>
              <Text size="sm">{t(`serviceStatus.reasons.${entry.reason}`, { defaultValue: t('serviceStatus.reasons.unsupported') })}</Text>
              {entry.maintenance && <>
                {entry.maintenance.note && <Text size="sm" className={classes.note}>{entry.maintenance.note}</Text>}
                <Text size="xs" c="dimmed">{time(entry.maintenance.starts_at)} — {time(entry.maintenance.ends_at)}</Text>
              </>}
              <Text size="xs" c="dimmed">{t('serviceStatus.observed', { time: time(entry.observed_at) })}</Text>
              <Group gap={5}><Text size="xs" c="dimmed">{t('serviceStatus.evidenceLabel')}</Text>{entry.evidence.filter(value => statusEvidence.includes(value as typeof statusEvidence[number])).map(value => <Badge key={value} variant="light" color="gray" size="sm">{t(`serviceStatus.evidence.${value}`)}</Badge>)}</Group>
              <Text size="xs" c="dimmed">{t('serviceStatus.noEndToEnd')}</Text>
            </Stack>
          </details>)}
        </div>
      </section>}
    </>}
    <Text size="xs" c="dimmed">{t('serviceStatus.limitations')}</Text>
  </Stack>
}
