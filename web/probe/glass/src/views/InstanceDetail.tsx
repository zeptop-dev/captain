import { useEffect, useMemo, useRef, useState } from 'react'
import { asyncComponent } from '@/lib/asyncComponent'
import type { ChangeEvent, Ref } from 'react'
import { Icon } from '@iconify/react'
import { t } from '@shared/i18n'
import { shows, snapshot, captainBandwidth, captainStorage } from '@shared/captain'
import { LOAD_RECORD_MAX_COUNT } from '@shared/constants/load'
import { loadNodeLoadRecords } from '@shared/services/history.service'
import { getCpuBenchmarkRating, getPassMarkCpuLookupUrl } from '@shared/utils/cpuBenchmark'
import type { CurrencyCode } from '@shared/utils/financeHelper'
import * as financeHelper from '@shared/utils/financeHelper'
import { getOSImage, getOSName } from '@shared/utils/osImageHelper'
import { getRegionCode, getRegionDisplayName } from '@shared/utils/regionHelper'
import { parseTags } from '@shared/utils/tagHelper'
import type { DetailContext } from '@shared/views/instanceDetail'
import { detailFormatters, hardwareSmallItems, metricCards, storageInfo, systemInfo } from '@shared/views/instanceDetail'
import { StorageVolumes } from '@/components/StorageSummary'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CardX } from '@/components/ui/card-x'
import { DataTooltip } from '@/components/ui/data-tooltip'
import { Empty } from '@/components/ui/empty'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useBox } from '@/hooks/useBox'
import { useNodeProviderMetadata } from '@/hooks/useNodeProviderMetadata'
import { useRoute, useRouter } from '@/lib/router'
import { appStore, useApp } from '@/stores/app'
import { useNodes } from '@/stores/nodes'

const LoadChart = asyncComponent(() => import('@/components/LoadChart'))
const PingChart = asyncComponent(() => import('@/components/PingChart'))

const CPU_TIER_PERCENT = { 'S': 92, 'A': 76, 'B': 58, 'C': 38, 'D': 20, '?': 0 } as const
const CPU_TIER_TEXT = { 'S': 'text-yellow-500', 'A': 'text-blue-500', 'B': 'text-green-500', 'C': 'text-orange-500', 'D': 'text-red-400', '?': 'text-muted-foreground' } as const
const CPU_TIER_BAR = { 'S': 'bg-yellow-500', 'A': 'bg-blue-500', 'B': 'bg-green-500', 'C': 'bg-orange-500', 'D': 'bg-red-400', '?': 'bg-slate-400' } as const

const getRegionAltText = (region: string) => getRegionDisplayName(region) || getRegionCode(region)

