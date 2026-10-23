import type { HTMLAttributes, KeyboardEvent } from 'react'
import { Icon } from '@iconify/react'
import { language, t } from '@shared/i18n'
import { shows, snapshot, captainPrice, captainBandwidth, captainCPUSpec } from '@shared/captain'
import { selectPings } from '@shared/ping-selection'
import { formatBytesPerSecondWithConfig, formatBytesWithConfig, formatDateTime, formatNumber, getStatus, getUptimeDays } from '@shared/utils/helper'
import { getMemoryPercentage, getTrafficUsed, getTrafficUsedPercentage, hasTrafficLimit } from '@shared/utils/nodeMetricsHelper'
import { getOSImage, getOSName } from '@shared/utils/osImageHelper'
import { getRegionCode, getRegionDisplayName } from '@shared/utils/regionHelper'
import { getDaysUntilExpired, getExpireStatus, parseTags } from '@shared/utils/tagHelper'
import StorageSummary from '@/components/StorageSummary'
import ThreeNetworkPing from '@/components/ThreeNetworkPing'
import { Badge } from '@/components/ui/badge'
import { CardX } from '@/components/ui/card-x'
import { DataTooltip } from '@/components/ui/data-tooltip'
import { ProgressThin } from '@/components/ui/progress-thin'
import { useBox } from '@/hooks/useBox'
import { useNodePingDisplay } from '@/hooks/useNodePingDisplay'
import { vclass } from '@/lib/vclass'
import '@/scoped/node-card.css'
import { scope } from '@/scoped/scope'
import { useApp } from '@/stores/app'
import type { NodeData } from '@/stores/nodes'

// The scoped stylesheet only targets .node-card (the CardX root), so the
// scope attribute goes on the root; Vue also stamps it on slot content, which
// no rule matches.
const s = scope('node-card')

interface ExpiryInfo {
  icon: string
  text?: string
  prefix?: string
  value?: string
  unit?: string
  className?: string
}

const NODE_METRIC_ICONS = {
  cpu: 'tabler:cpu',
  memory: 'icon-park-outline:memory',
  disk: 'tabler:server-2',
  traffic: 'tabler:arrows-transfer-up-down',
} as const

function getRegionAltText(region: string): string {
  return getRegionDisplayName(region) || getRegionCode(region)
}

function hasRegion(region: string | null | undefined): boolean {
  return Boolean(region?.trim())
}

export interface NodeCardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'onClick'> {
  node: NodeData
  reduceMotion?: boolean
  pingEnabled?: boolean
  onClick?: () => void
  onPingClick?: () => void
}

