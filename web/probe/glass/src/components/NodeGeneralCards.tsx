import type { CSSProperties, KeyboardEvent, ReactNode, Ref } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Icon } from '@iconify/react'
import { snapshot } from '@shared/captain'
import { useBox } from '@/hooks/useBox'
import { UI_CONFIG } from '@shared/constants/ui'
import type { GeneralMetricCard } from '@shared/views/generalCards'
import { buildGeneralCards } from '@shared/views/generalCards'
import NodeEarthGlobe from '@/components/NodeEarthGlobe'
import Transition from '@/components/Transition'
import { CardX } from '@/components/ui/card-x'
import { DataTooltip } from '@/components/ui/data-tooltip'
import { vclass } from '@/lib/vclass'
import '@/scoped/node-general-cards.css'
import { scope } from '@/scoped/scope'
import { useApp } from '@/stores/app'
import type { NodeData } from '@/stores/nodes'
import { useNodes } from '@/stores/nodes'

const s = scope('node-general-cards')

const cardClass = 'group relative z-10 h-full bg-background/50 border-none hover:bg-background backdrop-blur-sm md:backdrop-blur-none transition-all'
const cardPositionClasses = [
  'col-span-4 row-span-1 col-start-1 row-start-1',
  'col-span-4 row-span-1 col-start-1 row-start-2',
  'col-span-4 row-span-1 col-start-5 row-start-1',
  'col-span-4 row-span-1 col-start-5 row-start-2',
  'col-span-4 row-span-1 col-start-9 row-start-1',
  'col-span-4 row-span-1 col-start-9 row-start-2',
]
const tiledCardPositionClasses = [
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-1 row-start-1',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-4 row-start-1',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-7 row-start-2 sm:row-start-1',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-10 row-start-2 sm:row-start-1',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-1 row-start-3 sm:row-start-2',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-4 row-start-3 sm:row-start-2',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-7 row-start-4 sm:row-start-2',
  'col-span-6 sm:col-span-3 row-span-1 sm:col-start-10 row-start-4 sm:row-start-2',
]
const unitClass = 'text-[11px] md:text-xs font-medium text-muted-foreground truncate'

/** Animated value block; forwards its ref so Transition can drive it. */
function MetricValue({ ref, style, children }: { ref?: Ref<HTMLDivElement>, style: CSSProperties, children: ReactNode }) {
  return <div ref={ref} {...s} className="flex items-baseline gap-1 min-w-0" style={style}>{children}</div>
}

/** useNow({ interval: 1000 }) */
function useNow(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])
  return now
}

