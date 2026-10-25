import { useQuery } from '@tanstack/react-query'
import { IconArrowDown, IconArrowRight, IconArrowUpRight, IconBolt, IconBrandAndroid, IconBrandApple, IconBrandWindows, IconCheck, IconDevices, IconHeadset, IconRefresh, IconRouter, IconShieldLock, IconWorld } from '@tabler/icons-react'
import { NetworkMap } from '../components/NetworkMap'
import { bytes, money } from '../lib/format'
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
  return <div className={styles.deviceFigure} aria-hidden="true">
    <div className={styles.figureMeta}><span>DEVICE MATRIX</span><span>MULTI-PLATFORM</span></div>
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
    <div className={styles.figureFooter}><span>DESKTOP</span><span>MOBILE</span><span>ROUTER</span></div>
  </div>
}

function ConnectionFigure() {
  return <div className={styles.connectionFigure}>
    <svg viewBox="0 0 530 280" fill="none" role="img" aria-label="客户端通过订阅中的节点连接目标服务的流程示意">
      <path d="M98 140h90m134 0h108M265 52v35m0 106v35" stroke="currentColor" strokeOpacity=".2" strokeDasharray="3 6" />
      <circle cx="265" cy="140" r="82" stroke="currentColor" strokeOpacity=".12" />
      <circle cx="265" cy="140" r="66" stroke="currentColor" strokeOpacity=".32" />
      <circle cx="265" cy="140" r="49" fill="#e0e6d5" stroke="#a5b18f" />
      <path d="m265 116 21 12v24l-21 12-21-12v-24l21-12Zm0 0v48m-21-36 21 12 21-12" stroke="#6f8056" strokeWidth="1.2" />
      <rect x="25" y="114" width="73" height="48" rx="4" stroke="currentColor" strokeOpacity=".5" /><path d="M39 172h45m-22-10v10" stroke="currentColor" strokeOpacity=".5" />
      <rect x="430" y="109" width="74" height="59" rx="5" stroke="currentColor" strokeOpacity=".5" /><path d="M430 129h74m-74 20h74" stroke="currentColor" strokeOpacity=".3" />
      <circle cx="442" cy="119" r="2" fill="#6f8056" /><circle cx="442" cy="139" r="2" fill="#6f8056" /><circle cx="442" cy="159" r="2" fill="#6f8056" />
      <path d="m180 136 6 4-6 4m242-8 6 4-6 4" stroke="#6f8056" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="265" cy="45" r="5" fill="#6f8056" /><circle cx="265" cy="235" r="5" stroke="#6f8056" />
      <text x="61" y="199" textAnchor="middle">你的客户端</text><text x="265" y="264" textAnchor="middle">订阅中的节点</text><text x="467" y="199" textAnchor="middle">目标服务</text>
    </svg>
    <p>连接流程示意，不代表具体节点的实际线路拓扑。</p>
  </div>
}

