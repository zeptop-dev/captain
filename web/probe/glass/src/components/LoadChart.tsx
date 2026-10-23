import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { HTMLAttributes } from 'react'
import { Icon } from '@iconify/react'
import dayjs from 'dayjs'
import { t } from '@shared/i18n'
import { captainStorage, shows, snapshot } from '@shared/captain'
import { storageItems } from '../../../src/lib/storage'
import { formatMetricCount, latestStoragePoint, type StorageHistoryRange } from '../../../src/lib/storage-history'
import { createLoadChartModel, LOAD_METRIC_KEYS } from '@shared/charts/load'
import { LOAD_RECORD_MAX_COUNT } from '@shared/constants/load'
import { loadMetricDefinitions, loadPublicPingTasks, queryMetrics } from '@shared/services/metrics.service'
import { getChartSeriesPalette, getLoadChartPalette } from '@shared/utils/chartPalette'
import { formatBytesSplit } from '@shared/utils/helper'
import type { NormalizedMetricSeries } from '@shared/utils/metricSeries'
import { normalizeMetricSeriesList } from '@shared/utils/metricSeries'
import type { RecordFormat } from '@shared/utils/recordHelper'
import type { MetricQueryParams, PingTaskInfo, StatusRecord } from '@shared/utils/rpc'
import { getSharedRpc } from '@shared/utils/rpc'
import '@shared/utils/echarts' // 共享 ECharts 配置
import EChart from '@/components/EChart'
import MetricChartHeader from '@/components/MetricChartHeader'
import MetricSeriesChartCard from '@/components/MetricSeriesChartCard'
import { Button } from '@/components/ui/button'
import { CardX } from '@/components/ui/card-x'
import { Empty } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { loadNodeLoadRecords, useNodeLoadStats } from '@/hooks/useNodeLoadStats'
import { vclass } from '@/lib/vclass'
import { useApp } from '@/stores/app'
import { useNodes } from '@/stores/nodes'
import { useBox } from '@/hooks/useBox'
import { useStorageHistory } from '@/hooks/useStorageHistory'

const PING_METRIC_KEYS = ['ping.latency_ms', 'ping.loss'] as const
const METRIC_HISTORY_MAX_POINTS = 700
const REALTIME_METRIC_REFRESH_MS = 30_000
const rpc = getSharedRpc()

interface CustomRange { start: dayjs.Dayjs, end: dayjs.Dayjs, hours: number }

function parseRange(startInput: string, endInput: string): CustomRange | null {
  if (!startInput || !endInput)
    return null
  const start = dayjs(startInput)
  const end = dayjs(endInput)
  if (!start.isValid() || !end.isValid() || !end.isAfter(start))
    return null
  return { start, end, hours: Math.max(1, Math.ceil(end.diff(start, 'hour', true))) }
}

