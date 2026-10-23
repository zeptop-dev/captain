import type { CSSProperties, HTMLAttributes, KeyboardEvent, ReactNode, Ref } from 'react'
import { Fragment, useMemo, useRef, useState } from 'react'
import { Icon } from '@iconify/react'
import { t } from '@shared/i18n'
import { shows, captainPrice, captainBandwidth } from '@shared/captain'
import { UI_CONFIG } from '@shared/constants/ui'
import { formatCityNameZh } from '@shared/utils/cityNameHelper'
import { formatBytesPerSecondWithConfig, formatBytesWithConfig, formatDateTime, formatNumber, formatUptimeWithFormat, getStatus } from '@shared/utils/helper'
import { getRealtimeTotalSpeed, getTrafficUsed, getTrafficUsedPercentage, hasTrafficLimit } from '@shared/utils/nodeMetricsHelper'
import { getOSImage, getOSName } from '@shared/utils/osImageHelper'
import { getRegionCode, getRegionDisplayName } from '@shared/utils/regionHelper'
import { getDaysUntilExpired, getExpireStatus, parseTags } from '@shared/utils/tagHelper'
import StorageSummary from '@/components/StorageSummary'
import NodePingListCell from '@/components/NodePingListCell'
import TrafficProgress from '@/components/TrafficProgress'
import TransitionGroup from '@/components/TransitionGroup'
import { Badge } from '@/components/ui/badge'
import { DataTooltip } from '@/components/ui/data-tooltip'
import { ProgressThin } from '@/components/ui/progress-thin'
import { useNodeProviderMetadata } from '@/hooks/useNodeProviderMetadata'
import { useVirtualList } from '@/hooks/useVirtualList'
import { vclass } from '@/lib/vclass'
import '@/scoped/node-list.css'
import { scope } from '@/scoped/scope'
import { useApp } from '@/stores/app'
import type { NodeData } from '@/stores/nodes'

const s = scope('node-list')

interface ColumnConfig {
  key: string
  label: string
  width: string | number
  sortable: boolean
}

interface NodeMetadataItem {
  key: string
  value: string
  title?: string
  icon?: string
  flagSrc?: string
  variant?: 'secondary' | 'outline'
  class?: string
  style?: CSSProperties
}

const rowStaggerMs = UI_CONFIG.motion.staggerMs
const rowStaggerLimit = UI_CONFIG.motion.staggerLimit
const VIRTUAL_LIST_THRESHOLD = UI_CONFIG.virtualList.nodeThreshold
const VIRTUAL_ROW_HEIGHT = UI_CONFIG.virtualList.nodeRowHeight
const COLUMN_SECTION: Record<string, string> = { cpu: 'cpu', mem: 'memory', disk: 'disk', traffic: 'traffic', rate: 'network', uptime: 'system', os: 'system', metadata: 'system' }

function baseColumns(): ColumnConfig[] {
  return [
    { key: 'status', label: t('状态'), width: '40px', sortable: false },
    { key: 'os', label: t('系统'), width: '44px', sortable: false },
    { key: 'name', label: t('节点'), width: 'minmax(180px, 0.85fr)', sortable: true },
    { key: 'metadata', label: t('信息'), width: 'minmax(240px, 1.1fr)', sortable: false },
    { key: 'uptime', label: t('运行时间'), width: '116px', sortable: true },
    { key: 'cpu', label: 'CPU', width: '100px', sortable: false },
    { key: 'mem', label: t('内存'), width: '100px', sortable: false },
    { key: 'disk', label: t('硬盘'), width: '100px', sortable: false },
    { key: 'traffic', label: t('流量'), width: '104px', sortable: false },
    { key: 'rate', label: t('速率'), width: '88px', sortable: true },
  ]
}

const getFlagSrc = (region: string) => `./images/flags/${getRegionCode(region)}.svg`
const getRegionAltText = (region: string) => getRegionDisplayName(region) || getRegionCode(region)
const hasRegion = (region: string | null | undefined) => Boolean(region?.trim())
const getNodeMessage = (node: NodeData) => node.message?.trim() ?? ''
function getNodeMessageTooltip(node: NodeData): string {
  const message = getNodeMessage(node)
  if (!message)
    return ''
  const updatedAt = node.status_updated_at ? `\n更新时间：${formatDateTime(node.status_updated_at)}` : ''
  return `${message}${updatedAt}`
}