export default function HomePage({ site: s }: { site: Site }) {
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => getJSON<Plan[]>('/api/portal/plans'), enabled: s.show_plans })
  const featured = s.features[0]
  const FeaturedIcon = featureIcons[(featured?.icon as keyof typeof featureIcons) ?? 'world'] ?? IconWorld
  const startURL = s.registration ? '/portal/register' : '/portal/login'
  const startText = s.registration ? '立即开始' : '登录用户中心'
  const headingLines = heroLines(s.tagline.trim() || s.name)
  const shortcuts = [
    { href: '#platform', title: '多端接入', detail: '在你的设备上开始使用' },
    s.features.length ? { href: '#features', title: '服务特性', detail: '了解我们的服务' } : { href: '#coverage', title: '网络覆盖', detail: '查看配置的覆盖地区' },
    s.show_plans ? { href: '#plans', title: '选择套餐', detail: '找到适合你的方案' } : s.faq.length ? { href: '#faq', title: '常见问题', detail: '连接前，先了解更多' } : { href: '#start', title: '开始连接', detail: '前往你的用户中心' },
  ]
  return <div className={styles.page}>
    <section className={`${styles.hero} site-container`} aria-labelledby="home-heading">
      <div className={styles.eyebrow}><span />{s.name} / 订阅与连接</div>
      <div className={styles.heroCopy}>
        <h1 id="home-heading">{headingLines.map((line, index) => <span key={index}>{line}</span>)}</h1>
        <div className={styles.heroAside}>
          {s.description && <p>{s.description}</p>}
          <div className={styles.actions}>
            <a className={styles.primaryAction} href={startURL}>{startText}<IconArrowUpRight size={18} aria-hidden /></a>
            <a className={styles.secondaryAction} href={s.links.download || (s.show_plans ? '#plans' : '/portal/connect')}>{s.links.download ? '客户端下载' : s.show_plans ? '查看套餐' : '连接指南'}<IconArrowRight size={16} aria-hidden /></a>
          </div>
        </div>
      </div>
      <div id="coverage" className={styles.map}><NetworkMap locations={s.locations} hub={s.hub} /></div>
      <div className={styles.coverageNote}><span>NETWORK COVERAGE</span><p>网络覆盖示意，非实时监控；可用节点与访问权限以用户中心为准。</p></div>
      <div className={styles.shortcuts}>{shortcuts.map((item, index) => <a href={item.href} key={item.href}>
        <span className={styles.shortcutNumber}>0{index + 1}</span><div><h2>{item.title}</h2><p>{item.detail}</p></div><IconArrowDown size={20} stroke={1.3} aria-hidden />
      </a>)}</div>
    </section>

    <section id="platform" className={styles.section} aria-labelledby="platform-heading">
      <div className="site-container">
        <SectionLabel number="01" label="PLATFORM / 多端接入" />
        <h2 id="platform-heading">在熟悉的设备上，开始连接</h2>
        <div className={styles.platformGrid}>
          <div className={styles.platformCopy}>
            <h3>一份订阅，多种客户端选择。</h3>
            <p>从桌面到移动设备，在用户中心获取适合客户端的订阅格式，再按连接指南完成设置。</p>
            <div className={styles.clientsLabel}><span>SUPPORTED / CLIENTS</span><span>↗</span></div>
            <div className={styles.platformIcons} aria-label="桌面、移动设备及路由器"><IconBrandApple /><IconBrandWindows /><IconBrandAndroid /><IconDevices /><IconRouter /></div>
            <p className={styles.clientNames}>Mihomo · Stash · sing-box · Surge<br />Loon · Quantumult X · Egern</p>
            <p className={styles.finePrint}>订阅格式与可用协议以用户中心提供的选项及客户端支持为准。</p>
            <a className={styles.textLink} href={s.links.download || '/portal/connect'}>{s.links.download ? '查看客户端' : '查看连接指南'}<IconArrowUpRight size={15} aria-hidden /></a>
          </div>
          <DeviceFigure />
        </div>
      </div>
    </section>

    {featured && <section id="features" className={`${styles.section} ${styles.highlight}`} aria-labelledby="feature-heading">
      <div className={`site-container ${styles.highlightGrid}`}>
        <div><SectionLabel number="02" label="FEATURE / 服务特性" /><h2 id="feature-heading">{featured.title}</h2><p>{featured.text}</p><a className={styles.textLink} href={s.features.length > 1 ? '#service-details' : startURL}>{s.features.length > 1 ? '了解更多服务特性' : '前往用户中心'}<IconArrowUpRight size={15} aria-hidden /></a></div>
        <div className={styles.featureFigure} aria-hidden="true"><div className={styles.orbitOuter} /><div className={styles.orbitInner} /><div className={styles.featureSymbol}><FeaturedIcon size={60} stroke={1} /></div><span className={styles.featureCaption}>{s.name} / SERVICE</span></div>
      </div>
    </section>}

    <section className={`${styles.section} ${styles.backbone}`} aria-labelledby="connection-heading">
      <div className="site-container">
        <SectionLabel number="03" label="CONNECTION / 连接流程" />
        <h2 id="connection-heading">从订阅，到每一次连接</h2>
        <div className={styles.connectionGrid}>
          <div className={styles.steps}>
            <div><span>01</span><div><h3>选择服务</h3><p>在用户中心查看套餐的流量、周期和设备限制。</p></div></div>
            <div><span>02</span><div><h3>获取订阅</h3><p>按客户端选择订阅格式，导入后查看可用节点。</p></div></div>
            <div><span>03</span><div><h3>开始连接</h3><p>从当前套餐授权的节点中，选择适合你的连接。</p></div></div>
          </div>
          <ConnectionFigure />
        </div>
      </div>
    </section>

    <section className={`${styles.section} ${styles.signal}`} aria-labelledby="signal-heading">
      <div className={`site-container ${styles.signalGrid}`}>
        <div><SectionLabel number="04" label="YOUR CONNECTION / 用户中心" /><h2 id="signal-heading">连接与使用，<br />都清晰一点</h2><p>订阅、套餐与节点信息，<br />在用户中心集中管理。</p></div>
        <div className={styles.signalLinks}>
          <a href="/portal/connect"><span>01</span><div><h3>订阅连接</h3><p>获取订阅，查看客户端连接指南。</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
          <a href="/portal/"><span>02</span><div><h3>套餐与用量</h3><p>查看当前套餐、有效期与剩余流量。</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
          <a href={s.service_status_url || '/portal/servers'}><span>03</span><div><h3>{s.service_status_url ? '服务状态' : '节点信息'}</h3><p>{s.service_status_url ? '查看公开节点的服务状态与维护信息。' : '查看当前账户可用的订阅节点。'}</p></div><IconArrowUpRight size={21} stroke={1.4} aria-hidden /></a>
        </div>
      </div>
    </section>

    {s.features.length > 1 && <section id="service-details" className={styles.section} aria-labelledby="details-heading">
      <div className="site-container"><SectionLabel number="05" label="DETAILS / 服务信息" /><h2 id="details-heading">了解你的服务选择</h2>
        <div className={styles.featureRows}>{s.features.slice(1).map((feature, index) => { const Icon = featureIcons[(feature.icon as keyof typeof featureIcons) ?? 'bolt'] ?? IconBolt; return <div key={index}><h3><Icon size={20} stroke={1.4} aria-hidden />{feature.title}</h3><p>{feature.text}</p><IconCheck className={styles.rowMark} size={18} stroke={1.4} aria-hidden /></div> })}</div>
      </div>
    </section>}

    {s.show_plans && <section id="plans" className={styles.section} aria-labelledby="plans-heading">
      <div className="site-container"><SectionLabel number="06" label="PLANS / 套餐" /><div className={styles.sectionHeading}><h2 id="plans-heading">选择适合你的方案</h2><a className={styles.textLink} href="/portal/plans">前往用户中心<IconArrowUpRight size={15} aria-hidden /></a></div>
        {plans.isPending ? <p role="status">正在加载套餐…</p> : plans.isError ? <div role="status"><p>暂时无法加载套餐，请稍后重试。</p><button className={styles.secondaryAction} type="button" onClick={() => void plans.refetch()}>重新加载</button></div> : !plans.data?.length ? <p>暂无公开套餐，请在用户中心查看服务信息。</p> : <div className={styles.plans}>{plans.data.map(plan => <article key={plan.ID} className={styles.plan}>
          <h3>{plan.Name}</h3><div className={styles.price}>{money(plan.PriceCents)}<span> / {plan.PeriodDays ? `${plan.PeriodDays} 天` : '永久'}</span></div>
          <ul><li><IconCheck size={15} aria-hidden />{plan.QuotaBytes ? `${bytes(plan.QuotaBytes)} 流量` : '不限流量'}</li><li><IconCheck size={15} aria-hidden />{plan.DeviceLimit ? `${plan.DeviceLimit} 台设备` : '设备数不限'}</li>{plan.SpeedLimitMbps > 0 && <li><IconCheck size={15} aria-hidden />{plan.SpeedLimitMbps} Mbps</li>}</ul>
          <a className={styles.secondaryAction} href="/portal/plans">选择套餐<IconArrowUpRight size={16} aria-hidden /></a>
        </article>)}</div>}
      </div>
    </section>}

    {s.faq.length > 0 && <section id="faq" className={styles.section} aria-labelledby="faq-heading"><div className={`site-container ${styles.faqGrid}`}>
      <div><SectionLabel number="07" label="FAQ / 支持" /><h2 id="faq-heading">还有疑问？</h2><p>关于服务与连接，<br />你可能想了解这些。</p></div>
      <div className={styles.faq}>{s.faq.map((faq, index) => <details key={index}><summary>{faq.q}<span aria-hidden="true">+</span></summary><p>{faq.a}</p></details>)}</div>
    </div></section>}

    <section id="start" className={`${styles.closing} site-container`} aria-labelledby="start-heading"><div className={styles.eyebrow}><span />{s.name}</div><h2 id="start-heading">下一次连接，从这里开始</h2><a className={styles.primaryAction} href={startURL}>{startText}<IconArrowUpRight size={18} aria-hidden /></a></section>
  </div>
}