export default function InstanceDetail({ ref }: { ref?: Ref<HTMLDivElement> }) {
  const route = useRoute()
  const router = useRouter()
  const snap = useBox(snapshot)
  const app = useApp(a => ({
    lang: a.lang,
    byteDecimals: a.byteDecimals,
    privateFeaturesAllowed: a.privateFeaturesAllowed,
    hidePriceWhenLoggedOut: a.hidePriceWhenLoggedOut,
    providerAliases: a.providerAliases,
    nodeDetailSectionTabsEnabled: a.nodeDetailSectionTabsEnabled,
    detailMetricCardOrder: a.detailMetricCardOrder,
    favoriteNodeIdSet: a.favoriteNodeIdSet,
  }))
  const detailNodes = useNodes(n => n.visibleNodes)
  const visibleNodesByUuid = useNodes(n => n.visibleNodesByUuid)
  const data = visibleNodesByUuid.get(String(route.params.id))

  const [exchangeRates, setExchangeRates] = useState(financeHelper.DEFAULT_EXCHANGE_RATES)
  const [financeCurrency, setFinanceCurrency] = useState<CurrencyCode>('CNY')
  const [peakNetOut, setPeakNetOut] = useState(0)
  const [peakNetIn, setPeakNetIn] = useState(0)
  const [activeDetailSection, setActiveDetailSection] = useState<'overview' | 'load' | 'ping'>('overview')
  const trafficPeakSeq = useRef(0)

  const detailNodeIndex = detailNodes.findIndex(node => node.uuid === data?.uuid)
  const isFavoriteNode = data ? app.favoriteNodeIdSet.has(data.uuid) : false

  const providerNodes = useMemo(() => data ? [data] : [], [data])
  const { getNodeProviderMetadata } = useNodeProviderMetadata({
    nodes: providerNodes,
    customAliases: app.providerAliases,
    enabled: Boolean(data),
    allowGeoLookup: app.privateFeaturesAllowed,
    geoPermission: 'providerGeoLookup',
  })

  useEffect(() => {
    window.scrollTo({ top: 0, behavior: 'instant' })
    setFinanceCurrency(financeHelper.getStoredFinanceCurrency())
    let cancelled = false
    void financeHelper.getDailyExchangeRates().then(({ rates }) => {
      if (!cancelled)
        setExchangeRates(rates)
    })
    return () => {
      cancelled = true
    }
  }, [])

  // Vue: immediate watcher on the node — resets the section and loads the 24h peak.
  const dataUuid = data?.uuid
  const lastData = useRef<typeof data | null>(null)
  if (lastData.current !== data) {
    lastData.current = data
    if (activeDetailSection !== 'overview')
      setActiveDetailSection('overview')
  }
  useEffect(() => {
    if (!dataUuid)
      return
    const seq = ++trafficPeakSeq.current
    setPeakNetOut(0)
    setPeakNetIn(0)
    const load = async (): Promise<Array<{ net_in?: number, net_out?: number }>> => {
      if (!appStore.getState().privateFeaturesAllowed)
        return []
      try {
        return await loadNodeLoadRecords(dataUuid, 24, LOAD_RECORD_MAX_COUNT)
      }
      catch {
        return []
      }
    }
    void load().then((records) => {
      if (seq !== trafficPeakSeq.current)
        return
      let up = 0
      let down = 0
      for (const r of records) {
        if (typeof r.net_out === 'number' && r.net_out > up)
          up = r.net_out
        if (typeof r.net_in === 'number' && r.net_in > down)
          down = r.net_in
      }
      setPeakNetOut(up)
      setPeakNetIn(down)
    })
  }, [dataUuid])

  const providerMetadata = data ? getNodeProviderMetadata(data) : null
  const vpsProvider = providerMetadata?.provider ?? null
  const ctx: DetailContext = {
    data,
    snapshot: snap,
    lang: app.lang,
    byteDecimals: app.byteDecimals,
    privateFeaturesAllowed: app.privateFeaturesAllowed,
    hidePriceWhenLoggedOut: app.hidePriceWhenLoggedOut,
    exchangeRates,
    financeCurrency,
    providerMetadata,
  }
  const { formatBytes, formatBytesPerSecond } = detailFormatters(ctx)

  const cpuBenchmarkUrl = getPassMarkCpuLookupUrl(data?.cpu_name ?? '')
  const cpuBenchmarkRating = getCpuBenchmarkRating(data?.cpu_name ?? '')
  const customTags = parseTags(data?.tags).map(tag => tag.text)
  const ipSupport: string[] = []
  if (data?.ipv4)
    ipSupport.push('IPv4')
  if (data?.ipv6)
    ipSupport.push('IPv6')
  const hasPeak = peakNetOut > 0 || peakNetIn > 0

  const trafficUsed = snap?.nodes.find(n => String(n.id) === data?.uuid)?.traffic.used ?? Number.NaN
  const hasTrafficLimit = (data?.traffic_limit ?? 0) > 0
  const trafficLimit = data?.traffic_limit ?? 0
  const trafficUsedPercentage = trafficLimit <= 0 ? 0 : Math.min((trafficUsed / trafficLimit) * 100, 100)
  const trafficUsageText = `${formatBytes(trafficUsed)} / ${hasTrafficLimit ? formatBytes(data?.traffic_limit ?? 0) : '∞'}`
  const trafficProgressClass = trafficUsedPercentage >= 80 ? 'bg-red-500/30' : trafficUsedPercentage >= 60 ? 'bg-amber-500/25' : 'bg-emerald-500/20'

  const cards = metricCards(ctx, app.detailMetricCardOrder, shows)
  const hardware = hardwareSmallItems(ctx)
  const system = systemInfo(ctx)
  const storage = storageInfo(ctx)
  const bandwidth = data ? captainBandwidth(data.uuid, app.lang) : ''

  function navigateDetailNode(offset: number): void {
    if (detailNodes.length < 2 || detailNodeIndex < 0)
      return
    const target = detailNodes[(detailNodeIndex + offset + detailNodes.length) % detailNodes.length]
    if (target)
      void router.push({ name: 'instance-detail', params: { id: target.uuid } })
  }

  function selectDetailNode(event: ChangeEvent<HTMLSelectElement>): void {
    const uuid = event.target.value
    if (uuid && uuid !== data?.uuid)
      void router.push({ name: 'instance-detail', params: { id: uuid } })
  }

  const overview = !app.nodeDetailSectionTabsEnabled || activeDetailSection === 'overview'

  return (
    <div ref={ref} className="instance-detail space-y-4">
      {!data
        ? (
            <div className="p-4">
              <CardX>
                <Empty
                  description="节点不存在或已被删除"
                  extra={(
                    <Button onClick={() => void router.push('/')}>
                      {t('返回首页')}
                    </Button>
                  )}
                />
              </CardX>
            </div>
          )
        : (
            <>
              <div className="px-4 flex flex-wrap gap-2 items-center sm:gap-4">
                <Button variant="ghost" size="icon-sm" className="bg-background/50 hover:bg-background" aria-label={t('返回首页')} onClick={() => void router.push('/')}>
                  <Icon icon="tabler:arrow-left" width={16} height={16} />
                </Button>
                <div className="min-w-0 text-lg font-bold flex gap-2 items-center">
                  {data.region?.trim() && <img src={`./images/flags/${getRegionCode(data.region)}.svg`} alt={getRegionAltText(data.region)} className="size-6" />}
                  <span className="truncate">{data.name}</span>
                </div>
                <Badge variant={data.online ? 'default' : 'destructive'} className="text-xs !rounded">
                  {data.online ? '在线' : '离线'}
                </Badge>
                {customTags.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {customTags.map((tag, i) => (
                      <Badge
                        key={i}
                        variant="outline"
                        className="!text-[11px] rounded text-muted-foreground border-muted-foreground/15 px-1.5 py-0"
                      >
                        {tag}
                      </Badge>
                    ))}
                  </div>
                )}
                <div className="ml-auto flex h-8 shrink-0 items-center gap-1 rounded-md bg-background/50 p-0.5 backdrop-blur-xs">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className={`size-7 rounded-sm shadow-none${isFavoriteNode ? ' text-amber-500' : ''}`}
                    aria-label={isFavoriteNode ? '取消收藏当前节点' : '收藏当前节点'}
                    title={isFavoriteNode ? '取消收藏' : '收藏节点'}
                    onClick={() => appStore.toggleFavoriteNode(data.uuid)}
                  >
                    <Icon icon={isFavoriteNode ? 'tabler:star-filled' : 'tabler:star'} width={14} height={14} />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-7 rounded-sm shadow-none"
                    disabled={detailNodes.length < 2}
                    aria-label={t('上一个节点')}
                    title={t('上一个节点')}
                    onClick={() => navigateDetailNode(-1)}
                  >
                    <Icon icon="tabler:chevron-left" width={14} height={14} />
                  </Button>
                  <select
                    value={data.uuid}
                    className="h-7 max-w-34 rounded-sm border-0 bg-transparent px-1 text-xs text-foreground outline-none sm:max-w-48"
                    aria-label="切换节点"
                    onChange={selectDetailNode}
                  >
                    {detailNodes.map(node => (
                      <option key={node.uuid} value={node.uuid}>
                        {node.name}
                      </option>
                    ))}
                  </select>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-7 rounded-sm shadow-none"
                    disabled={detailNodes.length < 2}
                    aria-label={t('下一个节点')}
                    title={t('下一个节点')}
                    onClick={() => navigateDetailNode(1)}
                  >
                    <Icon icon="tabler:chevron-right" width={14} height={14} />
                  </Button>
                </div>
                {vpsProvider && (
                  <DataTooltip
                    as="div"
                    placement="bottom"
                    content={vpsProvider.tooltipLines.join('\n')}
                    className="max-w-full"
                    contentClass="w-72 whitespace-pre-wrap break-words px-2 py-1.5 text-left leading-relaxed"
                  >
                    <div className="flex max-w-full items-center gap-1.5 rounded-full bg-background/50 px-3 py-1 text-xs text-muted-foreground">
                      <Icon icon={vpsProvider.primary.icon} width={14} height={14} className="shrink-0" />
                      <span className="whitespace-normal break-words leading-snug">{vpsProvider.displayName}</span>
                    </div>
                  </DataTooltip>
                )}
              </div>

              {app.nodeDetailSectionTabsEnabled && (
                <div className="px-4 overflow-x-auto">
                  <Tabs value={activeDetailSection} onValueChange={v => setActiveDetailSection(v as typeof activeDetailSection)} className="w-full">
                    <TabsList className="w-max h-8 bg-background/50 backdrop-blur-xl rounded-md">
                      <TabsTrigger value="overview" className="h-6.5 flex-none shrink-0 gap-1 text-xs border-none data-[state=active]:text-selection shadow-none rounded-sm">
                        <Icon icon="tabler:layout-dashboard" width={12} height={12} />
                        {` ${t('概览')}`}
                      </TabsTrigger>
                      <TabsTrigger value="load" className="h-6.5 flex-none shrink-0 gap-1 text-xs border-none data-[state=active]:text-selection shadow-none rounded-sm">
                        <Icon icon="tabler:activity" width={12} height={12} />
                        {` ${t('负载')}`}
                      </TabsTrigger>
                      <TabsTrigger value="ping" className="h-6.5 flex-none shrink-0 gap-1 text-xs border-none data-[state=active]:text-selection shadow-none rounded-sm">
                        <Icon icon="tabler:timeline" width={12} height={12} />
                        {` ${t('延迟')}`}
                      </TabsTrigger>
                    </TabsList>
                  </Tabs>
                </div>
              )}

              {overview && (
                <div className="px-4 grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4">
                  {cards.map(item => (
                    <CardX
                      key={item.key}
                      hoverable
                      size="small"
                      className="group h-full bg-background/50 border-none hover:bg-background transition-all rounded-md"
                      contentClass="h-full !p-3"
                    >
                      <div title={item.tooltip} className="flex h-full min-h-10 md:min-h-18 flex-col justify-between gap-3">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-xs font-medium tracking-wider text-muted-foreground">{item.label}</span>
                          <Icon icon={item.icon} width={20} height={20} className="text-slate-500/25 transition-colors group-hover:text-slate-500" />
                        </div>
                        <div className="min-w-0 space-y-1">
                          <div className={`flex min-w-0 items-baseline gap-1 truncate font-semibold leading-none${item.valueClass ? ` ${item.valueClass}` : ''}`}>
                            <span className="truncate text-base sm:text-2xl">{item.value}</span>
                            {item.unit && <span className="shrink-0 text-[11px] font-medium text-muted-foreground sm:text-xs">{item.unit}</span>}
                          </div>
                        </div>
                      </div>
                    </CardX>
                  ))}
                </div>
              )}

              {overview && (
                <div className="px-4 gap-4 grid grid-cols-1 lg:grid-cols-2">
                  {shows('system') && (
                    <CardX
                      title={t('硬件信息')}
                      size="small"
                      contentClass="flex-1"
                      className="group h-full bg-background/50 border-none hover:bg-background transition-all rounded-md"
                    >
                      <div className="flex flex-col gap-3 h-full">
                        <div className="min-w-0 flex flex-col gap-2 rounded-sm bg-slate-500/5 p-2">
                          <div className="flex items-center justify-between gap-2 text-muted-foreground">
                            <div className="flex min-w-0 items-center gap-1">
                              <Icon icon="icon-park-outline:cpu" width={14} height={14} />
                              <span className="text-xs sm:text-sm">CPU</span>
                            </div>
                            <a
                              href={cpuBenchmarkUrl}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="ml-auto inline-flex shrink-0 items-center gap-1.5 rounded-sm bg-slate-500/8 px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-slate-500/12 hover:text-foreground"
                              title="在 PassMark 查看该 CPU 的公开 CPU Mark 跑分与排行"
                            >
                              <Icon icon="tabler:chart-bar" width={13} height={13} />
                              <span className="hidden sm:inline">CPU Mark 排行</span>
                              <span className="sm:hidden">CPU Mark</span>
                              <Icon icon="tabler:external-link" width={11} height={11} />
                            </a>
                          </div>
                          <span className="text-xs sm:text-sm break-all">{`${data.cpu_name} (${data.cpu_cores} vCPU)`}</span>
                          <div
                            className="mt-1 flex items-center gap-2"
                            title={`参考公开天梯与型号代际的本地近似分级，不代表当前 ${data.cpu_cores} vCPU 的实测性能。${cpuBenchmarkRating.description}`}
                          >
                            <span className={`shrink-0 text-xs font-bold ${CPU_TIER_TEXT[cpuBenchmarkRating.tier]}`}>{cpuBenchmarkRating.tier}</span>
                            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-slate-500/10">
                              <div
                                className={`h-full rounded-full transition-all duration-700 ${CPU_TIER_BAR[cpuBenchmarkRating.tier]}`}
                                style={{ width: `${CPU_TIER_PERCENT[cpuBenchmarkRating.tier]}%` }}
                              />
                            </div>
                            <span className="shrink-0 text-[11px] text-muted-foreground">{cpuBenchmarkRating.label}</span>
                          </div>
                        </div>

                        <div className={`grid gap-3 flex-1 auto-rows-fr ${hardware.length <= 2 ? 'grid-cols-2' : 'grid-cols-3'}`}>
                          {hardware.map(item => (
                            <div key={item.label} className="min-w-0 flex flex-col gap-1 rounded-sm bg-slate-500/5 p-2">
                              <div className="flex gap-1 items-center text-muted-foreground">
                                {item.icon && <Icon icon={item.icon} width={14} height={14} />}
                                <span className="text-xs sm:text-sm">{item.label}</span>
                              </div>
                              <span className="text-xs sm:text-sm break-words">{item.value}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    </CardX>
                  )}

                  {shows('system') && (
                    <CardX
                      title={t('系统信息')}
                      size="small"
                      contentClass="flex-1"
                      className="group h-full bg-background/50 border-none hover:bg-background transition-all rounded-md"
                    >
                      <div className="gap-3 grid grid-cols-1 sm:grid-cols-2 h-full sm:auto-rows-fr">
                        {system.map(item => (
                          <div key={item.label} className="min-w-0 flex flex-col gap-1 rounded-sm bg-slate-500/5 p-2">
                            <div className="flex gap-1 items-center text-muted-foreground">
                              {item.icon && <Icon icon={item.icon} width={14} height={14} />}
                              <span className="text-xs sm:text-sm">{item.label}</span>
                            </div>
                            <div className="flex min-w-0 gap-2 items-center">
                              {item.label === '操作系统' && <img src={getOSImage(data.os)} alt={getOSName(data.os)} className="size-5 shrink-0" />}
                              <span className="text-xs sm:text-sm break-words">{item.value}</span>
                            </div>
                          </div>
                        ))}
                      </div>
                    </CardX>
                  )}

                  {(shows('memory') || shows('disk')) && (
                    <CardX
                      title={t('存储信息')}
                      size="small"
                      className="group h-full bg-background/50 border-none hover:bg-background transition-all rounded-md"
                    >
                      <div className="gap-3 grid grid-cols-2">
                        {storage.filter(item => item.label !== t('硬盘') && shows('memory')).map(item => (
                          <div key={item.label} className="min-w-0 flex flex-col gap-1 rounded-sm bg-slate-500/5 p-2">
                            <div className="flex gap-1 items-center text-muted-foreground">
                              {item.icon && <Icon icon={item.icon} width={14} height={14} />}
                              <span className="text-xs sm:text-sm">{item.label}</span>
                            </div>
                            <span className="text-xs sm:text-sm break-words">{item.value}</span>
                          </div>
                        ))}
                      </div>
                      {shows('disk') && <div className={shows('memory') ? 'mt-3' : ''}><StorageVolumes items={captainStorage(data.uuid, t)} /></div>}
                    </CardX>
                  )}

                  {(shows('network') || shows('traffic') || bandwidth) && (
                    <CardX
                      title={t('网络信息')}
                      size="small"
                      className="group h-full bg-background/50 border-none hover:bg-background transition-all rounded-md"
                      contentClass="pt-0"
                    >
                      <div className="gap-3 grid grid-cols-2" data-network-info>
                        {shows('traffic') && (
                          <div title={t('周期流量说明')} className="relative min-w-0 overflow-hidden rounded-sm bg-slate-500/5 p-2">
                            {hasTrafficLimit && (
                              <div
                                className={`absolute inset-y-0 left-0 rounded-sm pointer-events-none transition-[width,background-color] duration-300 ease-out ${trafficProgressClass}`}
                                style={{ width: `${trafficUsedPercentage}%` }}
                              />
                            )}
                            <div className="relative flex flex-col gap-1.5">
                              <div className="flex gap-1 items-center text-muted-foreground">
                                <Icon icon="icon-park-outline:transfer-data" width={14} height={14} />
                                <span className="text-xs sm:text-sm">{t('流量配额')}</span>
                                {ipSupport.map(proto => (
                                  <Badge
                                    key={proto}
                                    variant="outline"
                                    className="!text-[10px] rounded text-emerald-600 border-emerald-600/25 px-1 py-0 leading-none"
                                  >
                                    {proto}
                                  </Badge>
                                ))}
                                <div className="flex-1" />
                                <span className="hidden sm:block text-[11px] font-medium text-foreground/70">
                                  {`${formatBytes(data?.net_total_up ?? Number.NaN)} / ${formatBytes(data?.net_total_down ?? Number.NaN)}`}
                                </span>
                              </div>
                              <span className="text-xs sm:text-sm break-all">{trafficUsageText}</span>
                              {shows('network') && hasPeak && (
                                <span className="text-[10px] text-muted-foreground/80 flex items-center gap-2 leading-none">
                                  <span>近一天峰值</span>
                                  <span className="text-green-600 flex items-center gap-0.5">
                                    <Icon icon="tabler:chevron-up" width="10" height="10" />
                                    {formatBytesPerSecond(peakNetOut)}
                                  </span>
                                  <span className="text-blue-600 flex items-center gap-0.5">
                                    <Icon icon="tabler:chevron-down" width="10" height="10" />
                                    {formatBytesPerSecond(peakNetIn)}
                                  </span>
                                </span>
                              )}
                            </div>
                          </div>
                        )}
                        {shows('network') && (
                          <div className="min-w-0 flex flex-col gap-1 rounded-sm bg-slate-500/5 p-2">
                            <div className="flex gap-1 items-center text-muted-foreground">
                              <Icon icon="icon-park-outline:dashboard-one" width={14} height={14} />
                              <span className="text-xs sm:text-sm">{t('网络速率')}</span>
                            </div>
                            <span className="text-xs sm:text-sm break-all flex flex-row flex-wrap items-center gap-1">
                              <Icon icon="tabler:chevron-up" width="12" height="12" />
                              {` ${formatBytesPerSecond(data?.net_out ?? 0)} `}
                              <span className="px-0.5" />
                              <Icon icon="tabler:chevron-down" width="12" height="12" />
                              {` ${formatBytesPerSecond(data?.net_in ?? 0)}`}
                            </span>
                          </div>
                        )}
                        {bandwidth && (
                          <div className="min-w-0 flex flex-col gap-1 rounded-sm bg-slate-500/5 p-2" data-advertised-bandwidth>
                            <div className="flex gap-1 items-center text-muted-foreground">
                              <Icon icon="tabler:bandwidth" width={14} height={14} className="shrink-0" />
                              <span className="text-xs sm:text-sm">{t('标称带宽')}</span>
                            </div>
                            <span className="text-xs sm:text-sm break-words">{bandwidth}</span>
                          </div>
                        )}
                      </div>
                    </CardX>
                  )}
                </div>
              )}

              <>
                {shows('history') && (!app.nodeDetailSectionTabsEnabled || activeDetailSection === 'load') && <LoadChart uuid={data.uuid} className="px-4" />}
                {shows('history') && shows('latency') && (!app.nodeDetailSectionTabsEnabled || activeDetailSection === 'ping') && <PingChart uuid={data.uuid} className="px-4" />}
              </>
            </>
          )}
    </div>
  )
}