export default function LoadChart({ uuid, className, ...attrs }: { uuid: string } & HTMLAttributes<HTMLDivElement>) {
  const app = useApp(a => ({
    publicSettings: a.publicSettings,
    dataUpdateInterval: a.dataUpdateInterval,
    isDark: a.isDark,
    colorVisionFriendly: a.colorVisionFriendly,
    gpuChartEnabled: a.gpuChartEnabled,
    chartDashboardTemplate: a.chartDashboardTemplate,
    diskPredictionEnabled: a.diskPredictionEnabled,
    privateFeaturesAllowed: a.privateFeaturesAllowed,
  }))
  const nodesByUuid = useNodes(n => n.nodesByUuid)
  const nodeInfo = nodesByUuid.get(uuid)
  const currentSnapshot = useBox(snapshot)
  const publicNode = currentSnapshot?.nodes.find(node => String(node.id) === uuid)
  const publicVolumes = captainStorage(uuid, t)
  const storageSelection = JSON.stringify(publicNode?.storage?.map(({ id, name, kind }) => ({ id, name, kind })) ?? null)
  const maxRecordPreserveTime = app.publicSettings?.record_preserve_time || 720
  const detailLoadStatsHours = app.publicSettings?.record_preserve_time || 720
  const chartColors = useMemo(() => getLoadChartPalette(app.colorVisionFriendly), [app.colorVisionFriendly])
  const metricSeriesColors = useMemo(() => getChartSeriesPalette(app.colorVisionFriendly), [app.colorVisionFriendly])

  const REALTIME = t('实时')
  const CUSTOM_VIEW_LABEL = t('自定义')
  const availableViews = useMemo(() => {
    const presetViews = [
      { label: t('4 小时'), hours: 4 },
      { label: t('1 天'), hours: 24 },
      { label: t('7 天'), hours: 168 },
      { label: t('30 天'), hours: 720 },
    ]
    const views: { label: string, hours?: number }[] = [{ label: REALTIME }]
    const maxHours = maxRecordPreserveTime
    for (const v of presetViews) {
      if (maxHours >= v.hours)
        views.push({ label: v.label, hours: v.hours })
    }
    const maxPreset = presetViews.at(-1)
    if (maxPreset && maxHours > maxPreset.hours) {
      views.push({ label: maxHours % 24 === 0 ? `${Math.floor(maxHours / 24)} 天` : `${maxHours} 小时`, hours: maxHours })
    }
    else if (maxHours > 4 && !presetViews.some(v => v.hours === maxHours)) {
      views.push({ label: maxHours % 24 === 0 ? `${Math.floor(maxHours / 24)} 天` : `${maxHours} 小时`, hours: maxHours })
    }
    views.push({ label: CUSTOM_VIEW_LABEL })
    return views
  }, [maxRecordPreserveTime, REALTIME, CUSTOM_VIEW_LABEL])

  const [selectedView, setSelectedView] = useState(REALTIME)
  const [customStartInput, setCustomStartInput] = useState('')
  const [customEndInput, setCustomEndInput] = useState('')
  const selectedHours = availableViews.find(v => v.label === selectedView)?.hours
  const isRealtime = selectedView === REALTIME
  const isCustomRange = selectedView === CUSTOM_VIEW_LABEL
  const customRange = useMemo(() => parseRange(customStartInput, customEndInput), [customStartInput, customEndInput])
  const customRangeError = !isCustomRange || (!customStartInput && !customEndInput)
    ? ''
    : !customStartInput || !customEndInput ? '请选择开始和结束时间' : customRange ? '' : '结束时间必须晚于开始时间'
  const effectiveHistoryHours = isCustomRange ? customRange?.hours ?? 4 : selectedHours ?? 4

  const [remoteData, setRemoteData] = useState<StatusRecord[]>([])
  const [metricData, setMetricData] = useState<RecordFormat[] | null>(null)
  const [rawMetricSeries, setRawMetricSeries] = useState<NormalizedMetricSeries[]>([])
  const [pingTasks, setPingTasks] = useState<PingTaskInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [storageRange, setStorageRange] = useState<StorageHistoryRange | null>({ range: '1h' })
  const [error, setError] = useState<string | null>(null)
  const availableMetricKeys = useRef<Set<string>>(new Set())
  const isInitialLoad = useRef(true)
  const lastRealtimeMetricFetchAt = useRef(0)

  const { diskPrediction, diskPredictionState } = useNodeLoadStats(uuid, {
    hours: detailLoadStatsHours,
    enabled: app.diskPredictionEnabled && app.privateFeaturesAllowed,
    diskTotal: nodeInfo?.disk_total ?? 0,
    online: nodeInfo?.online ?? false,
    permission: 'diskPrediction',
  })
  const diskPredictionSummary = (() => {
    if (!app.diskPredictionEnabled || !app.privateFeaturesAllowed)
      return ''
    if (diskPrediction) {
      const days = Math.max(0, Math.ceil(diskPrediction.daysUntilFull))
      return days <= 0 ? '预计已满' : `预计 ${days} 天后满`
    }
    if (diskPredictionState.reason === 'no_samples')
      return '暂无趋势'
    if (diskPredictionState.reason === 'insufficient_samples')
      return '样本不足'
    if (diskPredictionState.reason === 'insufficient_duration')
      return '趋势积累中'
    return ''
  })()

  const model = useMemo(() => createLoadChartModel({
    uuid,
    isDark: app.isDark,
    remoteData,
    metricData,
    rawMetricSeries,
    pingTasks,
    effectiveHistoryHours,
    nodeInfo,
    chartColors,
    metricSeriesColors,
    colorVisionFriendly: app.colorVisionFriendly,
    gpuChartEnabled: app.gpuChartEnabled,
    chartDashboardTemplate: app.chartDashboardTemplate,
  }), [uuid, app.isDark, remoteData, metricData, rawMetricSeries, pingTasks, effectiveHistoryHours, nodeInfo, chartColors, metricSeriesColors, app.colorVisionFriendly, app.gpuChartEnabled, app.chartDashboardTemplate])

  const storageResult = useStorageHistory(uuid, storageSelection, storageRange, isRealtime, Boolean(publicNode) && shows('history') && model.isChartCardEnabled('disk'))

  // Latest values for async fetchers (Vue read refs at call time).
  const live = useRef({ uuid, isRealtime, isCustomRange, customRange, customRangeError, effectiveHistoryHours, chartDashboardTemplate: app.chartDashboardTemplate, model })
  live.current = { uuid, isRealtime, isCustomRange, customRange, customRangeError, effectiveHistoryHours, chartDashboardTemplate: app.chartDashboardTemplate, model }

  const loadMetricCatalog = useCallback(async () => {
    const [definitions, tasks] = await Promise.all([
      loadMetricDefinitions().catch(() => []),
      loadPublicPingTasks().catch(() => []),
    ])
    availableMetricKeys.current = new Set(definitions.map(definition => definition.name))
    setPingTasks(tasks)
  }, [])

  const refreshRealtimeMetricSeries = useCallback(async (force = false) => {
    const cards = live.current.chartDashboardTemplate.cards
    if (!cards.includes('ping') && !cards.includes('pingLoss')) {
      setRawMetricSeries([])
      return
    }
    const now = Date.now()
    if (!force && now - lastRealtimeMetricFetchAt.current < REALTIME_METRIC_REFRESH_MS)
      return
    lastRealtimeMetricFetchAt.current = now
    const requestedUuid = live.current.uuid
    try {
      if (!availableMetricKeys.current.size)
        await loadMetricCatalog()
      const metricKeys = PING_METRIC_KEYS.filter(key => availableMetricKeys.current.has(key))
      if (!metricKeys.length) {
        setRawMetricSeries([])
        return
      }
      const result = await queryMetrics({ metric_keys: [...metricKeys], entity_id: requestedUuid, hours: 1, downsample: true, fill_empty: true, max_points: 150, aggregation: 'avg' })
      if (!live.current.isRealtime || live.current.uuid !== requestedUuid)
        return
      setRawMetricSeries(normalizeMetricSeriesList(result.series))
    }
    catch {
      if (live.current.isRealtime && live.current.uuid === requestedUuid)
        setRawMetricSeries([])
    }
  }, [loadMetricCatalog])

  const fetchRecentData = useCallback(async () => {
    const id = live.current.uuid
    if (!id)
      return
    setMetricData(null)
    void refreshRealtimeMetricSeries()
    if (isInitialLoad.current)
      setLoading(true)
    setError(null)
    try {
      const result = await rpc.getNodeRecentStatus(id)
      const records = result?.records || []
      records.sort((a, b) => dayjs(a.time).valueOf() - dayjs(b.time).valueOf())
      setRemoteData(records.slice(-150))
    }
    catch (err) {
      setError(err instanceof Error ? err.message : '获取数据失败')
      setRemoteData([])
    }
    finally {
      setLoading(false)
      isInitialLoad.current = false
    }
  }, [refreshRealtimeMetricSeries])

  const fetchHistoryData = useCallback(async () => {
    const l = live.current
    if (!l.uuid)
      return
    if (l.isCustomRange && !l.customRange) {
      setMetricData(null)
      setRawMetricSeries([])
      setRemoteData([])
      setError(l.customRangeError || '请选择有效的自定义时间范围')
      return
    }
    const range = l.customRange
    const hours = l.effectiveHistoryHours
    const metricParams: Pick<MetricQueryParams, 'hours' | 'start' | 'end'> = l.isCustomRange && range
      ? { start: range.start.toDate().toISOString(), end: range.end.toDate().toISOString() }
      : { hours }
    setLoading(true)
    setError(null)
    try {
      const metricHistory = await (async () => {
        const definitions = await loadMetricDefinitions()
        const availableKeys = new Set(definitions.map(definition => definition.name))
        availableMetricKeys.current = availableKeys
        const metricKeys = LOAD_METRIC_KEYS.filter(key => availableKeys.has(key))
        if (!metricKeys.length)
          return null
        const result = await queryMetrics({
          metric_keys: metricKeys,
          entity_id: l.uuid,
          ...metricParams,
          downsample: true,
          fill_empty: true,
          max_points: METRIC_HISTORY_MAX_POINTS,
          aggregation: 'avg',
          aggregation_by_metric: { 'net.total.up': 'last', 'net.total.down': 'last' },
        })
        const series = normalizeMetricSeriesList(result.series)
        if (!series.some(item => item.points.length > 0))
          return null
        return { records: live.current.model.metricSeriesToRecordFormat(result.series), series }
      })().catch(() => null)
      const hasCpuHistory = metricHistory?.records.some(record => record.cpu !== null && Number.isFinite(record.cpu)) ?? false
      if (metricHistory && hasCpuHistory) {
        setMetricData(metricHistory.records)
        setRawMetricSeries(metricHistory.series)
        setRemoteData([])
      }
      else {
        setMetricData(null)
        setRawMetricSeries(metricHistory?.series ?? [])
        setRemoteData(await loadNodeLoadRecords(l.uuid, hours, LOAD_RECORD_MAX_COUNT))
      }
    }
    catch (err) {
      setError(err instanceof Error ? err.message : '获取数据失败')
      setRemoteData([])
      setMetricData(null)
      setRawMetricSeries([])
    }
    finally {
      setLoading(false)
    }
  }, [])

  const fetchData = useCallback(async () => {
    const current = live.current
    setStorageRange(current.isRealtime ? { range: '1h' } : current.isCustomRange
      ? current.customRange ? { from: current.customRange.start.unix(), to: current.customRange.end.unix() } : null
      : { range: current.effectiveHistoryHours <= 4 ? '4h' : current.effectiveHistoryHours <= 24 ? '24h' : current.effectiveHistoryHours <= 168 ? '7d' : '30d' })
    if (live.current.isRealtime)
      await fetchRecentData()
    else
      await fetchHistoryData()
  }, [fetchRecentData, fetchHistoryData])

  // onMounted
  useEffect(() => {
    void loadMetricCatalog()
    void fetchData()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // useIntervalFn(fetchData, dataUpdateInterval) while realtime.
  useEffect(() => {
    if (!isRealtime)
      return
    const id = setInterval(() => void fetchData(), app.dataUpdateInterval * 1000)
    return () => clearInterval(id)
  }, [isRealtime, app.dataUpdateInterval, fetchData])

  const prevView = useRef(selectedView)
  useEffect(() => {
    if (prevView.current === selectedView)
      return
    prevView.current = selectedView
    isInitialLoad.current = true
    if (!isCustomRange || customRange)
      void fetchData()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedView])

  const prevUuid = useRef(uuid)
  useEffect(() => {
    if (prevUuid.current === uuid)
      return
    prevUuid.current = uuid
    setRemoteData([])
    setMetricData(null)
    setRawMetricSeries([])
    lastRealtimeMetricFetchAt.current = 0
    isInitialLoad.current = true
    void fetchData()
  }, [uuid, fetchData])

  const prevCards = useRef(app.chartDashboardTemplate.cards)
  useEffect(() => {
    if (prevCards.current === app.chartDashboardTemplate.cards)
      return
    prevCards.current = app.chartDashboardTemplate.cards
    if (live.current.isRealtime) {
      lastRealtimeMetricFetchAt.current = 0
      void refreshRealtimeMetricSeries(true)
    }
  }, [app.chartDashboardTemplate.cards, refreshRealtimeMetricSeries])

  const { latestStatus, chartData } = model
  // The response owns both labels and data; ordinal public IDs must never join
  // an old response to a newly reordered snapshot's volume names.
  const storageResponse = storageResult?.history
  const historyItems = storageResponse
    ? storageItems({ host: null, storage: storageResponse.volumes.map(volume => ({ ...volume, used: null, total: null })) }, t)
    : publicVolumes
  const storageCharts = model.isChartCardEnabled('disk') ? historyItems.map((item, index) => {
    const points = storageResponse?.volumes[index]?.points ?? (storageResult?.unsupported && publicNode?.storage === undefined && item.kind === 'system'
      ? chartData.map(record => ({ ts: dayjs(record.time).unix(), used: record.disk, total: record.disk_total ?? null })) : [])
    return { item, points, latest: latestStoragePoint(points, storageResponse?.to ?? Date.now() / 1000, storageResponse?.step ?? 60) }
  }) : []
  const hasExtraCharts = (['traffic', 'gpu', 'gpuMemory', 'temperature', 'ping', 'pingLoss'] as const).some(key => model.isChartCardEnabled(key))
  const lowerCount = storageCharts.length + Number(model.isChartCardEnabled('network')) + Number(model.isChartCardEnabled('connections'))
  const lowerColumns = lowerCount <= 2 ? 'md:grid-cols-2' : lowerCount === 3 ? 'md:grid-cols-2 xl:grid-cols-3' : 'md:grid-cols-2 xl:grid-cols-4'

  const cardClass = 'bg-background/50 border-none hover:bg-background transition-all rounded-md'
  const speed = (v: number) => {
    const f = formatBytesSplit(v)
    return `${f.value} ${f.unit}/s `
  }

  return (
    <div {...attrs} className={vclass('flex flex-col gap-4', className)}>
      <div className="flex flex-col items-center gap-2">
        <Tabs value={selectedView} onValueChange={setSelectedView} className="w-full items-center">
          <TabsList className="h-8 bg-background/50 backdrop-blur-xl pointer-events-auto rounded-md" data-load-chart-range="">
            {availableViews.map(view => (
              <TabsTrigger key={view.label} value={view.label} className="h-6.5 text-xs border-none data-[state=active]:text-green-600 shadow-none rounded-sm">
                {view.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        {isCustomRange && (
          <div className="flex w-full flex-col items-center gap-2 sm:flex-row sm:justify-center">
            <div className="grid w-full gap-2 sm:w-auto sm:grid-cols-[minmax(0,13rem)_minmax(0,13rem)_auto]">
              <Input value={customStartInput} onValueChange={v => setCustomStartInput(String(v))} type="datetime-local" aria-label="负载图开始时间" className="h-8 bg-background/50 text-xs" />
              <Input value={customEndInput} onValueChange={v => setCustomEndInput(String(v))} type="datetime-local" aria-label="负载图结束时间" className="h-8 bg-background/50 text-xs" />
              <Button type="button" size="sm" variant="outline" disabled={!customRange} className="h-8 text-xs" onClick={() => void fetchData()}>
                {' 应用 '}
              </Button>
            </div>
            {customRangeError && <div className="text-[11px] text-orange-500">{customRangeError}</div>}
          </div>
        )}
      </div>

      <Spinner show={loading}>
        {error
          ? <div className="text-red-500 py-8 text-center">{error}</div>
          : chartData.length === 0 && storageCharts.length === 0 && !loading
            ? (
                <div className="py-8">
                  <Empty description="暂无负载数据" />
                </div>
              )
            : (
                <div className="space-y-4" data-load-chart-layout>
                <div className="gap-4 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3" data-load-chart-row="host">
                  {model.isChartCardEnabled('cpu') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      data-load-chart-card="cpu"
                      header={(
                        <MetricChartHeader title={t('CPU 与负载')} icon="tabler:cpu" tone="rose">
                          {latestStatus?.cpu != null
                            ? (
                                <div className="text-xs flex gap-0.5 items-baseline">
                                  <span data-latest-cpu="">{latestStatus.cpu.toFixed(1)}</span>
                                  <span>%</span>
                                </div>
                              )
                            : <span>-</span>}
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.cpuChartOption} autoresize /></div>
                    </CardX>
                  )}

                  {model.isChartCardEnabled('memory') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      data-load-chart-card="memory"
                      header={(
                        <MetricChartHeader title={t('内存与 Swap')} icon="tabler:database" tone="violet">
                          <div className="text-xs flex gap-1 items-baseline">
                            {latestStatus?.ram != null
                              ? (
                                  <>
                                    <span>{formatBytesSplit(latestStatus.ram).value}</span>
                                    <span>{formatBytesSplit(latestStatus.ram).unit}</span>
                                  </>
                                )
                              : <span>-</span>}
                            <span>·</span>
                            {nodeInfo?.mem_total
                              ? (
                                  <>
                                    <span>{formatBytesSplit(nodeInfo.mem_total).value}</span>
                                    <span>{formatBytesSplit(nodeInfo.mem_total).unit}</span>
                                  </>
                                )
                              : <span>-</span>}
                          </div>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.memoryChartOption} autoresize /></div>
                    </CardX>
                  )}

                  {model.isChartCardEnabled('process') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      data-load-chart-card="process"
                      header={(
                        <MetricChartHeader title={t('进程')} icon="tabler:activity" tone="slate">
                          <span className="text-xs">
                            {formatMetricCount(latestStatus?.process)}
                          </span>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.processChartOption} autoresize /></div>
                    </CardX>
                  )}

                </div>
                {storageResponse?.truncated === true && <p role="status" className="text-xs text-muted-foreground" data-storage-history-truncated>{t('storage.historyTruncated')}</p>}
                <div className={`gap-4 grid grid-cols-1 ${lowerColumns}`} data-load-chart-row="storage-network">
                  {storageCharts.map(({ item, points, latest }) => (
                    <CardX key={item.id} size="small" className={cardClass} data-load-chart-card="disk" data-storage-chart={item.id}
                      header={(
                        <MetricChartHeader title={item.detailLabel} icon="tabler:device-floppy" tone="emerald" subtitle={item.kind === 'system' ? diskPredictionSummary : ''}>
                          <div className="text-xs flex flex-wrap justify-end gap-x-1 items-baseline" data-storage-chart-capacity title={latest ? dayjs(latest.ts * 1000).format('YYYY-MM-DD HH:mm') : undefined}>
                            <span>{latest?.used != null ? `${formatBytesSplit(latest.used).value} ${formatBytesSplit(latest.used).unit}` : '—'}</span>
                            <span>·</span>
                            <span>{latest?.total != null ? `${formatBytesSplit(latest.total).value} ${formatBytesSplit(latest.total).unit}` : '—'}</span>
                          </div>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="relative h-48">
                        <EChart option={model.storageChartOption(points)} autoresize />
                        {!points.some(point => point.used !== null || point.total !== null) && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-muted-foreground" data-storage-history-empty>{t(storageResult?.error && !storageResult.unsupported ? '加载失败' : '暂无数据')}</div>}
                      </div>
                    </CardX>
                  ))}

                  {model.isChartCardEnabled('network') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      data-load-chart-card="network"
                      header={(
                        <MetricChartHeader title={t('实时网络')} icon="tabler:network" tone="sky">
                          <div className="text-xs flex gap-2 items-baseline">
                            <span className="flex flex-row items-center justify-center gap-0.5">
                              <Icon icon="tabler:chevron-up" width="12" height="12" />
                              {latestStatus?.net_out != null ? speed(latestStatus.net_out) : '-'}
                            </span>
                            <span className="flex flex-row items-center justify-center gap-0.5">
                              <Icon icon="tabler:chevron-down" width="12" height="12" />
                              {latestStatus?.net_in != null ? speed(latestStatus.net_in) : '-'}
                            </span>
                          </div>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.networkChartOption} autoresize /></div>
                    </CardX>
                  )}

                  {model.isChartCardEnabled('connections') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      data-load-chart-card="connections"
                      header={(
                        <MetricChartHeader title={t('网络连接')} icon="tabler:binary-tree" tone="amber">
                          <div className="text-xs flex gap-1 items-baseline">
                            <span>{`TCP: ${formatMetricCount(latestStatus?.connections)}`}</span>
                            <span>·</span>
                            <span>{`UDP: ${formatMetricCount(latestStatus?.connections_udp)}`}</span>
                          </div>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.connectionsChartOption} autoresize /></div>
                    </CardX>
                  )}

                </div>
                {hasExtraCharts && <div className="gap-4 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3" data-load-chart-row="extra">
                  {model.isChartCardEnabled('traffic') && <MetricSeriesChartCard title="累计与周期流量" icon="tabler:arrows-transfer-up-down" tone="sky" series={model.trafficChartSeries} order={model.getChartCardOrder('traffic')} />}

                  {model.isChartCardEnabled('gpu') && (
                    <CardX
                      size="small"
                      className={cardClass}
                      style={model.getChartCardStyle('gpu')}
                      header={(
                        <MetricChartHeader title="GPU 利用率" icon="tabler:device-desktop-analytics" tone="cyan" subtitle={model.getGpuDeviceNames(latestStatus)}>
                          <div className="text-xs flex gap-1 items-baseline shrink-0">
                            {latestStatus?.gpu_usage != null || latestStatus?.gpu != null
                              ? (
                                  <>
                                    <span>{(latestStatus.gpu_usage ?? latestStatus.gpu ?? 0).toFixed(1)}</span>
                                    <span>%</span>
                                  </>
                                )
                              : <span>-</span>}
                          </div>
                        </MetricChartHeader>
                      )}
                    >
                      <div className="h-48"><EChart option={model.gpuChartOption} autoresize /></div>
                    </CardX>
                  )}

                  {model.isChartCardEnabled('gpuMemory') && <MetricSeriesChartCard title="GPU 显存" icon="tabler:stack-2" tone="violet" series={model.gpuMemoryChartSeries} order={model.getChartCardOrder('gpuMemory')} />}
                  {model.isChartCardEnabled('temperature') && <MetricSeriesChartCard title="温度" icon="tabler:temperature" tone="orange" series={model.temperatureChartSeries} order={model.getChartCardOrder('temperature')} />}

                  {model.isChartCardEnabled('ping') && <MetricSeriesChartCard title="Ping 延迟" icon="tabler:radar" tone="cyan" series={model.pingChartSeries} order={model.getChartCardOrder('ping')} />}
                  {model.isChartCardEnabled('pingLoss') && <MetricSeriesChartCard title="Ping 丢包" icon="tabler:cloud-exclamation" tone="rose" series={model.pingLossChartSeries} order={model.getChartCardOrder('pingLoss')} percentScale />}
                </div>}
                </div>
              )}
      </Spinner>
    </div>
  )
}
