import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { IconArrowDown, IconArrowRight, IconArrowUpRight, IconBolt, IconBrandAndroid, IconBrandApple, IconBrandWindows, IconCheck, IconDevices, IconHeadset, IconRefresh, IconRouter, IconShieldLock, IconWorld } from '@tabler/icons-react'
import { ClientCatalog } from '../components/ClientCatalog'
import type { PublishedClient } from '../../../shared/client-downloads'
import { NetworkMap } from '../components/NetworkMap'
import { bytes, money } from '../lib/format'
import { localizeSiteDefaults } from '../lib/site-defaults'
import { getJSON, type Plan, type Site } from '../lib/site'
import styles from './HomePage.module.css'

const featureIcons = { bolt: IconBolt, shield: IconShieldLock, world: IconWorld, devices: IconDevices, refresh: IconRefresh, headset: IconHeadset }

function heroLines(text: string): string[] {
  const value = text.trim()
  const newline = value.indexOf('\n')
  if (newline >= 0) return [value.slice(0, newline).trim(), value.slice(newline + 1).trim()].filter(Boolean)
  const boundaries = [...value.matchAll(/[，、,]/gu)].map(match => match.index + 1).filter(index => index < value.length)
  if (!boundaries.length) return [value]
  const split = boundaries.reduce((best, index) => Math.abs(index - value.length / 2) < Math.abs(best - value.length / 2) ? index : best)
  return [value.slice(0, split).trim(), value.slice(split).trim()]
}

function SectionLabel({ number, label }: { number: string; label: string }) {
  return <div className={styles.sectionLabel}><span>{number}</span><span>{label}</span></div>
}

function DeviceFigure() {
  const { t } = useTranslation()
  return <div className={styles.deviceFigure} aria-hidden="true">
    <div className={styles.figureMeta}><span>{t('home.figure.deviceMatrix')}</span><span>{t('home.figure.multiPlatform')}</span></div>
    <svg viewBox="0 0 600 300" fill="none">
      <ellipse cx="300" cy="175" rx="244" ry="75" className={styles.svgSubtle} strokeDasharray="3 7" />
      <path d="M77 230h447M300 42v210" className={styles.svgSubtle} strokeDasharray="3 7" />
      <rect x="135" y="65" width="295" height="180" rx="8" className={styles.svgLine} />
      <rect x="148" y="78" width="269" height="153" rx="3" className={styles.svgSubtle} />
      <path d="M135 245h295l30 20H105l30-20Z" className={styles.svgLine} />
      <path d="M248 245h70l7 9h-84l7-9Z" className={styles.svgSubtle} />
      <rect x="206" y="113" width="152" height="86" rx="5" className={styles.svgSubtle} />
      <path d="M206 132h152" className={styles.svgSubtle} />
      <circle cx="217" cy="123" r="2" className={styles.svgFill} /><circle cx="225" cy="123" r="2" className={styles.svgFill} /><circle cx="233" cy="123" r="2" className={styles.svgFill} />
      <circle cx="282" cy="165" r="17" className={styles.svgAccent} />
      <path d="m274 165 5 5 11-11" className={styles.svgAccent} strokeWidth="2" />
      <rect x="401" y="132" width="70" height="134" rx="10" className={styles.svgDevice} />
      <path d="M421 141h30M425 256h21" className={styles.svgLine} strokeLinecap="round" />
      <circle cx="436" cy="193" r="15" className={styles.svgAccent} /><path d="m429 193 4 4 10-10" className={styles.svgAccent} strokeWidth="1.7" />
      <path d="M91 204v-40m-1 40h52m-9 0v-40" className={styles.svgLine} />
      <rect x="77" y="204" width="78" height="30" rx="5" className={styles.svgDevice} />
      <circle cx="91" cy="219" r="2" className={styles.svgAccentFill} /><circle cx="100" cy="219" r="2" className={styles.svgAccentFill} /><path d="M122 219h20" className={styles.svgSubtle} />
      <path d="M478 92c8 0 16 4 20 10m-25 0c5 0 10 3 13 7m-19 0 7 7" className={styles.svgAccent} strokeLinecap="round" />
    </svg>
    <div className={styles.figureFooter}><span>{t('home.figure.desktop')}</span><span>{t('home.figure.mobile')}</span><span>{t('home.figure.router')}</span></div>
  </div>
}