export default function NodeCard({ node, reduceMotion = false, pingEnabled = true, onClick, onPingClick, ...attrs }: NodeCardProps) {
  const app = useApp(a => ({
    nodeCardSize: a.nodeCardSize,
    byteDecimals: a.byteDecimals,
    privateFeaturesAllowed: a.privateFeaturesAllowed,
    hidePriceWhenLoggedOut: a.hidePriceWhenLoggedOut,
    lang: a.lang,
    isFavorite: a.favoriteNodeIdSet.has(node.uuid),
  }))
  const snap = useBox(snapshot)
  const isFavorite = app.isFavorite

  const isMiniNodeCard = app.nodeCardSize === 'mini'
  const nodeCardXSize = app.nodeCardSize === 'large' ? 'large' : 'medium'
  const nodeCardContentClass = app.nodeCardSize === 'large' ? 'gap-4' : isMiniNodeCard ? 'gap-2' : 'gap-3'
  const nodeCardContentPaddingClass = isMiniNodeCard ? 'pb-2' : ''
  const nodeCardMetricGridClass = 'grid-cols-3'
  const nodeCardMetricBoxClass = isMiniNodeCard ? 'px-1 py-1' : app.nodeCardSize === 'compact' ? 'px-1.5 py-1.5' : 'px-2 py-1.5'
  const nodeCardPanelClass = app.nodeCardSize === 'large' ? 'h-14' : app.nodeCardSize === 'comfortable' ? 'h-12' : isMiniNodeCard ? 'h-7' : 'h-11'
  const nodeCardPingPanelClass = isMiniNodeCard ? 'gap-1 p-1' : 'gap-1.5 p-2'

  const formatBytes = (bytes: number) => formatBytesWithConfig(bytes, app.byteDecimals)
  const formatBytesPerSecond = (bytes: number) => formatBytesPerSecondWithConfig(bytes, app.byteDecimals)
  const offlineTime = formatDateTime(node.time)

  const cpuStatus = getStatus(node.cpu ?? 0)
  const memPercentage = getMemoryPercentage(node)
  const memStatus = getStatus(memPercentage)
  const swapUsed = formatBytes(Math.max(0, node.swap ?? 0))
  const swapTotal = Math.max(0, node.swap_total ?? 0)
  const swapTooltip = swapTotal > 0 ? `Swap 已用 ${swapUsed} / 总计 ${formatBytes(swapTotal)}` : `Swap 已用 ${swapUsed}`

  const threeNetwork = selectPings(snap?.nodes.find(n => String(n.id) === node.uuid), snap?.carrier_ping ?? false).threeNetwork

  const {
    latencyRenderBars,
    lossRenderBars,
    latencyDisplay,
    lossDisplay,
    latencyPanelTooltip,
    lossPanelTooltip,
  } = useNodePingDisplay(node.uuid, { enabled: pingEnabled && !threeNetwork })

  const trafficUsedPercentage = getTrafficUsedPercentage(node)
  const trafficUsed = getTrafficUsed(node)
  const nodeMessage = node.message?.trim() ?? ''
  const nodeMessageTooltip = nodeMessage
    ? `${nodeMessage}${node.status_updated_at ? `\n更新时间：${formatDateTime(node.status_updated_at)}` : ''}`
    : ''

  const limited = hasTrafficLimit(node)
  const trafficStatus = !limited ? 'success' : trafficUsedPercentage >= 95 ? 'error' : trafficUsedPercentage >= 80 ? 'warning' : trafficUsedPercentage >= 60 ? 'info' : 'success'
  const trafficPercentageClass = !limited ? 'text-muted-foreground' : trafficUsedPercentage >= 95 ? 'text-destructive' : trafficUsedPercentage >= 80 ? 'text-warning' : trafficUsedPercentage >= 60 ? 'text-warning' : 'text-success'

  const showPrice = app.privateFeaturesAllowed || !app.hidePriceWhenLoggedOut

  const uptimeDaysText = !Number.isFinite(node.uptime)
    ? '—'
    : language === 'zh-CN' ? `在线 ${getUptimeDays(node.uptime)} 天` : `${t('在线')} ${getUptimeDays(node.uptime)} ${t('天')}`

  const bandwidthText = captainBandwidth(node.uuid, app.lang)
  const priceText = showPrice ? captainPrice(node.uuid, app.lang) : ''
  const cpuSpecText = captainCPUSpec(node.uuid, app.lang)

  // Keep the two metadata rows stable even when one of their values is absent.
  const asymmetricBandwidth = bandwidthText.startsWith('RX ')
  const compactBandwidthText = asymmetricBandwidth
    ? bandwidthText.replaceAll(' Gbps', 'G').replaceAll(' Mbps', 'M').replace('RX ', '↓').replace(' · TX ', ' ↑')
    : bandwidthText
  const expiryInfo: ExpiryInfo | null = (() => {
    if (!shows('system') || !node.expired_at) return null
    const days = getDaysUntilExpired(node.expired_at)
    const status = getExpireStatus(node.expired_at)
    const className = status === 'expired' || status === 'critical'
      ? 'text-destructive'
      : status === 'warning' ? 'text-warning' : 'text-muted-foreground'
    const base = { icon: 'tabler:calendar-stats', className }
    if (status === 'unknown') return { ...base, text: '—' }
    if (status === 'expired') return { ...base, text: t('已过期') }
    if (status === 'long_term') return { ...base, text: t('长期') }
    return { ...base, prefix: t('剩余'), value: String(days), unit: t('天') }
  })()

  const customTags = parseTags(node.tags).map(tag => tag.text)

  function handleKeyboardOpen(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Enter' && event.key !== ' ')
      return
    event.preventDefault()
    onClick?.()
  }

  const pingClick = (e: { stopPropagation: () => void }) => {
    e.stopPropagation()
    onPingClick?.()
  }

  const trafficLine = (
    <>
      {`${formatBytes(trafficUsed)} `}
      {limited ? ` / ${formatBytes(node.traffic_limit)}` : ' / ∞ '}
    </>
  )

  const trafficBlock = (iconSize: string, iconClass: string) => (
    <div className="flex flex-col gap-1">
      <div className="flex justify-between text-xs">
        <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
          <Icon icon={NODE_METRIC_ICONS.traffic} data-node-metric-icon="traffic" width={iconSize} height={iconSize} className={iconClass} aria-hidden="true" />
          <span className="truncate">{t('流量')}</span>
        </span>
        <span className={vclass('tabular-nums font-medium', trafficPercentageClass)}>
          {limited ? `${trafficUsedPercentage.toFixed(1)}%` : '∞'}
        </span>
      </div>
      <ProgressThin percentage={trafficUsedPercentage} status={trafficStatus} height={4} />
      <div className={vclass('text-[11px] truncate', trafficUsedPercentage >= 95 ? 'text-destructive' : 'text-muted-foreground')}>
        {trafficLine}
      </div>
    </div>
  )

  const pingPanel = (metric: 'latency' | 'loss') => {
    const bars = metric === 'latency' ? latencyRenderBars : lossRenderBars
    return (
      <button
        type="button"
        className={vclass('group/panel relative flex flex-col rounded-lg bg-slate-500/5', [nodeCardPingPanelClass, nodeCardPanelClass, !node.online ? 'blur-xs opacity-50' : ''])}
        title={metric === 'latency' ? latencyPanelTooltip : lossPanelTooltip}
        aria-label={`${node.name} ${metric === 'latency' ? '延迟监测' : '丢包监测'}`}
        onClick={pingClick}
      >
        <div className="flex items-center justify-between text-[11px] leading-none">
          <span className="text-muted-foreground">{t(metric === 'latency' ? '延迟' : '丢包')}</span>
          <span className="font-medium">{metric === 'latency' ? latencyDisplay : lossDisplay}</span>
        </div>
        <div
          data-node-ping-bars={metric}
          className="grid min-h-0 min-w-0 w-full flex-1 items-end gap-[1px] opacity-80 group-hover/panel:opacity-100"
          style={{ gridTemplateColumns: `repeat(${bars.length}, minmax(0, 1fr))` }}
        >
          {bars.map(bar => (
            <DataTooltip key={bar.key} placement="top" content={bar.tooltip} className="h-full w-full">
              <span className={vclass('block h-full w-full rounded-[1px] transition-transform duration-150 group-hover/data-tooltip:scale-y-160 group-hover/panel:opacity-60 group-hover/data-tooltip:!opacity-100', bar.className)} />
            </DataTooltip>
          ))}
        </div>
      </button>
    )
  }

  return (
    <CardX
      {...attrs}
      {...s}
      hoverable
      size={nodeCardXSize}
      contentClass={nodeCardContentPaddingClass}
      className={vclass('node-card w-full cursor-pointer border-none shadow-[0_0_0_3px] shadow-transparent transition-all duration-200 rounded-xl', [!node.online && '!shadow-destructive/30'], attrs.className)}
      role="button"
      tabIndex={0}
      aria-label={`查看节点 ${node.name} 详情`}
      onClick={() => onClick?.()}
      onKeyDown={handleKeyboardOpen}
      header={(
        <div className="flex items-center gap-2 min-w-0">
          <div className="relative size-2.5 shrink-0">
            <span className={vclass('size-2.5 rounded-full block', node.online ? 'bg-success' : 'bg-destructive')} />
            {!reduceMotion && <span className={vclass('animate-ping absolute inset-0 rounded-full opacity-60', node.online ? 'bg-success' : 'bg-destructive')} />}
          </div>
          <span className="text-sm font-bold flex-1 min-w-0 truncate">{node.name}</span>
          {nodeMessage && (
            <DataTooltip
              content={nodeMessageTooltip}
              placement="top"
              as="span"
              className="inline-flex shrink-0 text-amber-500"
              contentClass="w-56 whitespace-pre-line leading-snug text-left"
            >
              <Icon icon="tabler:alert-triangle-filled" width="14" height="14" aria-label="节点消息" />
            </DataTooltip>
          )}
        </div>
      )}
      headerExtra={(
        <div className="flex gap-1.5 items-center shrink-0">
          {isFavorite && <span data-node-favorite role="img" aria-label={t('收藏')} title={t('收藏')} className="inline-flex size-5 items-center justify-center text-amber-500">
            <Icon icon="tabler:star-filled" width="14" height="14" aria-hidden="true" />
          </span>}
          <img src={getOSImage(node.os)} alt={getOSName(node.os)} className="size-4" />
          {hasRegion(node.region) && (
            <img
              src={`./images/flags/${getRegionCode(node.region)}.svg`}
              alt={getRegionAltText(node.region)}
              className="size-5 shrink-0"
            />
          )}
        </div>
      )}
    >
      <div className={vclass('flex flex-col relative', nodeCardContentClass)}>
        <div className="relative z-20 flex items-center gap-1.5 -mt-1 h-[19px] overflow-hidden">
          {shows('system') && (
            <span className="shrink-0 text-[11px] px-2 py-0.5 rounded-full bg-slate-500/10 text-muted-foreground leading-tight">
              {uptimeDaysText}
            </span>
          )}
          {priceText && (
            <span className="min-w-0 truncate text-[11px] px-2 py-0.5 rounded-full bg-slate-500/10 text-muted-foreground leading-tight">
              {priceText}
            </span>
          )}
        </div>

        {isMiniNodeCard
          ? (
              <div className="grid grid-cols-[3fr_2fr] gap-x-4 gap-y-2">
                <div className="grid grid-cols-2 gap-x-3 gap-y-1">
                  <div className="flex flex-col gap-1">
                    <div className="flex justify-between text-xs">
                      <span className="inline-flex items-center text-sky-500" role="img" title="CPU" aria-label="CPU">
                        <Icon icon={NODE_METRIC_ICONS.cpu} data-node-metric-icon="cpu" width="12" height="12" aria-hidden="true" />
                      </span>
                      <span className="tabular-nums font-medium">{`${formatNumber(node.cpu ?? Number.NaN, 1)}%`}</span>
                    </div>
                    <ProgressThin percentage={node.cpu ?? 0} status={cpuStatus} height={4} />
                  </div>
                  <div className="flex flex-col gap-1" title={swapTooltip}>
                    <div className="flex justify-between text-xs">
                      <span className="inline-flex items-center text-emerald-500" role="img" title={t('内存')} aria-label={t('内存')}>
                        <Icon icon={NODE_METRIC_ICONS.memory} data-node-metric-icon="memory" width="12" height="12" aria-hidden="true" />
                      </span>
                      <span className="tabular-nums font-medium">{`${formatNumber(memPercentage)}%`}</span>
                    </div>
                    <ProgressThin percentage={memPercentage} status={memStatus} height={4} />
                  </div>
                  <div className="col-span-2 text-[11px] text-muted-foreground truncate">
                    {`${formatBytes(node.ram ?? 0)} / ${formatBytes(node.mem_total ?? 0)}`}
                  </div>
                </div>
                {shows('traffic') && trafficBlock('12', 'shrink-0 text-violet-500')}
              </div>
            )
          : (
              <div className="grid grid-cols-2 gap-x-4 gap-y-2.5">
                {shows('cpu') && (
                  <div className="flex flex-col gap-1">
                    <div className="flex justify-between text-xs">
                      <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
                        <Icon icon={NODE_METRIC_ICONS.cpu} data-node-metric-icon="cpu" width="13" height="13" className="shrink-0 text-sky-500" aria-hidden="true" />
                        <span className="truncate">CPU</span>
                      </span>
                      <span className="tabular-nums font-medium">{`${formatNumber(node.cpu ?? Number.NaN, 1)}%`}</span>
                    </div>
                    <ProgressThin percentage={node.cpu ?? 0} status={cpuStatus} height={4} />
                    <div className="text-[11px] text-muted-foreground truncate min-h-[1lh]" data-node-cpu-spec title={cpuSpecText || undefined}>
                      {cpuSpecText}
                    </div>
                  </div>
                )}
                {shows('memory') && (
                  <div className="flex flex-col gap-1" title={swapTooltip}>
                    <div className="flex justify-between text-xs">
                      <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
                        <Icon icon={NODE_METRIC_ICONS.memory} data-node-metric-icon="memory" width="13" height="13" className="shrink-0 text-emerald-500" aria-hidden="true" />
                        <span className="truncate">{t('内存')}</span>
                      </span>
                      <span className="tabular-nums font-medium">{`${formatNumber(memPercentage)}%`}</span>
                    </div>
                    <ProgressThin percentage={memPercentage} status={memStatus} height={4} />
                    <div className="text-[11px] text-muted-foreground truncate">
                      {`${formatBytes(node.ram ?? 0)} / ${formatBytes(node.mem_total ?? 0)}`}
                    </div>
                  </div>
                )}
                {shows('disk') && <StorageSummary uuid={node.uuid} />}
                {shows('traffic') && trafficBlock('13', 'shrink-0 text-violet-500')}
              </div>
            )}

        <div className={vclass('grid gap-1.5', nodeCardMetricGridClass)}>
          {shows('network') && (
            <div className={vclass('flex flex-col gap-0.5 rounded-lg bg-slate-500/5 min-w-0 overflow-hidden', nodeCardMetricBoxClass)}>
              <div className="text-[11px] text-success flex items-center gap-1">
                <Icon icon="tabler:chevron-up" width="11" height="11" />
                <span className="truncate min-w-0 overflow-hidden">{formatBytesPerSecond(node.net_out ?? 0)}</span>
              </div>
              <div className="text-[11px] text-blue-600 flex items-center gap-1">
                <Icon icon="tabler:chevron-down" width="11" height="11" />
                <span className="truncate min-w-0 overflow-hidden">{formatBytesPerSecond(node.net_in ?? 0)}</span>
              </div>
            </div>
          )}
          {shows('traffic') && (
            <div title={t('周期流量说明')} className={vclass('flex flex-col gap-0.5 rounded-lg bg-slate-500/5 min-w-0 overflow-hidden', nodeCardMetricBoxClass)}>
              <div className="text-[11px] text-muted-foreground flex items-center gap-1">
                <Icon {...{ title: t('本周期出站') }} icon="tabler:upload" width="11" height="11" />
                <span className="truncate min-w-0 overflow-hidden">{formatBytes(node.net_total_up ?? Number.NaN)}</span>
              </div>
              <div className="text-[11px] text-muted-foreground flex items-center gap-1">
                <Icon {...{ title: t('本周期入站') }} icon="tabler:download" width="11" height="11" />
                <span className="truncate min-w-0 overflow-hidden">{formatBytes(node.net_total_down ?? Number.NaN)}</span>
              </div>
            </div>
          )}
          {(shows('system') || shows('info')) && (
            <div className={vclass('col-start-3 grid grid-rows-[1rem_1rem] gap-0.5 rounded-lg bg-slate-500/5 min-w-0 overflow-hidden', nodeCardMetricBoxClass)}>
              <div data-node-expiry className={vclass('text-[11px] leading-4 flex items-center gap-0.5 min-w-0', expiryInfo?.className ?? 'text-muted-foreground')}>
                {expiryInfo && <>
                  <Icon icon={expiryInfo.icon} width="11" height="11" className="shrink-0" />
                  <span className="truncate min-w-0" title={expiryInfo.text ?? `${expiryInfo.prefix} ${expiryInfo.value} ${expiryInfo.unit}`}>
                    {expiryInfo.text ?? <>{expiryInfo.prefix} <span className="tabular-nums">{expiryInfo.value}</span> {expiryInfo.unit}</>}
                  </span>
                </>}
              </div>
              <div data-node-bandwidth className="text-[11px] leading-4 text-muted-foreground flex items-center gap-1 min-w-0" title={bandwidthText ? `${t('标称带宽')}: ${bandwidthText}` : undefined} aria-label={bandwidthText ? `${t('标称带宽')}: ${bandwidthText}` : undefined}>
                {bandwidthText && <>
                  {!asymmetricBandwidth && <Icon icon="tabler:gauge" width="11" height="11" className="shrink-0" aria-hidden="true" />}
                  <span className="truncate min-w-0">{compactBandwidthText}</span>
                </>}
              </div>
            </div>
          )}
        </div>

        {shows('latency') && threeNetwork
          ? <ThreeNetworkPing uuid={node.uuid} nodeName={node.name} online={node.online} enabled={pingEnabled} onClick={() => onPingClick?.()} />
          : shows('latency')
            ? (
                <div className="grid grid-cols-2 gap-1.5">
                  {pingPanel('latency')}
                  {pingPanel('loss')}
                </div>
              )
            : null}

        {customTags.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {customTags.map((tag, i) => (
              <Badge key={i} variant="outline" className="!text-[11px] rounded-full text-muted-foreground border-muted-foreground/15 px-2 py-0">
                {tag}
              </Badge>
            ))}
          </div>
        )}

        {!node.online && (
          <div className="absolute inset-0 flex flex-col items-center justify-center z-10 rounded-xl bg-white/20 dark:bg-black/20 backdrop-blur-[2px]">
            <div className="text-sm font-semibold text-destructive">
              {t('离线')}
            </div>
            <div className="text-[11px] text-muted-foreground mt-1">
              {offlineTime}
            </div>
          </div>
        )}
      </div>
    </CardX>
  )
}