export default function NodeGeneralCards({ nodes, globeNodes, transitionKey, ...attrs }: { nodes?: NodeData[], globeNodes?: NodeData[], transitionKey?: string } & Record<string, unknown>) {
  const app = useApp(a => ({
    lang: a.lang,
    privateFeaturesAllowed: a.privateFeaturesAllowed,
    hidePriceWhenLoggedOut: a.hidePriceWhenLoggedOut,
    disablePageAnimation: a.disablePageAnimation,
    byteDecimals: a.byteDecimals,
    homeHighLoadThreshold: a.homeHighLoadThreshold,
    homeExpiringDays: a.homeExpiringDays,
    homeTrafficWarningThreshold: a.homeTrafficWarningThreshold,
    generalCardOrder: a.generalCardOrder,
    hideEarth: a.hideEarth,
    earthRenderer: a.earthRenderer,
  }))
  const snap = useBox(snapshot)
  const visibleNodes = useNodes(n => n.visibleNodes)
  const showPrice = app.privateFeaturesAllowed || !app.hidePriceWhenLoggedOut
  const currentTime = useNow()
  const summaryNodes = nodes ?? visibleNodes
  const summaryTransitionKey = transitionKey ?? visibleNodes.length

  const visibleCards = useMemo(() => buildGeneralCards({
    nodes: summaryNodes,
    snapshot: snap,
    lang: app.lang,
    byteDecimals: app.byteDecimals,
    homeHighLoadThreshold: app.homeHighLoadThreshold,
    homeExpiringDays: app.homeExpiringDays,
    homeTrafficWarningThreshold: app.homeTrafficWarningThreshold,
    showPrice,
    now: currentTime,
  }, app.generalCardOrder), [summaryNodes, snap, app, showPrice, currentTime])

  const showEarth = !app.hideEarth
  const isTiledEarth = showEarth && app.earthRenderer === 'tiled'
  const shouldRenderHeader = showEarth || visibleCards.length > 0
  const hasExtraCards = visibleCards.length > 6
  const wrapperClass = !showEarth
    ? 'p-4 grid grid-cols-1 gap-2 h-auto'
    : isTiledEarth
      ? 'p-3 sm:p-4 grid grid-cols-12 gap-2 sm:gap-3 h-auto min-h-[40rem] sm:min-h-[30rem] md:min-h-[36rem] lg:min-h-[40rem]'
      : hasExtraCards ? 'p-4 grid grid-cols-12 gap-2 h-auto md:min-h-58' : 'p-4 grid grid-cols-12 grid-rows-1 gap-2 h-auto md:h-58'
  const earthClass = isTiledEarth
    ? 'col-span-12 row-start-2 min-h-[18rem] h-[18rem] sm:h-[20rem] md:h-[24rem] lg:h-[28rem]'
    : 'col-span-12 col-start-1 md:col-span-6 md:col-start-7 md:row-start-1'
  const cardGridClass = !showEarth
    ? 'col-span-1 grid grid-cols-3 md:grid-cols-6 gap-2'
    : isTiledEarth
      ? 'col-span-12 row-start-1 z-9 grid grid-cols-12 auto-rows-[4.75rem] sm:auto-rows-[5rem] md:auto-rows-[5.8rem] gap-2 sm:gap-3'
      : hasExtraCards
        ? 'h-auto -mt-42 md:mt-0 col-span-12 row-start-3 z-9 md:h-auto md:col-span-6 md:row-start-1 grid grid-cols-12 auto-rows-[5rem] md:auto-rows-[7rem] gap-2'
        : 'h-42 -mt-42 md:mt-0 col-span-12 row-start-3 z-9 md:h-auto md:col-span-6 md:row-start-1 grid grid-cols-12 grid-rows-2 gap-2'

  function getCardPositionClass(index: number): string {
    if (!showEarth)
      return 'col-span-1 min-h-18 md:min-h-28'
    if (isTiledEarth)
      return tiledCardPositionClasses[index] ?? 'col-span-6 sm:col-span-3 row-span-1'
    return cardPositionClasses[index] ?? 'col-span-4 row-span-1'
  }

  // Finance rows are read-only summaries; the upstream exchange-rate dialog
  // is not used for Captain's original-currency amounts.
  function activateCard(_card: GeneralMetricCard) {}

  function handleCardKeydown(event: KeyboardEvent<HTMLDivElement>, card: GeneralMetricCard) {
    if (!card.action || (event.key !== 'Enter' && event.key !== ' '))
      return
    event.preventDefault()
    activateCard(card)
  }

  return (
    <>
      {shouldRenderHeader && (
        <div {...attrs} {...s} className={vclass(wrapperClass, attrs.className as string | undefined)}>
          {showEarth && <NodeEarthGlobe {...s} nodes={globeNodes} className={earthClass} />}

          {visibleCards.length > 0 && (
            <div {...s} className={cardGridClass}>
              {visibleCards.map((card, index) => (
                <CardX
                  {...s}
                  key={card.key}
                  data-general-card-key={card.key}
                  hoverable
                  className={vclass([cardClass, getCardPositionClass(index), card.action && 'cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'])}
                  contentClass="h-full !p-3"
                  role={card.action ? 'button' : undefined}
                  tabIndex={card.action ? 0 : undefined}
                  aria-label={card.action ? `查看${card.label}明细` : undefined}
                  onClick={() => activateCard(card)}
                  onKeyDown={e => handleCardKeydown(e, card)}
                >
                  <div {...s} className="flex h-full min-h-0 flex-col justify-between gap-1">
                    <div {...s} className="flex items-start justify-between gap-2">
                      <span {...s} className="text-xs font-medium tracking-wider text-muted-foreground truncate">{card.label}</span>
                      <Icon {...s} icon={card.icon} width={20} height={20} className="shrink-0 text-slate-500/20 group-hover:text-slate-500 transition-colors" />
                    </div>
                    <DataTooltip
                      {...s}
                      as="span"
                      placement="top"
                      content={card.tooltip}
                      className={card.moneyRows ? "min-h-0 min-w-0 flex-1" : "min-w-0"}
                      contentClass={card.key === 'remainingValue' ? "whitespace-normal w-64 max-w-[calc(100vw-2rem)] px-2 py-1 left-0 -translate-x-0 leading-normal" : "whitespace-pre px-2 py-1 left-0 -translate-x-0 leading-normal"}
                    >
                      {card.moneyRows ? (
                        <div tabIndex={0} role="group" aria-label={card.label} data-finance-rows className="h-full min-w-0 space-y-0.5 overflow-y-auto outline-offset-1 focus-visible:outline focus-visible:outline-ring">
                          {card.moneyRows.map(money => (
                            <div key={money.currency} data-finance-currency={money.currency} className="flex min-w-0 items-baseline justify-between gap-1 text-xs leading-4 md:text-base md:leading-5">
                              <span className="shrink-0 font-medium text-muted-foreground">{money.currency}</span>
                              <span className="min-w-0 break-all text-right font-semibold tabular-nums">{money.value}</span>
                            </div>
                          ))}
                        </div>
                      ) : <Transition {...(app.disablePageAnimation ? { css: false } : { name: 'metric-switch', mode: 'out-in' as const })}>
                        <MetricValue key={`${card.key}-${summaryTransitionKey}`} style={{ '--metric-switch-delay': `${index * UI_CONFIG.motion.staggerMs}ms` } as CSSProperties}>
                          <span {...s} className="text-md md:text-2xl font-bold leading-none tracking-tight truncate">
                            {card.value}
                          </span>
                          {card.unit && (
                            <span {...s} className={unitClass}>
                              {card.unit}
                            </span>
                          )}
                        </MetricValue>
                      </Transition>}
                    </DataTooltip>
                    {card.note && <span data-finance-coverage className="shrink-0 text-[10px] leading-3 text-muted-foreground md:text-[11px]">{card.note}</span>}
                  </div>
                </CardX>
              ))}
            </div>
          )}
        </div>
      )}

    </>
  )
}