function ConnectionFigure() {
  const { t } = useTranslation()
  return <div className={styles.connectionFigure}>
    <svg viewBox="0 0 530 280" fill="none" role="img" aria-label={t('home.connection.aria')}>
      <path d="M98 140h90m134 0h108M265 52v35m0 106v35" stroke="currentColor" strokeOpacity=".2" strokeDasharray="3 6" />
      <circle cx="265" cy="140" r="82" stroke="currentColor" strokeOpacity=".12" />
      <circle cx="265" cy="140" r="66" stroke="currentColor" strokeOpacity=".32" />
      <circle cx="265" cy="140" r="49" className={styles.connectionCenter} />
      <path d="m265 116 21 12v24l-21 12-21-12v-24l21-12Zm0 0v48m-21-36 21 12 21-12" className={styles.svgAccent} strokeWidth="1.2" />
      <rect x="25" y="114" width="73" height="48" rx="4" stroke="currentColor" strokeOpacity=".5" /><path d="M39 172h45m-22-10v10" stroke="currentColor" strokeOpacity=".5" />
      <rect x="430" y="109" width="74" height="59" rx="5" stroke="currentColor" strokeOpacity=".5" /><path d="M430 129h74m-74 20h74" stroke="currentColor" strokeOpacity=".3" />
      <circle cx="442" cy="119" r="2" className={styles.svgAccentFill} /><circle cx="442" cy="139" r="2" className={styles.svgAccentFill} /><circle cx="442" cy="159" r="2" className={styles.svgAccentFill} />
      <path d="m180 136 6 4-6 4m242-8 6 4-6 4" className={styles.svgAccent} strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="265" cy="45" r="5" className={styles.svgAccentFill} /><circle cx="265" cy="235" r="5" className={styles.svgAccent} />
      <text x="61" y="199" textAnchor="middle">{t('home.connection.client')}</text><text x="265" y="264" textAnchor="middle">{t('home.connection.nodes')}</text><text x="467" y="199" textAnchor="middle">{t('home.connection.target')}</text>
    </svg>
    <p>{t('home.connection.note')}</p>
  </div>
}

