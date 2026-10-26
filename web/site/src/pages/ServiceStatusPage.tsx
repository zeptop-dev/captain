import { IconArrowUpRight, IconRefresh } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { NodeGlobe } from '../components/Globe'
import { SiteLayout } from '../components/SiteLayout'
import { statusMapLocations } from '../lib/map-locations'
import type { Location, Site } from '../lib/site'
import { StatusRequestError, currentEntries, fetchServiceStatus, headlineKey, snapshotExpired, statusCategories, statusEvidence, statusStates, summarize, type StatusState } from '../lib/service-status'
import classes from './ServiceStatusPage.module.css'

const noLocations: Location[] = []
const colors: Record<StatusState, string> = { healthy: 'var(--status-healthy, #67c7a1)', degraded: 'var(--status-degraded, #fbbf24)', down: 'var(--status-down, #f16f70)', maintenance: 'var(--status-maintenance, #fbbf24)', unknown: 'var(--status-unknown, #858b90)' }

export default function ServiceStatusPage({ site }: { site?: Site }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage || i18n.language || 'en'
  const number = new Intl.NumberFormat(locale, { maximumFractionDigits: 20 })
  const dateTime = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' })
  const [now, setNow] = useState(() => Date.now() / 1000)
  const [online, setOnline] = useState(() => navigator.onLine)
  const [category, setCategory] = useState<string | null>(null)
  const query = useQuery({
    queryKey: ['service-status'], queryFn: ({ signal }) => fetchServiceStatus('/api/site/service-status', signal),
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
  const options = categories.map(value => ({ value: String(value.id), name: value.name, description: value.description }))
  if (entries.some(entry => !entry.category)) options.push({ value: '0', name: t('serviceStatus.uncategorized'), description: '' })
  if (options.length > 1) options.push({ value: 'all', name: t('serviceStatus.all'), description: '' })
  const activeCategory = options.some(option => option.value === category) ? category : options[0]?.value ?? 'all'
  const visibleEntries = activeCategory === 'all' ? entries : entries.filter(entry => String(entry.category?.id ?? 0) === activeCategory)
  const entryMap = site?.status_map_source === 'entries'
  const mapLocations = entryMap ? statusMapLocations(visibleEntries) : site?.status_locations ?? site?.locations ?? noLocations
  const description = options.find(option => option.value === activeCategory)?.description
  const colorStyle = (state: StatusState) => ({ '--status-color': colors[state] } as CSSProperties)
  const time = (timestamp: number | null) => {
    const date = timestamp ? new Date(timestamp * 1000) : null
    return date && Number.isFinite(date.getTime()) ? dateTime.format(date) : t('serviceStatus.never')
  }
  const closed = query.error instanceof StatusRequestError && query.error.status === 404
  const overall: StatusState = !data || !summary.total ? 'unknown' : summary.down === summary.total ? 'down' : summary.down || summary.degraded ? 'degraded' : summary.maintenance ? 'maintenance' : summary.unknown ? 'unknown' : 'healthy'
  const heading = unavailable ? t(closed ? 'serviceStatus.notPublic' : 'serviceStatus.awaiting') : query.isPending ? t('serviceStatus.checking') : t(`serviceStatus.${headlineKey(summary)}`)
  const counts = [
    summary.down + summary.degraded > 0 ? t('serviceStatus.summaryAffected', { value: number.format(summary.down + summary.degraded) }) : '',
    t('serviceStatus.summaryHealthy', { value: number.format(summary.healthy) }),
    summary.maintenance > 0 ? t('serviceStatus.summaryMaintenance', { value: number.format(summary.maintenance) }) : '',
    summary.unknown > 0 ? t('serviceStatus.summaryUnknown', { value: number.format(summary.unknown) }) : '',
  ].filter(Boolean).join(' · ')
  const errorMessage = closed ? t('serviceStatus.closedHint') : t(online ? 'serviceStatus.fetchError' : 'serviceStatus.offline')
  return <SiteLayout site={site}>
    <section className={classes.hero} aria-label={t('serviceStatus.overview')}>
      <div className={`site-container ${classes.heroInner}`}>
        <div className={classes.heroCopy}>
          <p className={classes.eyebrow}>{site?.name || 'Captain'} · {t('serviceStatus.title')}</p>
          <h1 className={classes.heading} style={colorStyle(overall)}>{heading}</h1>
          <p className={classes.summary} aria-live="polite">{unavailable ? t(closed ? 'serviceStatus.closed' : 'serviceStatus.unavailable') : query.isPending ? t('serviceStatus.loading') : summary.total ? counts : t('serviceStatus.empty')}</p>
          <p className={classes.scope}>{t('serviceStatus.limitations')}</p>
          <p className={classes.updated}>{data ? <>{t('serviceStatus.updated', { time: time(data.generated_at) })}<span className={classes.separator}>·</span>{t('serviceStatus.autoRefresh', { seconds: number.format(60) })}</> : t(unavailable ? 'serviceStatus.waitingRefresh' : 'serviceStatus.connecting')}</p>
        </div>
        <div className={classes.globe}>
          <NodeGlobe locations={mapLocations} hub={entryMap ? null : site?.status_hub !== undefined ? site.status_hub : site?.hub ?? null} variant="status" />
        </div>
      </div>
    </section>
    <section className={classes.nodes} aria-label={t('serviceStatus.nodes')}>
      <div className="site-container">
        <div className={classes.sectionHeading}>
          <h2>{t('serviceStatus.nodes')}</h2>
          <div className={classes.controls}>
            <div className={classes.legend} aria-label={t('serviceStatus.legend')}>{statusStates.map(state => <span key={state} style={colorStyle(state)}><span className={classes.dot} aria-hidden="true" />{t(`serviceStatus.states.${state}`)}</span>)}</div>
            <button className={classes.refresh} type="button" onClick={() => void refetch()} disabled={query.isFetching} aria-label={t('serviceStatus.refresh')} title={t('serviceStatus.refresh')}><IconRefresh size={15} className={query.isFetching ? classes.spinning : undefined} aria-hidden="true" /></button>
          </div>
        </div>
        {unavailable && <div className={classes.notice} role="status">{errorMessage}</div>}
        {data && snapshotExpired(data, at) && <div className={classes.notice} role="status">{t('serviceStatus.expired')}</div>}
        {query.isPending && online && <div className={classes.panel} aria-label={t('serviceStatus.loading')} aria-busy="true"><div className={classes.loadingTabs} /><div className={classes.loadingRows} aria-hidden="true">{Array.from({ length: 8 }, (_, index) => <div key={index}><span /><i /></div>)}</div></div>}
        {data && summary.total > 0 && <div className={classes.panel}>
          <div className={classes.categories} role="group" aria-label={t('serviceStatus.categories')}>
            {options.map(option => {
              const members = option.value === 'all' ? entries : entries.filter(entry => String(entry.category?.id ?? 0) === option.value)
              const healthy = members.filter(entry => entry.state === 'healthy').length
              return <button type="button" key={option.value} className={classes.category} aria-label={`${option.name} — ${t('serviceStatus.categoryCount', { healthy: number.format(healthy), total: number.format(members.length) })}`} aria-pressed={activeCategory === option.value} onClick={() => setCategory(option.value)}>{option.name}<small data-affected={healthy < members.length || undefined}>{number.format(healthy)}/{number.format(members.length)}</small></button>
            })}
          </div>
          {description && <p className={classes.description}>{description}</p>}
          <div className={classes.grid}>
            {visibleEntries.map(entry => <details key={entry.id} className={classes.entry}>
              <summary title={t('serviceStatus.showEvidence')}>
                <span className={classes.name}>{entry.name}<span className={classes.rate}>{Number.isFinite(entry.rate) ? number.format(entry.rate) : '—'}×</span></span>
                <span className={classes.state} style={colorStyle(entry.state)}><span className={classes.dot} aria-hidden="true" />{t(`serviceStatus.states.${entry.state}`)}</span>
              </summary>
              <div className={classes.detail}>
                <p className={classes.reason}>{t(`serviceStatus.reasons.${entry.reason}`, { defaultValue: t('serviceStatus.reasons.unsupported') })}</p>
                {entry.maintenance && <>
                  {entry.maintenance.note && <p className={classes.note}>{entry.maintenance.note}</p>}
                  <p>{time(entry.maintenance.starts_at)} — {time(entry.maintenance.ends_at)}</p>
                </>}
                <p>{t('serviceStatus.observed', { time: time(entry.observed_at) })}</p>
                <div className={classes.evidence}><span>{t('serviceStatus.evidenceLabel')}</span>{entry.evidence.filter(value => statusEvidence.includes(value as typeof statusEvidence[number])).map(value => <span key={value}>{t(`serviceStatus.evidence.${value}`)}</span>)}</div>
                <p>{t('serviceStatus.noEndToEnd')}</p>
              </div>
            </details>)}
          </div>
        </div>}
        {(unavailable || data && summary.total === 0) && <div className={`${classes.panel} ${classes.empty}`}>
          <span className={classes.emptyMark} aria-hidden="true"><span /><span /><span /></span>
          <h3>{unavailable ? t(closed ? 'serviceStatus.closed' : 'serviceStatus.unavailable') : t('serviceStatus.emptyTitle')}</h3>
          <p>{unavailable ? errorMessage : t('serviceStatus.empty')}</p>
          {closed ? <a href="/portal/status">{t('serviceStatus.myNodes')}<IconArrowUpRight size={14} aria-hidden="true" /></a> : unavailable && <button type="button" onClick={() => void refetch()} disabled={query.isFetching}>{t(query.isFetching ? 'serviceStatus.refreshing' : 'serviceStatus.retry')}</button>}
        </div>}
        {data && summary.total > 0 && <p className={classes.hint}>{t('serviceStatus.categoryHint')}</p>}
      </div>
    </section>
  </SiteLayout>
}
