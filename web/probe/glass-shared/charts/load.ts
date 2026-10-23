// Pure part of the glass LoadChart, shared by the Vue and React components.
// Bodies are moved verbatim from LoadChart.vue; reactive reads became fields
// of the input `s`, and each former computed is a cached getter on the model
// `m`, so both builds produce identical ECharts options for the same inputs.
import dayjs from 'dayjs'
import { escapeHTML } from '@shared/utils/html'
import { shows } from '@shared/captain'
import { t } from '@shared/i18n'
import type { ChartDashboardTemplate } from '@shared/stores/app-settings'
import type { ChartDashboardCardKey } from '@shared/stores/app-settings'
import type { NodeData } from '@shared/types/node'
import type { NormalizedMetricSeries } from '@shared/utils/metricSeries'
import type { RecordFormat } from '@shared/utils/recordHelper'
import type { MetricSeries, PingTaskInfo, StatusRecord } from '@shared/utils/rpc'
import { gpuUsageFromStatus } from '@shared/utils/gpuHelper'
import { formatBytes } from '@shared/utils/helper'
import { comparePingTaskOrder, createPingTaskOrderMap, metricTags, normalizeMetricSeriesList } from '@shared/utils/metricSeries'
import type { getLoadChartPalette } from '@shared/utils/chartPalette'
import { formatMetricCount, type StorageHistoryPoint } from '../../src/lib/storage-history'

export const LOAD_METRIC_KEYS = [
  'cpu.usage',
  'load.average',
  'memory.used',
  'memory.total',
  'swap.used',
  'swap.total',
  'temperature',
  'disk.used',
  'disk.total',
  'net.in.rate',
  'net.out.rate',
  'net.total.down',
  'net.total.up',
  'traffic.down',
  'traffic.up',
  'process.count',
  'connections.tcp',
  'connections.udp',
  'gpu.usage',
  'gpu.device.usage',
  'gpu.memory.used',
  'gpu.memory.total',
  'gpu.temperature',
  'ping.latency_ms',
  'ping.loss',
] as const

export interface MetricChartSeriesData {
  name: string
  color: string
  kind: 'bytes' | 'bytesPerSecond' | 'count' | 'milliseconds' | 'percent' | 'temperature'
  data: Array<[string, number | null]>
  dashed?: boolean
}

export type LoadMetricKey = typeof LOAD_METRIC_KEYS[number]

export interface LoadChartInputs {
  uuid: string
  isDark: boolean
  remoteData: StatusRecord[]
  metricData: RecordFormat[] | null
  rawMetricSeries: NormalizedMetricSeries[]
  pingTasks: PingTaskInfo[]
  effectiveHistoryHours: number
  nodeInfo: NodeData | undefined
  chartColors: ReturnType<typeof getLoadChartPalette>
  metricSeriesColors: string[]
  colorVisionFriendly: boolean
  gpuChartEnabled: boolean
  chartDashboardTemplate: ChartDashboardTemplate
  hasDiskPrediction?: boolean
}