/** One row; forwards its ref so the TransitionGroup can animate it. */
function Row({ ref, children, ...props }: HTMLAttributes<HTMLDivElement> & { ref?: Ref<HTMLDivElement>, children: ReactNode }) {
  return <div ref={ref} {...props}>{children}</div>
}

export interface NodeListProps extends Omit<HTMLAttributes<HTMLDivElement>, 'onClick'> {
  nodes: NodeData[]
  transitionKey?: string
  sortResetKey?: string
  pingEnabled: boolean
  onClick?: (node: NodeData) => void
  onPingClick?: (node: NodeData) => void
}

export default function NodeList({ nodes, transitionKey, sortResetKey, pingEnabled, onClick, onPingClick, ...attrs }: NodeListProps) {
  const app = useApp(a => ({
    privateFeaturesAllowed: a.privateFeaturesAllowed,
    hidePriceWhenLoggedOut: a.hidePriceWhenLoggedOut,
    nodeListMetadataEnabled: a.nodeListMetadataEnabled,
    nodeListMetadataFields: a.nodeListMetadataFields,
    nodeListCustomTagsVisible: a.nodeListCustomTagsVisible,
    providerAliases: a.providerAliases,
    disablePageAnimation: a.disablePageAnimation,
    byteDecimals: a.byteDecimals,
    lang: a.lang,
    favoriteNodeIdSet: a.favoriteNodeIdSet,
  }))
  const showPrice = app.privateFeaturesAllowed || !app.hidePriceWhenLoggedOut

  const columns = useMemo(() => baseColumns().filter(col => (col.key !== 'metadata' || app.nodeListMetadataEnabled) && (['status', 'name'].includes(col.key) || shows(COLUMN_SECTION[col.key] ?? ''))), [app.nodeListMetadataEnabled])
  const providerMetadataEnabled = app.nodeListMetadataEnabled && app.nodeListMetadataFields.some(field => field === 'provider' || field === 'city' || field === 'asn')
  const { getNodeProviderMetadata } = useNodeProviderMetadata({
    nodes,
    customAliases: app.providerAliases,
    enabled: providerMetadataEnabled,
    allowGeoLookup: app.privateFeaturesAllowed,
    geoPermission: 'providerGeoLookup',
  })

  const [sortKey, setSortKey] = useState('')
  const [sortDir, setSortDir] = useState<1 | -1>(1)
  // watch(sortResetKey) without immediate: reset during render on change.
  const lastResetKey = useRef(sortResetKey)
  if (lastResetKey.current !== sortResetKey) {
    lastResetKey.current = sortResetKey
    if (sortKey !== '')
      setSortKey('')
    if (sortDir !== 1)
      setSortDir(1)
  }

  function handleSort(col: ColumnConfig) {
    if (!col.sortable)
      return
    if (sortKey === col.key) {
      setSortDir(sortDir === 1 ? -1 : 1)
    }
    else {
      setSortKey(col.key)
      setSortDir(1)
    }
  }

  const sortedNodes = useMemo(() => {
    const list = [...nodes]
    if (!sortKey)
      return list
    const dir = sortDir
    const cmp = (va: string, vb: string) => dir * (va < vb ? -1 : va > vb ? 1 : 0)
    return list.sort((a, b) => {
      switch (sortKey) {
        case 'status': return dir * ((a.online ? 1 : 0) - (b.online ? 1 : 0))
        case 'region': return cmp((a.region || '').toLowerCase(), (b.region || '').toLowerCase())
        case 'name': return cmp((a.name || '').toLowerCase(), (b.name || '').toLowerCase())
        case 'uptime': return dir * ((a.uptime ?? 0) - (b.uptime ?? 0))
        case 'os': return cmp((a.os || '').toLowerCase(), (b.os || '').toLowerCase())
        case 'cpu': return dir * ((a.cpu ?? 0) - (b.cpu ?? 0))
        case 'mem': return dir * ((a.ram ?? 0) / (a.mem_total || 1) - (b.ram ?? 0) / (b.mem_total || 1))
        case 'disk': return dir * ((a.disk ?? 0) / (a.disk_total || 1) - (b.disk ?? 0) / (b.disk_total || 1))
        case 'traffic':
        case 'rate':
          return dir * (getRealtimeTotalSpeed(a) - getRealtimeTotalSpeed(b))
        default: return 0
      }
    })
  }, [nodes, sortKey, sortDir])

  const shouldVirtualizeNodes = sortedNodes.length > VIRTUAL_LIST_THRESHOLD
  const virtual = useVirtualList(sortedNodes, { itemHeight: VIRTUAL_ROW_HEIGHT, overscan: UI_CONFIG.virtualList.overscan })
  const renderedRows = shouldVirtualizeNodes ? virtual.list : sortedNodes.map((node, index) => ({ data: node, index }))

  const formatBytes = (bytes: number) => formatBytesWithConfig(bytes, app.byteDecimals)
  const formatBytesPerSecond = (bytes: number) => formatBytesPerSecondWithConfig(bytes, app.byteDecimals)
  const formatUptime = (seconds: number) => formatUptimeWithFormat(seconds, 'hour')

  const columnKeys = columns.map(c => c.key)
  const gridStyle: CSSProperties = { gridTemplateColumns: columns.map(c => c.width).join(' ') }
  const statusIndex = columnKeys.indexOf('status')
  const regionIndex = columnKeys.indexOf('region')
  const nameIndex = columnKeys.indexOf('name')
  const startColumn = nameIndex !== -1 ? nameIndex + 1 : regionIndex !== -1 ? regionIndex + 2 : statusIndex === -1 ? 1 : statusIndex + 2
  const offlineOverlayContentStyle: CSSProperties = { gridColumn: `${startColumn} / -1` }

  function getPriceTags(node: NodeData): string[] {
    const tags: string[] = []
    const lang = app.lang
    if (node.price !== 0) {
      const days = getDaysUntilExpired(node.expired_at)
      const status = getExpireStatus(node.expired_at)
      if (status === 'expired')
        tags.push(lang === 'zh-CN' ? t('已过期') : 'Expired')
      else if (status === 'long_term')
        tags.push(lang === 'zh-CN' ? t('长期') : 'Long-term')
      else tags.push(lang === 'zh-CN' ? `剩余 ${days} 天` : `${days} days left`)
      const price = captainPrice(node.uuid, lang)
      if (price) tags.push(price)
    }
    return tags
  }

  function buildNodeMetadataItems(node: NodeData): NodeMetadataItem[] {
    const items: NodeMetadataItem[] = []
    const bandwidth = captainBandwidth(node.uuid, app.lang)
    if (bandwidth) items.push({ key: 'bandwidth', value: bandwidth, title: `${t('标称带宽')}: ${bandwidth}`, icon: 'tabler:network', variant: 'secondary', class: 'max-w-full' })
    const providerMetadata = getNodeProviderMetadata(node)
    for (const field of app.nodeListMetadataFields) {
      switch (field) {
        case 'provider': {
          const provider = providerMetadata?.provider
          if (!provider?.displayName)
            break
          items.push({ key: 'provider', value: provider.displayName, icon: provider.primary.icon, title: provider.tooltipLines.length > 0 ? provider.tooltipLines.join('\n') : provider.displayName, variant: 'secondary', class: 'max-w-[9.5rem] bg-secondary/85 text-secondary-foreground border-border/40' })
          break
        }
        case 'region': {
          if (!hasRegion(node.region))
            break
          const displayName = getRegionDisplayName(node.region)
          if (!displayName)
            break
          items.push({ key: 'region', value: displayName, flagSrc: getFlagSrc(node.region), title: displayName, variant: 'outline', class: 'max-w-[8rem] bg-background/45 text-foreground/80 border-border/60' })
          break
        }
        case 'city': {
          const cityName = formatCityNameZh(providerMetadata?.geo?.city)
          if (!cityName)
            break
          items.push({ key: 'city', value: cityName, icon: 'tabler:map-pin', title: [cityName, providerMetadata?.geo?.countryCode].filter(Boolean).join(' · '), variant: 'outline', class: 'max-w-[8rem] bg-background/45 text-foreground/80 border-border/60' })
          break
        }
        case 'asn': {
          const asn = providerMetadata?.geo?.asn
          if (!asn)
            break
          items.push({ key: 'asn', value: asn, icon: 'tabler:network', title: providerMetadata?.geo?.org ? `${asn}\n${providerMetadata.geo.org}` : asn, variant: 'outline', class: 'max-w-[7rem] bg-info/10 text-info border-info/25 font-mono' })
          break
        }
        case 'tags': {
          if (!app.nodeListCustomTagsVisible)
            break
          parseTags(node.tags).forEach((tag, index) => {
            items.push({ key: `tag-${index}-${tag.text}`, value: tag.text, title: tag.text, icon: 'tabler:tag', variant: 'outline', class: 'max-w-[8rem] bg-background/45 text-foreground/80 border-border/60', style: tag.hex ? { borderColor: `${tag.hex}66`, color: tag.hex } : undefined })
          })
          break
        }
        case 'group': {
          node.groups.forEach((group, index) => {
            items.push({ key: `group-${index}-${group}`, value: group, title: group, icon: 'tabler:folder', variant: 'outline', class: 'max-w-[8rem] bg-background/45 text-foreground/80 border-border/60' })
          })
          break
        }
      }
    }
    return items
  }
  const metadataItemsByUuid: Record<string, NodeMetadataItem[]> = {}
  for (const node of nodes)
    metadataItemsByUuid[node.uuid] = buildNodeMetadataItems(node)

  function handleRowKeydown(event: KeyboardEvent<HTMLDivElement>, node: NodeData) {
    if (event.key !== 'Enter' && event.key !== ' ')
      return
    event.preventDefault()
    onClick?.(node)
  }

  const getRowTransitionKey = (node: NodeData) => transitionKey ? `${transitionKey}-${node.uuid}` : node.uuid
  const getRowTransitionStyle = (index: number) => ({ '--node-row-delay': `${Math.min(index, rowStaggerLimit) * rowStaggerMs}ms` }) as CSSProperties

  function cell(col: ColumnConfig, node: NodeData): ReactNode {
    const fav = app.favoriteNodeIdSet.has(node.uuid)
    switch (col.key) {
      case 'status':
        return (
          <div {...s} className="flex justify-center">
            <div {...s} className={vclass('size-2 rounded-full relative', [node.online ? 'bg-success' : 'bg-destructive'])}>
              <div {...s} className={vclass('animate-ping absolute inset-0 rounded-full opacity-50', [node.online ? 'bg-success' : 'bg-destructive'])} />
            </div>
          </div>
        )
      case 'name': {
        const priceTags = getPriceTags(node)
        return (
          <div {...s} className={vclass('space-y-0.5 min-w-0', [!node.online && 'blur-sm opacity-30'])}>
            <div {...s} className="flex gap-1.5 items-center text-[13px] font-semibold text-foreground min-w-0">
              {hasRegion(node.region) && <img {...s} src={getFlagSrc(node.region)} alt={getRegionAltText(node.region)} className="size-5 rounded-sm shrink-0" />}
              <span {...s} className="truncate">{node.name}</span>
              {fav && <span {...s} data-node-favorite role="img" aria-label={t('收藏')} title={t('收藏')} className="inline-flex size-5 shrink-0 items-center justify-center text-amber-500">
                <Icon {...s} icon="tabler:star-filled" width="13" height="13" aria-hidden="true" />
              </span>}
              {getNodeMessage(node) && (
                <DataTooltip {...s} content={getNodeMessageTooltip(node)} placement="top" as="span" className="inline-flex shrink-0 text-amber-500" contentClass="w-56 whitespace-pre-line leading-snug text-left">
                  <Icon {...s} icon="tabler:alert-triangle-filled" width="13" height="13" aria-label="节点消息" />
                </DataTooltip>
              )}
            </div>
            {showPrice && priceTags.length > 0 && (
              <div {...s} className="text-[11px] font-medium text-foreground/60 truncate">
                {priceTags.map((tag, tagIndex) => (
                  <span {...s} key={tagIndex} className={vclass(!!tagIndex && 'ml-3') || undefined}>{tag}</span>
                ))}
              </div>
            )}
          </div>
        )
      }
      case 'metadata': {
        const items = metadataItemsByUuid[node.uuid] ?? []
        return (
          <div {...s} className="min-w-0 overflow-hidden">
            {items.length > 0 && (
              <div {...s} className="flex flex-wrap gap-1 items-center max-h-11 overflow-hidden">
                {items.map(item => (
                  <Badge
                    {...s}
                    key={item.key}
                    variant={item.variant ?? 'secondary'}
                    title={item.title ?? item.value}
                    style={item.style}
                    className={vclass('min-w-0 overflow-hidden whitespace-nowrap rounded-md px-1.5 text-[11px] font-medium shadow-none', item.class)}
                  >
                    {item.flagSrc
                      ? <img {...s} src={item.flagSrc} alt={item.value} className="size-3.5 rounded-[2px] shrink-0" />
                      : item.icon ? <Icon {...s} icon={item.icon} width="12" height="12" className="shrink-0" /> : null}
                    <span {...s} className="truncate">{item.value}</span>
                  </Badge>
                ))}
              </div>
            )}
          </div>
        )
      }
      case 'uptime':
        return (
          <div {...s} className="flex flex-col gap-0.5 min-w-0">
            <span {...s} className="text-[11px] font-medium text-foreground/70 truncate">
              {formatUptime(node.uptime ?? 0)}
            </span>
            {shows('latency') && <NodePingListCell {...s} uuid={node.uuid} online={node.online} enabled={pingEnabled} onClick={() => onPingClick?.(node)} />}
          </div>
        )
      case 'os':
        return (
          <div {...s} className="flex justify-center">
            <img {...s} src={getOSImage(node.os)} alt={getOSName(node.os)} className="size-4.5" />
          </div>
        )
      case 'cpu':
        return (
          <div {...s} className="group min-w-0">
            <div {...s} className="space-y-1">
              <div {...s} className="text-[11px] font-medium text-foreground/75 truncate">
                <span {...s} className="inline group-hover:hidden">{`${formatNumber(node.cpu ?? Number.NaN, 1)}% `}</span>
                <span {...s} className="hidden group-hover:inline">{`${formatNumber(node.load ?? Number.NaN, 2)}, ${formatNumber(node.load5 ?? Number.NaN, 2)}, ${formatNumber(node.load15 ?? Number.NaN, 2)}`}</span>
              </div>
              <ProgressThin {...s} percentage={node.cpu ?? 0} status={getStatus(node.cpu ?? 0)} height={4} />
            </div>
          </div>
        )
      case 'mem': {
        const p = (node.ram ?? 0) / (node.mem_total || 1) * 100
        return (
          <div {...s} className="group min-w-0">
            <div {...s} className="space-y-1">
              <div {...s} className="text-[11px] font-medium text-foreground/75 truncate">
                <span {...s} className="inline group-hover:hidden">{`${formatNumber((node.ram ?? Number.NaN) / (node.mem_total || 1) * 100)}% `}</span>
                <span {...s} className="hidden group-hover:inline">{`${formatBytes(node.ram ?? 0)} / ${formatBytes(node.mem_total ?? 0)}`}</span>
              </div>
              <ProgressThin {...s} percentage={p} status={getStatus(p)} height={4} />
            </div>
          </div>
        )
      }
      case 'disk': return <StorageSummary uuid={node.uuid} list />
      case 'traffic':
        return (
          <div
            {...s}
            className="group min-w-0"
            title={`${t('本周期出站')} ${formatBytes(node.net_total_up ?? Number.NaN)}\n${t('本周期入站')} ${formatBytes(node.net_total_down ?? Number.NaN)}\n${t('周期流量说明')}`}
          >
            <div {...s} className="space-y-1 w-full">
              <div {...s} className="text-[11px] font-medium text-foreground/75 truncate">
                <span {...s} className="inline group-hover:hidden">{`${getTrafficUsedPercentage(node).toFixed(1)}% `}</span>
                <span {...s} className="hidden group-hover:inline">
                  {`${formatBytes(getTrafficUsed(node))} / `}
                  {hasTrafficLimit(node) ? formatBytes(node.traffic_limit) : '∞'}
                </span>
              </div>
              {Number.isFinite(node.net_total_up) && Number.isFinite(node.net_total_down)
                ? <TrafficProgress {...s} upload={node.net_total_up ?? 0} download={node.net_total_down ?? 0} trafficLimit={node.traffic_limit} trafficLimitType={node.traffic_limit_type || 'sum'} height="4px" />
                : <ProgressThin {...s} percentage={getTrafficUsedPercentage(node)} status={getStatus(getTrafficUsedPercentage(node))} height={4} />}
            </div>
          </div>
        )
      case 'rate':
        return (
          <div {...s} className="min-w-0">
            <div {...s} className="text-[11px] font-medium flex flex-col">
              <span {...s} className="text-success flex flex-row gap-1 items-center truncate">
                <Icon {...s} icon="tabler:chevron-up" width="12" height="12" className="shrink-0" />
                {` ${formatBytesPerSecond(node.net_out ?? 0)}`}
              </span>
              <span {...s} className="text-blue-600 dark:text-blue-400 flex flex-row gap-1 items-center truncate">
                <Icon {...s} icon="tabler:chevron-down" width="12" height="12" className="shrink-0" />
                {` ${formatBytesPerSecond(node.net_in ?? 0)}`}
              </span>
            </div>
          </div>
        )
      default:
        return null
    }
  }

  const rows = renderedRows.map(({ data: node, index }) => (
    <Row
      {...s}
      key={getRowTransitionKey(node)}
      className={vclass('flex flex-col relative h-16 min-h-16 max-h-16 overflow-hidden justify-center px-2.5 cursor-pointer bg-background/40 rounded-lg backdrop-blur-sm shadow-[0_0_0_2px] shadow-transparent hover:shadow-slate-500/10 hover:bg-background/70 transition-all', [!node.online && '!shadow-red-600/10'])}
      style={getRowTransitionStyle(index)}
      role="button"
      tabIndex={0}
      aria-label={`查看节点 ${node.name} 详情`}
      onClick={() => onClick?.(node)}
      onKeyDown={e => handleRowKeydown(e, node)}
    >
      <div {...s} className="grid gap-2 items-center overflow-hidden" style={gridStyle}>
        {columns.map(col => <Fragment key={col.key}>{cell(col, node)}</Fragment>)}
      </div>
      {!node.online && (
        <div {...s} className="absolute inset-0 z-2 p-2 bg-background/10 rounded-lg flex items-center" aria-hidden="true">
          <div {...s} className="grid gap-2 items-center justify-center" style={gridStyle}>
            <div {...s} className="h-full space-y-1" style={offlineOverlayContentStyle}>
              <div {...s} className="text-sm font-semibold truncate">
                <span {...s} className="text-destructive">{t('离线')}</span>
                {` ${node.name}`}
              </div>
              <div {...s} className="text-xs font-medium text-foreground/65">
                {formatDateTime(node.time)}
              </div>
            </div>
          </div>
        </div>
      )}
    </Row>
  ))

  return (
    <div {...attrs} {...s} className={vclass('overflow-x-auto overflow-y-hidden min-w-0 p-1 -m-1', attrs.className)}>
      <div {...s} className="min-w-fit w-full flex flex-col gap-1">
        <div {...s} className="grid px-2.5 py-2 bg-background/70 rounded-lg backdrop-blur-sm gap-2" style={gridStyle}>
          {columns.map(col => (
            <div
              {...s}
              key={col.key}
              className={vclass([col.sortable ? 'cursor-pointer select-none' : '', ['status', 'os'].includes(col.key) ? 'text-center' : 'text-left'])}
              onClick={() => handleSort(col)}
            >
              <span {...s} className="text-[11px] font-medium tracking-wide text-foreground/70">
                {`${col.label}${col.sortable && sortKey === col.key ? (sortDir === 1 ? ' ↑' : ' ↓') : ''}`}
              </span>
            </div>
          ))}
        </div>

        {shouldVirtualizeNodes
          ? (
              <div {...s} ref={virtual.containerProps.ref} onScroll={virtual.containerProps.onScroll} style={virtual.containerProps.style} className="max-h-[min(72vh,54rem)] overflow-y-auto pr-1">
                <div {...s} style={virtual.wrapperProps.style} className="flex flex-col gap-1">
                  {rows}
                </div>
              </div>
            )
          : (
              // Vue renders :class="''" as an empty class attribute here.
              <div {...s} className="">
                <TransitionGroup
                  {...s}
                  appear={!app.disablePageAnimation}
                  css={!app.disablePageAnimation}
                  name="node-row-switch"
                  tag="div"
                  className="flex flex-col gap-1"
                >
                  {rows}
                </TransitionGroup>
              </div>
            )}
      </div>
    </div>
  )
}