export default function HomePage({ site }: { site: Site }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage || i18n.language || 'en'
  const s = localizeSiteDefaults(site, t)
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => getJSON<Plan[]>('/api/portal/plans'), enabled: s.show_plans })
  const catalog = useQuery({ queryKey: ['client-catalog', 'site'], queryFn: () => getJSON<PublishedClient[]>('/api/site/clients') })
  const hasClients = !!catalog.data?.length
  const downloadsURL = s.links.download || (hasClients ? '/downloads' : '')
  const featured = s.features[0]
  const FeaturedIcon = featureIcons[(featured?.icon as keyof typeof featureIcons) ?? 'world'] ?? IconWorld
  const startURL = s.registration ? '/portal/register' : '/portal/login'
  const startText = s.registration ? t('home.actions.start') : t('home.actions.login')
  const headingLines = heroLines(s.tagline.trim() || s.name)
  const shortcuts = [
    { href: '#platform', title: t('home.shortcuts.platform'), detail: t('home.shortcuts.platformDescription') },
    s.features.length ? { href: '#features', title: t('home.shortcuts.features'), detail: t('home.shortcuts.featuresDescription') } : { href: '#coverage', title: t('home.shortcuts.coverage'), detail: t('home.shortcuts.coverageDescription') },
    s.show_plans ? { href: '#plans', title: t('home.plans.choose'), detail: t('home.shortcuts.plansDescription') } : s.faq.length ? { href: '#faq', title: t('home.shortcuts.faq'), detail: t('home.shortcuts.faqDescription') } : { href: '#start', title: t('home.connection.connect'), detail: t('home.actions.portal') },
  ]
  return <div className={styles.page}>
    <section className={`${styles.hero} site-container`} aria-labelledby="home-heading">
      <div className={styles.eyebrow}><span />{s.name} / {t('home.eyebrow')}</div>
      <div className={styles.heroCopy}>
        <h1 id="home-heading">{headingLines.map((line, index) => <span key={index}>{line}</span>)}</h1>
        <div className={styles.heroAside}>
          {s.description && <p>{s.description}</p>}
          <div className={styles.actions}>
            <a className={styles.primaryAction} href={startURL}>{startText}<IconArrowUpRight size={18} aria-hidden /></a>
            <a className={styles.secondaryAction} href={downloadsURL || (s.show_plans ? '#plans' : '/portal/connect')}>{downloadsURL ? t('home.actions.download') : s.show_plans ? t('home.actions.plans') : t('home.actions.guide')}<IconArrowRight size={16} aria-hidden /></a>
          </div>
        </div>
      </div>
      <div id="coverage" className={styles.map}><NetworkMap locations={s.locations} hub={s.hub} /></div>
      <div className={styles.coverageNote}><span>{t('home.coverage.label')}</span><p>{t('home.coverage.note')}</p></div>
      <div className={styles.shortcuts}>{shortcuts.map((item, index) => <a href={item.href} key={item.href}>
        <span className={styles.shortcutNumber}>0{index + 1}</span><div><h2>{item.title}</h2><p>{item.detail}</p></div><IconArrowDown size={20} stroke={1.3} aria-hidden />
      </a>)}</div>
    </section>

    <section id="platform" className={styles.section} aria-labelledby="platform-heading">
      <div className="site-container">
        <SectionLabel number="01" label={t('home.sections.platform')} />
        <h2 id="platform-heading">{t('home.platform.heading')}</h2>
        <div className={styles.platformGrid}>
          <div className={styles.platformCopy}>
            <h3>{t('home.platform.title')}</h3>
            <p>{t('home.platform.description')}</p>
            <div className={styles.clientsLabel}><span>{t('home.platform.supported')}</span><span>↗</span></div>
            <div className={styles.platformIcons} aria-label={t('home.platform.devices')}><IconBrandApple /><IconBrandWindows /><IconBrandAndroid /><IconDevices /><IconRouter /></div>
            <p className={styles.clientNames}>Mihomo · Stash · sing-box · Surge<br />Loon · Quantumult X · Egern</p>
            <p className={styles.finePrint}>{t('home.platform.note')}</p>
            <a className={styles.textLink} href={downloadsURL || '/portal/connect'}>{downloadsURL ? t('home.actions.clients') : t('home.actions.guide')}<IconArrowUpRight size={15} aria-hidden /></a>
          </div>
          <DeviceFigure />
        </div>
        {hasClients && <div id="downloads" style={{ marginTop: 40 }}><ClientCatalog clients={catalog.data!} /></div>}
      </div>
    </section>

    {featured && <section id="features" className={`${styles.section} ${styles.highlight}`} aria-labelledby="feature-heading">
      <div className={`site-container ${styles.highlightGrid}`}>
        <div><SectionLabel number="02" label={t('home.sections.feature')} /><h2 id="feature-heading">{featured.title}</h2><p>{featured.text}</p><a className={styles.textLink} href={s.features.length > 1 ? '#service-details' : startURL}>{s.features.length > 1 ? t('home.actions.moreFeatures') : t('home.actions.portal')}<IconArrowUpRight size={15} aria-hidden /></a></div>
        <div className={styles.featureFigure} aria-hidden="true"><div className={styles.orbitOuter} /><div className={styles.orbitInner} /><div className={styles.featureSymbol}><FeaturedIcon size={60} stroke={1} /></div><span className={styles.featureCaption}>{s.name} / {t('home.figure.service')}</span></div>
      </div>
    </section>}

    <section className={styles.section} aria-labelledby="connection-heading">
      <div className="site-container">
        <SectionLabel number="03" label={t('home.sections.connection')} />
        <h2 id="connection-heading">{t('home.connection.heading')}</h2>
        <div className={styles.connectionGrid}>
          <div className={styles.steps}>
            <div><span>01</span><div><h3>{t('home.connection.choose')}</h3><p>{t('home.connection.chooseDescription')}</p></div></div>
            <div><span>02</span><div><h3>{t('home.connection.subscribe')}</h3><p>{t('home.connection.subscribeDescription')}</p></div></div>
            <div><span>03</span><div><h3>{t('home.connection.connect')}</h3><p>{t('home.connection.connectDescription')}</p></div></div>
          </div>
          <ConnectionFigure />
        </div>
      </div>
    </section>

    <section className={`${styles.section} ${styles.signal}`} aria-labelledby="signal-heading">
      <div className={`site-container ${styles.signalGrid}`}>
        <div><SectionLabel number="04" label={t('home.sections.signal')} /><h2 id="signal-heading">{t('home.signal.heading')}</h2><p>{t('home.signal.description')}</p></div>
        <div className={styles.signalLinks}>
          <a href="/portal/connect"><span>01</span><div><h3>{t('home.signal.subscription')}</h3><p>{t('home.signal.subscriptionDescription')}</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
          <a href="/portal/"><span>02</span><div><h3>{t('home.signal.usage')}</h3><p>{t('home.signal.usageDescription')}</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
          <a href={s.service_status_url || '/portal/servers'}><span>03</span><div><h3>{s.service_status_url ? t('home.signal.status') : t('home.signal.nodes')}</h3><p>{s.service_status_url ? t('home.signal.statusDescription') : t('home.signal.nodesDescription')}</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
        </div>
      </div>
    </section>

    {s.features.length > 1 && <section id="service-details" className={styles.section} aria-labelledby="details-heading">
      <div className="site-container"><SectionLabel number="05" label={t('home.sections.details')} /><h2 id="details-heading">{t('home.details.heading')}</h2>
        <div className={styles.featureRows}>{s.features.slice(1).map((feature, index) => { const Icon = featureIcons[(feature.icon as keyof typeof featureIcons) ?? 'bolt'] ?? IconBolt; return <div key={index}><h3><Icon size={20} stroke={1.4} aria-hidden />{feature.title}</h3><p>{feature.text}</p><IconCheck className={styles.rowMark} size={18} stroke={1.4} aria-hidden /></div> })}</div>
      </div>
    </section>}

    {s.show_plans && <section id="plans" className={styles.section} aria-labelledby="plans-heading">
      <div className="site-container"><SectionLabel number="06" label={t('home.sections.plans')} /><div className={styles.sectionHeading}><h2 id="plans-heading">{t('home.plans.heading')}</h2><a className={styles.textLink} href="/portal/plans">{t('home.actions.portal')}<IconArrowUpRight size={15} aria-hidden /></a></div>
        {plans.isPending ? <p role="status">{t('home.plans.loading')}</p> : plans.isError ? <div role="status"><p>{t('home.plans.error')}</p><button className={styles.secondaryAction} type="button" onClick={() => void plans.refetch()}>{t('home.plans.retry')}</button></div> : !plans.data?.length ? <p>{t('home.plans.empty')}</p> : <div className={styles.plans}>{plans.data.map(plan => <article key={plan.ID} className={styles.plan}>
          <h3>{plan.Name}</h3><div className={styles.price}>{money(plan.PriceCents, '¥', locale)}<span> / {plan.PeriodDays ? t('home.plans.days', { count: plan.PeriodDays }) : t('home.plans.permanent')}</span></div>
          <ul><li><IconCheck size={15} aria-hidden />{plan.QuotaBytes ? t('home.plans.traffic', { amount: bytes(plan.QuotaBytes, locale) }) : t('home.plans.unlimitedTraffic')}</li><li><IconCheck size={15} aria-hidden />{plan.DeviceLimit ? t('home.plans.devices', { count: plan.DeviceLimit }) : t('home.plans.unlimitedDevices')}</li>{plan.SpeedLimitMbps > 0 && <li><IconCheck size={15} aria-hidden />{t('home.plans.speed', { speed: plan.SpeedLimitMbps.toLocaleString(locale) })}</li>}</ul>
          <a className={styles.secondaryAction} href="/portal/plans">{t('home.plans.choose')}<IconArrowUpRight size={16} aria-hidden /></a>
        </article>)}</div>}
      </div>
    </section>}

    {s.faq.length > 0 && <section id="faq" className={styles.section} aria-labelledby="faq-heading"><div className={`site-container ${styles.faqGrid}`}>
      <div><SectionLabel number="07" label={t('home.sections.faq')} /><h2 id="faq-heading">{t('home.faq.heading')}</h2><p>{t('home.faq.description')}</p></div>
      <div className={styles.faq}>{s.faq.map((faq, index) => <details key={index}><summary>{faq.q}<span aria-hidden="true">+</span></summary><p>{faq.a}</p></details>)}</div>
    </div></section>}

    <section id="start" className={`${styles.closing} site-container`} aria-labelledby="start-heading"><div className={styles.eyebrow}><span />{s.name}</div><h2 id="start-heading">{t('home.closing.heading')}</h2><a className={styles.primaryAction} href={startURL}>{startText}<IconArrowUpRight size={18} aria-hidden /></a></section>
  </div>
}