export function createLoadChartModel(s: LoadChartInputs) {
  const chartColors = s.chartColors
  const metricSeriesColors = s.metricSeriesColors
  const cache = new Map<string, unknown>()
  const memo = <T>(key: string, f: () => T): T => {
    if (!cache.has(key))
      cache.set(key, f())
    return cache.get(key) as T
  }
  // eslint-disable-next-line ts/no-use-before-define
  const m = {
    get chartThemeColors() { return memo('chartThemeColors', () => ({
        text: s.isDark ? 'rgba(255, 255, 255, 0.85)' : 'rgba(0, 0, 0, 0.85)',
        textSecondary: s.isDark ? 'rgba(255, 255, 255, 0.55)' : 'rgba(0, 0, 0, 0.55)',
        textTertiary: s.isDark ? 'rgba(255, 255, 255, 0.35)' : 'rgba(0, 0, 0, 0.35)',
        borderColor: s.isDark ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.1)',
        splitLineColor: s.isDark ? 'rgba(255, 255, 255, 0.06)' : 'rgba(0, 0, 0, 0.06)',
        tooltipBg: s.isDark ? 'rgba(40, 40, 40, 0.95)' : 'rgba(255, 255, 255, 0.8)',
        tooltipShadow: s.isDark ? 'rgba(0, 0, 0, 0.4)' : 'rgba(0, 0, 0, 0.06)',
        crosshairColor: s.isDark ? 'rgba(255, 255, 255, 0.15)' : 'rgba(0, 0, 0, 0.1)',
      })) },
    get baseTooltipConfig() { return memo('baseTooltipConfig', () => ({
        trigger: 'axis' as const,
        confine: false,
        backgroundColor: m.chartThemeColors.tooltipBg,
        borderColor: 'transparent',
        borderWidth: 0,
        borderRadius: 6,
        textStyle: {
          color: m.chartThemeColors.text,
          fontSize: 12,
          lineHeight: 20,
        },
        extraCssText: `backdrop-filter: blur(5px);z-index:9;box-shadow:0 0 0 1px ${m.chartThemeColors.tooltipShadow}, 0 0 16px ${m.chartThemeColors.tooltipShadow}`,
        axisPointer: {
          type: 'cross' as const,
          crossStyle: {
            color: m.chartThemeColors.textTertiary,
          },
          lineStyle: {
            color: m.chartThemeColors.crosshairColor,
            width: 1,
            type: 'dashed' as const,
          },
          shadowStyle: {
            color: m.chartThemeColors.crosshairColor,
          },
        },
      })) },
    get chartData() { return memo('chartData', () => s.metricData ?? statusToRecordFormat(s.remoteData)) },
    get latestStatus() { return memo('latestStatus', () => (() => {
        const data = m.chartData
        if (!data.length)
          return null
        return data.at(-1) ?? null
      })()) },
    get hasGpuData() { return memo('hasGpuData', () => m.chartData.some(record => record.gpu != null || record.gpu_usage != null || record.gpu_memory != null || record.gpu_detailed)) },
    get pingTaskMap() { return memo('pingTaskMap', () => new Map(s.pingTasks.map(task => [String(task.id), task]))) },
    get pingTaskOrder() { return memo('pingTaskOrder', () => createPingTaskOrderMap(s.pingTasks)) },
    get trafficChartSeries(): MetricChartSeriesData[] { return memo('trafficChartSeries', () => [
        recordMetricSeries('累计下载', chartColors.quinary, 'bytes', record => record.net_total_down),
        recordMetricSeries('累计上传', chartColors.quaternary, 'bytes', record => record.net_total_up),
        recordMetricSeries('周期下载', chartColors.tertiary, 'bytes', record => record.traffic_down, true),
        recordMetricSeries('周期上传', chartColors.secondary, 'bytes', record => record.traffic_up, true),
      ].filter(seriesHasData)) },
    get gpuMemoryChartSeries(): MetricChartSeriesData[] { return memo('gpuMemoryChartSeries', () => gpuDeviceEntries().flatMap((device, index) => {
        const used = recordMetricSeries(
          `${device.name} 已用`,
          metricSeriesColors[index * 2 % metricSeriesColors.length]!,
          'bytes',
          record => record.gpu_detailed?.[device.index]?.mem_used,
        )
        const total = recordMetricSeries(
          `${device.name} 总量`,
          metricSeriesColors[(index * 2 + 1) % metricSeriesColors.length]!,
          'bytes',
          record => record.gpu_detailed?.[device.index]?.mem_total,
          true,
        )
        return [used, total].filter(seriesHasData)
      })) },
    get temperatureChartSeries(): MetricChartSeriesData[] { return memo('temperatureChartSeries', () => (() => {
        const series = [recordMetricSeries('系统温度', chartColors.secondary, 'temperature', record => record.temp)]
        if (s.gpuChartEnabled) {
          series.push(...gpuDeviceEntries().map((device, index) => recordMetricSeries(
            `${device.name} 温度`,
            metricSeriesColors[index % metricSeriesColors.length]!,
            'temperature',
            record => record.gpu_detailed?.[device.index]?.temperature,
          )))
        }
        return series.filter(seriesHasData)
      })()) },
    get pingChartSeries(): MetricChartSeriesData[] { return memo('pingChartSeries', () => pingSeries('ping.latency_ms')) },
    get pingLossChartSeries(): MetricChartSeriesData[] { return memo('pingLossChartSeries', () => pingSeries('ping.loss')) },
    get hasTrafficData() { return memo('hasTrafficData', () => m.trafficChartSeries.length > 0) },
    get hasGpuMemoryData() { return memo('hasGpuMemoryData', () => m.gpuMemoryChartSeries.length > 0) },
    get hasTemperatureData() { return memo('hasTemperatureData', () => m.temperatureChartSeries.length > 0) },
    get hasPingData() { return memo('hasPingData', () => m.pingChartSeries.length > 0) },
    get hasPingLossData() { return memo('hasPingLossData', () => m.pingLossChartSeries.length > 0) },
    get showDateInAxis() { return memo('showDateInAxis', () => (s.effectiveHistoryHours) >= 24) },
    get baseXAxisConfig() { return memo('baseXAxisConfig', () => ({
        type: 'category' as const,
        data: m.chartData.map(r => formatTime(r.time, m.showDateInAxis)),
        axisLabel: {
          fontSize: 11,
          color: m.chartThemeColors.textSecondary,
          margin: 12,
        },
        axisLine: {
          show: true,
          lineStyle: { color: m.chartThemeColors.borderColor, width: 1 },
        },
        axisTick: { show: false },
        boundaryGap: false,
      })) },
    get baseYAxisConfig() { return memo('baseYAxisConfig', () => ({
        type: 'value' as const,
        axisLabel: {
          fontSize: 11,
          color: m.chartThemeColors.textSecondary,
        },
        axisLine: { show: false },
        axisTick: { show: false },
        splitLine: {
          lineStyle: {
            color: m.chartThemeColors.splitLineColor,
            type: 'dashed' as const,
          },
        },
      })) },
    get cpuChartOption() { return memo('cpuChartOption', () => ({
        animation: false,
        // 全局颜色配置（确保 Tooltip 圆点颜色与线条一致）
        color: [chartColors.primary, chartColors.secondary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, seriesName: string, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
      
            for (const item of p) {
              const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${item.color};margin-right:8px;flex-shrink:0"></span>`
              if (item.seriesName === 'CPU') {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>CPU</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${item.value?.toFixed(1) ?? '-'}%</span></div>`
              }
              else if (item.seriesName === t('负载')) {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>系统负载</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${item.value?.toFixed(2) ?? '-'}</span></div>`
              }
            }
            html += '</div>'
            return html
          },
        },
        grid: chartMargin,
        xAxis: m.baseXAxisConfig,
        yAxis: [
          {
            ...m.baseYAxisConfig,
            name: 'CPU %',
            nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
            min: 0,
            max: 100,
            axisLabel: { ...m.baseYAxisConfig.axisLabel, formatter: '{value}%' },
          },
          {
            ...m.baseYAxisConfig,
            name: t('负载'),
            nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 0, 0, 40] },
            min: 0,
            splitLine: { show: false },
          },
        ],
        series: [
          {
            name: 'CPU',
            type: 'line',
            data: m.chartData.map(r => r.cpu),
      
            showSymbol: false,
            yAxisIndex: 0,
            lineStyle: { width: 1.5, color: chartColors.primary, cap: 'round' as const },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: chartColors.primaryAreaStrong },
                  { offset: 1, color: chartColors.primaryAreaFaint },
                ],
              },
            },
          },
          {
            name: t('负载'),
            type: 'line',
            data: m.chartData.map(r => r.load),
      
            showSymbol: false,
            yAxisIndex: 1,
            lineStyle: { width: 1.5, color: chartColors.secondary, cap: 'round' as const },
          },
        ],
      })) },
    get memoryChartOption() { return memo('memoryChartOption', () => ({
        animation: false,
        color: [chartColors.primary, chartColors.quinary, chartColors.secondary, chartColors.quaternary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, seriesName: string, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const ramUsed = record.ram ?? 0
            const ramTotal = record.ram_total ?? s.nodeInfo?.mem_total ?? 0
            const swapUsed = record.swap ?? 0
            const swapTotal = record.swap_total ?? s.nodeInfo?.swap_total ?? 0
            const ramPercent = ramTotal > 0 ? ((ramUsed / ramTotal) * 100).toFixed(1) : '0'
            const swapPercent = swapTotal > 0 ? ((swapUsed / swapTotal) * 100).toFixed(1) : '0'
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
      
            for (const item of p) {
              const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${item.color};margin-right:8px;flex-shrink:0"></span>`
              if (item.seriesName === 'RAM') {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>RAM</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${formatBytes(ramUsed)} (${ramPercent}%)</span></div>`
              }
              else if (item.seriesName === 'Swap') {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>Swap</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${formatBytes(swapUsed)} (${swapPercent}%)</span></div>`
              }
              else if (item.seriesName === 'RAM 总量') {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>RAM 总量</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${formatBytes(ramTotal)}</span></div>`
              }
              else if (item.seriesName === 'Swap 总量') {
                html += `<div style="display:flex;align-items:center">${colorDot}<span>Swap 总量</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${formatBytes(swapTotal)}</span></div>`
              }
            }
            html += '</div>'
            return html
          },
        },
        legend: {
          data: ['RAM', 'RAM 总量', 'Swap', 'Swap 总量'],
          bottom: 4,
          itemWidth: 10,
          itemHeight: 8,
          textStyle: { fontSize: 10, color: m.chartThemeColors.textSecondary },
        },
        grid: chartMarginWithLegend,
        xAxis: m.baseXAxisConfig,
        yAxis: {
          ...m.baseYAxisConfig,
          name: t('内存'),
          nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
          axisLabel: {
            ...m.baseYAxisConfig.axisLabel,
            formatter: (val: number) => formatBytes(val),
          },
        },
        series: [
          {
            name: 'RAM',
            type: 'line',
            data: m.chartData.map(r => r.ram),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.primary, cap: 'round' as const },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: chartColors.primaryAreaStrong },
                  { offset: 1, color: chartColors.primaryAreaFaint },
                ],
              },
            },
          },
          {
            name: 'RAM 总量',
            type: 'line',
            data: m.chartData.map(r => r.ram_total ?? s.nodeInfo?.mem_total ?? null),
            showSymbol: false,
            lineStyle: { width: 1.2, type: 'dashed' as const, color: chartColors.quinary, cap: 'round' as const },
          },
          {
            name: 'Swap',
            type: 'line',
            data: m.chartData.map(r => r.swap),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.secondary, cap: 'round' as const },
          },
          {
            name: 'Swap 总量',
            type: 'line',
            data: m.chartData.map(r => r.swap_total ?? s.nodeInfo?.swap_total ?? null),
            showSymbol: false,
            lineStyle: { width: 1.2, type: 'dashed' as const, color: chartColors.quaternary, cap: 'round' as const },
          },
        ],
      })) },
    get diskChartOption() { return memo('diskChartOption', () => storageChartOption(m.chartData.map(record => ({
      ts: dayjs(record.time).unix(), used: record.disk, total: record.disk_total ?? s.nodeInfo?.disk_total ?? null,
    })))) },
    get networkChartOption() { return memo('networkChartOption', () => ({
        animation: false,
        color: [chartColors.quinary, chartColors.quaternary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, seriesName: string, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
      
            for (const item of p) {
              const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${item.color};margin-right:8px;flex-shrink:0"></span>`
              const label = item.seriesName === '下载' ? '↓ 下载' : '↑ 上传'
              html += `<div style="display:flex;align-items:center">${colorDot}<span>${label}</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${formatBytes(item.value)}/s</span></div>`
            }
            html += '</div>'
            return html
          },
        },
        legend: {
          data: ['下载', '上传'],
          bottom: 4,
          itemWidth: 12,
          itemHeight: 12,
          itemGap: 20,
          icon: 'roundRect',
          textStyle: { fontSize: 11, color: m.chartThemeColors.textSecondary },
        },
        grid: chartMarginWithLegend,
        xAxis: m.baseXAxisConfig,
        yAxis: {
          ...m.baseYAxisConfig,
          name: '速度',
          nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
          axisLabel: {
            ...m.baseYAxisConfig.axisLabel,
            formatter: (val: number) => formatBytes(val),
          },
        },
        series: [
          {
            name: '下载',
            type: 'line',
            data: m.chartData.map(r => r.net_in ?? 0),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.quinary, cap: 'round' as const },
          },
          {
            name: '上传',
            type: 'line',
            data: m.chartData.map(r => r.net_out ?? 0),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.quaternary, cap: 'round' as const },
          },
        ],
      })) },
    get gpuDeviceUsageEChartSeries() { return memo('gpuDeviceUsageEChartSeries', () => gpuDeviceEntries().map((device, index) => ({
        name: device.name,
        type: 'line',
        data: m.chartData.map(record => record.gpu_detailed?.[device.index]?.usage ?? null),
        showSymbol: false,
        lineStyle: {
          width: 1.2,
          type: 'dashed' as const,
          color: metricSeriesColors[index % metricSeriesColors.length]!,
          cap: 'round' as const,
        },
      })).filter(series => series.data.some(value => value !== null))) },
    get gpuChartOption() { return memo('gpuChartOption', () => ({
        animation: false,
        color: [chartColors.senary, chartColors.quaternary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, seriesName: string, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
      
            for (const item of p) {
              const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${item.color};margin-right:8px;flex-shrink:0"></span>`
              html += `<div style="display:flex;align-items:center">${colorDot}<span>${escapeHTML(item.seriesName)}</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${item.value?.toFixed(1) ?? '-'}%</span></div>`
            }
      
            if (record.gpu_detailed) {
              html += `<div style="margin-top:4px;padding-top:4px;border-top:1px solid ${m.chartThemeColors.splitLineColor}">`
              for (const detail of Object.values(record.gpu_detailed)) {
                const name = detail.device_name || (detail.device_index === undefined ? 'GPU' : `GPU ${detail.device_index}`)
                const usage = detail.usage == null ? '-' : `${detail.usage.toFixed(1)}%`
                const memory = detail.memory == null ? '-' : `${detail.memory.toFixed(1)}%`
                const temp = detail.temperature == null ? '' : ` · ${Math.round(detail.temperature)}℃`
                html += `<div style="display:flex;align-items:center;gap:8px;color:${m.chartThemeColors.textSecondary}"><span>${escapeHTML(name)}</span><span style="margin-left:auto">${usage} / ${memory}${temp}</span></div>`
              }
              html += '</div>'
            }
      
            html += '</div>'
            return html
          },
        },
        legend: {
          data: ['GPU 使用率', '显存使用率', ...m.gpuDeviceUsageEChartSeries.map(series => series.name)],
          bottom: 4,
          itemWidth: 12,
          itemHeight: 12,
          itemGap: 20,
          icon: 'roundRect',
          textStyle: { fontSize: 11, color: m.chartThemeColors.textSecondary },
        },
        grid: chartMarginWithLegend,
        xAxis: m.baseXAxisConfig,
        yAxis: {
          ...m.baseYAxisConfig,
          name: 'GPU %',
          nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
          min: 0,
          max: 100,
          axisLabel: { ...m.baseYAxisConfig.axisLabel, formatter: '{value}%' },
        },
        series: [
          {
            name: 'GPU 使用率',
            type: 'line',
            data: m.chartData.map(r => r.gpu_usage ?? r.gpu),
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.senary, cap: 'round' as const },
          },
          {
            name: '显存使用率',
            type: 'line',
            data: m.chartData.map(r => r.gpu_memory),
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.quaternary, cap: 'round' as const },
          },
          ...m.gpuDeviceUsageEChartSeries,
        ],
      })) },
    get chartDashboardCards() { return memo('chartDashboardCards', () => s.chartDashboardTemplate.cards) },
    get connectionsChartOption() { return memo('connectionsChartOption', () => ({
        animation: false,
        color: [chartColors.primary, chartColors.tertiary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, seriesName: string, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
      
            for (const item of p) {
              const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${item.color};margin-right:8px;flex-shrink:0"></span>`
              const displayValue = formatMetricCount(item.value)
              html += `<div style="display:flex;align-items:center">${colorDot}<span>${escapeHTML(item.seriesName)}</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${displayValue}</span></div>`
            }
            html += '</div>'
            return html
          },
        },
        legend: {
          data: ['TCP', 'UDP'],
          bottom: 4,
          itemWidth: 12,
          itemHeight: 12,
          itemGap: 20,
          icon: 'roundRect',
          textStyle: { fontSize: 11, color: m.chartThemeColors.textSecondary },
        },
        grid: chartMarginWithLegend,
        xAxis: m.baseXAxisConfig,
        yAxis: {
          ...m.baseYAxisConfig,
          name: t('连接数'),
          nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
          min: 0,
          axisLabel: {
            ...m.baseYAxisConfig.axisLabel,
            formatter: (val: number) => Math.round(val).toString(),
          },
        },
        series: [
          {
            name: 'TCP',
            type: 'line',
            data: m.chartData.map(r => r.connections ?? null),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.primary, cap: 'round' as const },
          },
          {
            name: 'UDP',
            type: 'line',
            data: m.chartData.map(r => r.connections_udp ?? null),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.tertiary, cap: 'round' as const },
          },
        ],
      })) },
    get processChartOption() { return memo('processChartOption', () => ({
        animation: false,
        color: [chartColors.quaternary],
        tooltip: {
          ...m.baseTooltipConfig,
          formatter: (params: unknown) => {
            const p = params as Array<{ dataIndex: number, value: number, color: string }>
            if (!p.length)
              return ''
            const firstParam = p[0]
            if (!firstParam)
              return ''
            const record = m.chartData[firstParam.dataIndex]
            if (!record)
              return ''
      
            const timeStr = formatTimeForTooltip(record.time, s.effectiveHistoryHours)
            const colorDot = `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${firstParam.color};margin-right:8px;flex-shrink:0"></span>`
            const displayValue = formatMetricCount(firstParam.value)
      
            let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${timeStr}</div>`
            html += '<div style="display:flex;flex-direction:column;gap:4px">'
            html += `<div style="display:flex;align-items:center">${colorDot}<span>进程数</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${displayValue}</span></div>`
            html += '</div>'
            return html
          },
        },
        grid: chartMargin,
        xAxis: m.baseXAxisConfig,
        yAxis: {
          ...m.baseYAxisConfig,
          name: t('进程'),
          nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
          min: 0,
          axisLabel: {
            ...m.baseYAxisConfig.axisLabel,
            formatter: (val: number) => Math.round(val).toString(),
          },
        },
        series: [
          {
            name: '进程数',
            type: 'line',
            data: m.chartData.map(r => r.process ?? null),
      
            showSymbol: false,
            lineStyle: { width: 1.5, color: chartColors.quaternary, cap: 'round' as const },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: 'rgba(167, 139, 250, 0.25)' },
                  { offset: 1, color: 'rgba(167, 139, 250, 0.02)' },
                ],
              },
            },
          },
        ],
      })) },
  }

  const chartMargin = { top: 30, right: 24, bottom: 32, left: 56 }
  const chartMarginWithLegend = { top: 30, right: 24, bottom: 52, left: 56 }
  
  // 视图选项
  function gpuDetailsFromStatus(record: StatusRecord): RecordFormat['gpu_detailed'] {
    if (!record.gpu_detailed_info?.length)
      return undefined
  
    const details: NonNullable<RecordFormat['gpu_detailed']> = {}
    record.gpu_detailed_info.forEach((item, index) => {
      const deviceIndex = item.device_index ?? index
      const memUsed = metricValue(item.memory_used)
      const memTotal = metricValue(item.memory_total)
      details[deviceIndex] = {
        usage: metricValue(item.utilization ?? item.usage),
        memory: memUsed != null && memTotal && memTotal > 0 ? memUsed / memTotal * 100 : null,
        temperature: metricValue(item.temperature),
        device_index: deviceIndex,
        device_name: item.device_name || item.name,
        mem_total: memTotal ?? undefined,
        mem_used: memUsed ?? undefined,
      }
    })
    return details
  }
  function averageGpuMemoryPercent(details: NonNullable<RecordFormat['gpu_detailed']>): number | null {
    const percents = Object.values(details)
      .map(detail => detail.memory)
      .filter((value): value is number => typeof value === 'number' && Number.isFinite(value))
    if (!percents.length)
      return null
    return percents.reduce((sum, value) => sum + value, 0) / percents.length
  }
  function statusToRecordFormat(records: StatusRecord[]): RecordFormat[] {
    return records.map((r) => {
      const gpuDetailed = gpuDetailsFromStatus(r)
      const gpu = metricValue(gpuUsageFromStatus(r))
      return {
        client: r.client,
        time: r.time,
        cpu: metricValue(r.cpu),
        gpu,
        gpu_usage: gpu,
        gpu_memory: gpuDetailed
          ? averageGpuMemoryPercent(gpuDetailed)
          : null,
        gpu_detailed: gpuDetailed,
        ram: metricValue(r.ram),
        ram_total: metricValue(r.ram_total),
        swap: metricValue(r.swap),
        swap_total: metricValue(r.swap_total),
        load: metricValue(r.load),
        temp: metricValue(r.temp),
        disk: metricValue(r.disk),
        disk_total: metricValue(r.disk_total),
        net_in: metricValue(r.net_in),
        net_out: metricValue(r.net_out),
        net_total_up: metricValue(r.net_total_up),
        net_total_down: metricValue(r.net_total_down),
        traffic_up: metricValue(r.traffic_up),
        traffic_down: metricValue(r.traffic_down),
        process: metricValue(r.process),
        connections: metricValue(r.connections),
        connections_udp: metricValue(r.connections_udp),
      }
    })
  }
  function metricValue(value: unknown): number | null {
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  }
  function getMetricDeviceKey(series: MetricSeries): string {
    const tags = metricTags(series)
    const index = tags.device_index ?? tags.gpu_index ?? tags.index
    const name = tags.device_name ?? tags.gpu_name ?? tags.name
    return String(index ?? name ?? '0')
  }
  function getMetricDeviceIndex(series: MetricSeries): number {
    const tags = metricTags(series)
    const rawIndex = tags.device_index ?? tags.gpu_index ?? tags.index
    const numericIndex = Number(rawIndex)
    return Number.isFinite(numericIndex) ? numericIndex : Math.abs(getMetricDeviceKey(series).split('').reduce((sum, char) => sum + char.charCodeAt(0), 0))
  }
  function getMetricDeviceName(series: MetricSeries): string | undefined {
    const tags = metricTags(series)
    const name = tags.device_name ?? tags.gpu_name ?? tags.name
    return typeof name === 'string' && name.trim() ? name.trim() : undefined
  }
  function ensureMetricRow(rows: Map<string, RecordFormat>, time: string): RecordFormat {
    const existing = rows.get(time)
    if (existing)
      return existing
  
    const node = s.nodeInfo
    const row: RecordFormat = {
      client: s.uuid,
      time,
      cpu: null,
      gpu: null,
      gpu_usage: null,
      gpu_memory: null,
      ram: null,
      ram_total: node?.mem_total ?? null,
      swap: null,
      swap_total: node?.swap_total ?? null,
      load: null,
      temp: null,
      disk: null,
      disk_total: node?.disk_total ?? null,
      net_in: null,
      net_out: null,
      net_total_up: null,
      net_total_down: null,
      traffic_up: null,
      traffic_down: null,
      process: null,
      connections: null,
      connections_udp: null,
    }
    rows.set(time, row)
    return row
  }
  function applyMetricPoint(row: RecordFormat, key: LoadMetricKey, value: number | null, series: MetricSeries): void {
    switch (key) {
      case 'cpu.usage':
        row.cpu = value
        break
      case 'load.average':
        row.load = value
        break
      case 'memory.used':
        row.ram = value
        break
      case 'memory.total':
        row.ram_total = value
        break
      case 'swap.used':
        row.swap = value
        break
      case 'swap.total':
        row.swap_total = value
        break
      case 'temperature':
        row.temp = value
        break
      case 'disk.used':
        row.disk = value
        break
      case 'disk.total':
        row.disk_total = value
        break
      case 'net.in.rate':
        row.net_in = value
        break
      case 'net.out.rate':
        row.net_out = value
        break
      case 'net.total.down':
        row.net_total_down = value
        break
      case 'net.total.up':
        row.net_total_up = value
        break
      case 'traffic.down':
        row.traffic_down = value
        break
      case 'traffic.up':
        row.traffic_up = value
        break
      case 'process.count':
        row.process = value
        break
      case 'connections.tcp':
        row.connections = value
        break
      case 'connections.udp':
        row.connections_udp = value
        break
      case 'gpu.usage':
        row.gpu = value
        row.gpu_usage = value
        break
      case 'gpu.device.usage': {
        const deviceIndex = getMetricDeviceIndex(series)
        row.gpu_detailed ??= {}
        row.gpu_detailed[deviceIndex] ??= { usage: null, memory: null, temperature: null, device_index: deviceIndex, device_name: getMetricDeviceName(series) }
        row.gpu_detailed[deviceIndex].usage = value
        row.gpu_usage = row.gpu_usage ?? value
        row.gpu = row.gpu ?? value
        break
      }
      case 'gpu.memory.used': {
        const deviceIndex = getMetricDeviceIndex(series)
        row.gpu_detailed ??= {}
        row.gpu_detailed[deviceIndex] ??= { usage: null, memory: null, temperature: null, device_index: deviceIndex, device_name: getMetricDeviceName(series) }
        row.gpu_detailed[deviceIndex].mem_used = value ?? undefined
        break
      }
      case 'gpu.memory.total': {
        const deviceIndex = getMetricDeviceIndex(series)
        row.gpu_detailed ??= {}
        row.gpu_detailed[deviceIndex] ??= { usage: null, memory: null, temperature: null, device_index: deviceIndex, device_name: getMetricDeviceName(series) }
        row.gpu_detailed[deviceIndex].mem_total = value ?? undefined
        break
      }
      case 'gpu.temperature': {
        const deviceIndex = getMetricDeviceIndex(series)
        row.gpu_detailed ??= {}
        row.gpu_detailed[deviceIndex] ??= { usage: null, memory: null, temperature: null, device_index: deviceIndex, device_name: getMetricDeviceName(series) }
        row.gpu_detailed[deviceIndex].temperature = value
        break
      }
    }
  }
  function finalizeGpuRows(rows: RecordFormat[]): RecordFormat[] {
    for (const row of rows) {
      if (!row.gpu_detailed)
        continue
  
      const usages: number[] = []
      const memories: number[] = []
      for (const detail of Object.values(row.gpu_detailed)) {
        if (detail.mem_used != null && detail.mem_total && detail.mem_total > 0)
          detail.memory = detail.mem_used / detail.mem_total * 100
        if (typeof detail.usage === 'number' && Number.isFinite(detail.usage))
          usages.push(detail.usage)
        if (typeof detail.memory === 'number' && Number.isFinite(detail.memory))
          memories.push(detail.memory)
      }
  
      if (row.gpu_usage == null && usages.length)
        row.gpu_usage = usages.reduce((sum, value) => sum + value, 0) / usages.length
      row.gpu ??= row.gpu_usage
      if (row.gpu_memory == null && memories.length)
        row.gpu_memory = memories.reduce((sum, value) => sum + value, 0) / memories.length
    }
    return rows
  }
  function getGpuDeviceNames(record: RecordFormat | null): string {
    if (!record?.gpu_detailed)
      return s.nodeInfo?.gpu_name || ''
    return Object.values(record.gpu_detailed)
      .map(detail => detail.device_name || (detail.device_index === undefined ? '' : `GPU ${detail.device_index}`))
      .filter(Boolean)
      .join(' / ')
  }
  function metricSeriesToRecordFormat(seriesList: MetricSeries[]): RecordFormat[] {
    const rows = new Map<string, RecordFormat>()
    const normalizedSeriesList = normalizeMetricSeriesList(seriesList)
  
    for (const series of normalizedSeriesList) {
      if (!LOAD_METRIC_KEYS.includes(series.metric_key as LoadMetricKey))
        continue
  
      const key = series.metric_key as LoadMetricKey
      if (key === 'ping.latency_ms' || key === 'ping.loss')
        continue
  
      for (const point of series.points) {
        const row = ensureMetricRow(rows, point.time)
        applyMetricPoint(row, key, metricValue(point.value), series)
      }
    }
  
    return finalizeGpuRows([...rows.values()].sort((a, b) => dayjs(a.time).valueOf() - dayjs(b.time).valueOf()))
  }
  function seriesHasData(series: MetricChartSeriesData): boolean {
    return series.data.some(([, value]) => value !== null && Number.isFinite(value))
  }
  function recordMetricSeries(
    name: string,
    color: string,
    kind: MetricChartSeriesData['kind'],
    getter: (record: RecordFormat) => number | null | undefined,
    dashed = false,
  ): MetricChartSeriesData {
    return {
      name,
      color,
      kind,
      dashed,
      data: m.chartData.map(record => [record.time, metricValue(getter(record))]),
    }
  }
  function gpuDeviceEntries(): Array<{ index: number, name: string }> {
    const devices = new Map<number, string>()
    for (const record of m.chartData) {
      for (const [rawIndex, detail] of Object.entries(record.gpu_detailed ?? {})) {
        const index = detail.device_index ?? Number(rawIndex)
        devices.set(index, detail.device_name || `GPU ${index + 1}`)
      }
    }
    return [...devices.entries()]
      .sort(([left], [right]) => left - right)
      .map(([index, name]) => ({ index, name }))
  }
  function pingSeries(metricKey: 'ping.latency_ms' | 'ping.loss'): MetricChartSeriesData[] {
    return s.rawMetricSeries
      .filter(series => series.metric_key === metricKey)
      .sort((left, right) => comparePingTaskOrder(metricTags(left), metricTags(right), m.pingTaskOrder))
      .map<MetricChartSeriesData>((series, index) => {
        const tags = metricTags(series)
        const taskId = String(tags.task_id ?? tags.task ?? '')
        const taskName = m.pingTaskMap.get(taskId)?.name || (taskId ? `任务 ${taskId}` : `Ping ${index + 1}`)
        return {
          name: taskName,
          color: metricSeriesColors[index % metricSeriesColors.length]!,
          kind: metricKey === 'ping.loss' ? 'percent' : 'milliseconds',
          dashed: s.colorVisionFriendly && index % 2 === 1,
          data: series.points.map(point => [
            point.time,
            point.value === null || !Number.isFinite(point.value)
              ? null
              : metricKey === 'ping.loss' ? point.value * 100 : point.value,
          ] as [string, number | null]),
        }
      })
      .filter(seriesHasData)
  }
  function storageChartOption(points: StorageHistoryPoint[]) {
    const usedLabel = t('磁盘已用'), totalLabel = t('磁盘总量')
    const capacity = (value: number | null) => value === null ? '—' : formatBytes(value)
    return {
      animation: false,
      color: [chartColors.tertiary, chartColors.quinary],
      tooltip: {
        ...m.baseTooltipConfig,
        formatter: (params: unknown) => {
          const rows = params as Array<{ dataIndex: number; seriesName: string; color: string }>
          const point = rows[0] && points[rows[0].dataIndex]
          if (!point) return ''
          const when = formatTimeForTooltip(new Date(point.ts * 1000).toISOString(), s.effectiveHistoryHours)
          const percentage = point.used !== null && point.total !== null && point.total > 0 ? ` (${(point.used / point.total * 100).toFixed(1)}%)` : ''
          let html = `<div style="font-weight:600;margin-bottom:6px;color:${m.chartThemeColors.textSecondary}">${when}</div><div style="display:flex;flex-direction:column;gap:4px">`
          for (const row of rows) {
            const value = row.seriesName === totalLabel ? capacity(point.total) : `${capacity(point.used)}${percentage}`
            html += `<div style="display:flex;align-items:center"><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${row.color};margin-right:8px;flex-shrink:0"></span><span>${escapeHTML(row.seriesName)}</span><span style="margin-left:auto;font-weight:600;margin-left:16px">${value}</span></div>`
          }
          return `${html}</div>`
        },
      },
      legend: {
        data: [usedLabel, totalLabel], bottom: 4, itemWidth: 10, itemHeight: 8,
        textStyle: { fontSize: 10, color: m.chartThemeColors.textSecondary },
      },
      grid: chartMarginWithLegend,
      xAxis: { ...m.baseXAxisConfig, data: points.map(point => formatTime(new Date(point.ts * 1000).toISOString(), m.showDateInAxis)) },
      yAxis: {
        ...m.baseYAxisConfig, name: t('磁盘'),
        nameTextStyle: { color: m.chartThemeColors.textSecondary, padding: [0, 40, 0, 0] },
        axisLabel: { ...m.baseYAxisConfig.axisLabel, formatter: (value: number) => formatBytes(value) },
      },
      series: [
        {
          name: usedLabel, type: 'line', data: points.map(point => point.used), showSymbol: false, connectNulls: false,
          lineStyle: { width: 1.5, color: chartColors.tertiary, cap: 'round' as const },
          areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [
            { offset: 0, color: chartColors.tertiaryAreaStrong }, { offset: 1, color: chartColors.tertiaryAreaFaint },
          ] } },
        },
        {
          name: totalLabel, type: 'line', data: points.map(point => point.total), showSymbol: false, connectNulls: false,
          lineStyle: { width: 1.2, type: 'dashed' as const, color: chartColors.quinary, cap: 'round' as const },
        },
      ],
    }
  }
  function formatTime(time: string, showDate: boolean): string {
    const date = dayjs(time)
    if (showDate) {
      return date.format('M/D HH:mm')
    }
    return date.format('HH:mm')
  }
  function formatTimeForTooltip(time: string, hours: number): string {
    const date = dayjs(time)
    if (hours < 24) {
      return date.format('HH:mm:ss')
    }
    return date.format('MM/DD HH:mm')
  }
  function isChartCardEnabled(key: ChartDashboardCardKey): boolean {
    if (!shows(({cpu:'cpu',memory:'memory',disk:'disk',network:'network',traffic:'network',connections:'system',processes:'system',ping:'latency',pingLoss:'latency',gpu:'gpu',gpuMemory:'gpu',temperature:'system'} as Record<string,string>)[key] ?? 'system')) return false
    if (!m.chartDashboardCards.includes(key))
      return false
  
    switch (key) {
      case 'gpu':
        return s.gpuChartEnabled && m.hasGpuData
      case 'gpuMemory':
        return s.gpuChartEnabled && m.hasGpuMemoryData
      case 'traffic':
        return m.hasTrafficData
      case 'temperature':
        return m.hasTemperatureData
      case 'ping':
        return m.hasPingData
      case 'pingLoss':
        return m.hasPingLossData
      default:
        return true
    }
  }
  function getChartCardOrder(key: ChartDashboardCardKey): number {
    const index = m.chartDashboardCards.indexOf(key)
    return index < 0 ? 99 : index
  }
  function getChartCardStyle(key: ChartDashboardCardKey): Record<string, string> {
    return { order: String(getChartCardOrder(key)) }
  }
  
  // 连接数图表

  return Object.assign(m, { gpuDetailsFromStatus, averageGpuMemoryPercent, statusToRecordFormat, metricValue, getMetricDeviceKey, getMetricDeviceIndex, getMetricDeviceName, ensureMetricRow, applyMetricPoint, finalizeGpuRows, getGpuDeviceNames, metricSeriesToRecordFormat, seriesHasData, recordMetricSeries, gpuDeviceEntries, pingSeries, formatTime, formatTimeForTooltip, isChartCardEnabled, getChartCardOrder, getChartCardStyle, storageChartOption })
}
