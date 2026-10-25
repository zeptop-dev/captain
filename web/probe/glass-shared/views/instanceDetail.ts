// Pure view logic of the glass instance detail page, shared by the Vue and
// React views so both compute identical card values, labels and classes.
import { t } from '@shared/i18n'
import type { Snapshot } from '@shared/captain'
import { nodePrice } from '../../src/lib/node-price'
import { calculateNodeCost, formatCostAmount } from '../../src/lib/node-cost'
import type { NodeProviderMetadata } from '@shared/services/provider.service'
import type { DetailMetricCardKey, Lang } from '@shared/stores/app-settings'
import type { NodeData } from '@shared/types/node'
import type { ByteDecimalsConfig } from '@shared/utils/helper'
import { formatCityNameZh } from '@shared/utils/cityNameHelper'
import { gpuUsageFromStatus } from '@shared/utils/gpuHelper'
import { formatBytesPerSecondWithConfig, formatBytesWithConfig, formatNumber, formatUptimeWithFormat } from '@shared/utils/helper'
import { getExpireStatus, getExpireText } from '@shared/utils/tagHelper'

/** Inputs the Vue view reads from its stores and refs. */
export interface DetailContext {
  data: NodeData | undefined
  snapshot: Snapshot | null
  lang: Lang
  byteDecimals: ByteDecimalsConfig
  privateFeaturesAllowed: boolean
  hidePriceWhenLoggedOut: boolean
  providerMetadata: NodeProviderMetadata | null
}

export interface InfoItem {
  label: string
  value: string | undefined
  icon?: string
}

export interface MetricCard {
  key: DetailMetricCardKey
  label: string
  value: string
  unit?: string
  icon: string
  valueClass?: string
  tooltip?: string
  note?: string
  finance?: boolean
}

const EXPIRES_IN_SUFFIX_REGEX = /^(\d+)\s*(天|days?)$/i
const CURRENCY_SUFFIX_REGEX = /^(\S.*\S)\s+([A-Z]{3})$/

function publicCostInfo(ctx: DetailContext) {
  if (!ctx.snapshot || (ctx.snapshot.public_sections != null && !ctx.snapshot.public_sections.includes('info'))
    || (!ctx.privateFeaturesAllowed && ctx.hidePriceWhenLoggedOut))
    return undefined
  return ctx.snapshot.nodes.find(node => String(node.id) === ctx.data?.uuid)?.info
}

function costReasonText(reason: ReturnType<typeof calculateNodeCost>['remainingReason']): string {
  const keys = {
    'missing-price': 'finance.missingPrice',
    'legacy-price': 'finance.legacyPrice',
    'missing-expiry': 'finance.missingExpiry',
    'unsupported-cycle': 'finance.unsupportedCycle',
    invalid: 'finance.invalid',
  }
  return reason ? t(keys[reason]) : ''
}

function splitMetricValue(value: string): { value: string, unit?: string } {
  const cycleIndex = value.indexOf(' / ')
  if (cycleIndex > -1)
    return { value: value.slice(0, cycleIndex), unit: value.slice(cycleIndex) }
  const expiresInMatch = value.match(EXPIRES_IN_SUFFIX_REGEX)
  if (expiresInMatch)
    return { value: expiresInMatch[1] ?? value, unit: expiresInMatch[2] ?? undefined }
  const currencyMatch = value.match(CURRENCY_SUFFIX_REGEX)
  if (currencyMatch)
    return { value: currencyMatch[1] ?? value, unit: currencyMatch[2] ?? undefined }
  return { value }
}

export function detailFormatters(ctx: DetailContext) {
  return {
    formatBytes: (bytes: number) => formatBytesWithConfig(bytes, ctx.byteDecimals),
    formatBytesPerSecond: (bytes: number) => formatBytesPerSecondWithConfig(bytes, ctx.byteDecimals),
    formatUptime: (seconds: number) => formatUptimeWithFormat(seconds, 'minute'),
  }
}

export function nodePriceText(ctx: DetailContext): string {
  const info = publicCostInfo(ctx)
  return info ? nodePrice(info, ctx.lang) || '—' : '—'
}

export function monthlyAverageCostText(ctx: DetailContext): string {
  const info = publicCostInfo(ctx)
  const money = info ? calculateNodeCost(info).monthly : null
  return money ? `${formatCostAmount(money, ctx.lang)} ${money.currency} / ${t('finance.month')}` : '—'
}

export function remainingTimeText(ctx: DetailContext): string {
  const expires = publicCostInfo(ctx)?.expires_at
  return expires ? getExpireText(expires) : '—'
}

export function remainingValueText(ctx: DetailContext): string {
  const info = publicCostInfo(ctx)
  const money = info ? calculateNodeCost(info).remaining : null
  return money ? `${formatCostAmount(money, ctx.lang)} ${money.currency}` : '—'
}

export function remainingTimeValueClass(ctx: DetailContext): string {
  const expires = publicCostInfo(ctx)?.expires_at
  if (!expires)
    return ''
  const status = getExpireStatus(expires)
  if (status === 'expired' || status === 'critical')
    return 'text-destructive'
  if (status === 'warning')
    return 'text-orange-600 dark:text-orange-400'
  if (status === 'long_term')
    return 'text-muted-foreground'
  return 'text-emerald-600 dark:text-emerald-400'
}

export function usagePercentage(used: number, total: number): number | null {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0)
    return null
  return Math.min(100, Math.max(0, used / total * 100))
}

export function splitMeasurement(value: string): { value: string, unit?: string } {
  const separatorIndex = value.lastIndexOf(' ')
  if (separatorIndex <= 0)
    return { value }
  return {
    value: value.slice(0, separatorIndex),
    unit: value.slice(separatorIndex + 1),
  }
}

export function getDetailMetricCard(ctx: DetailContext, key: DetailMetricCardKey): MetricCard {
  const { formatBytes, formatBytesPerSecond, formatUptime } = detailFormatters(ctx)
  const node = ctx.data
  const remainingTime = splitMetricValue(remainingTimeText(ctx))
  const memoryUsage = usagePercentage(node?.ram ?? 0, node?.mem_total ?? 0)
  const swapUsage = usagePercentage(node?.swap ?? 0, node?.swap_total ?? 0)
  const diskUsage = usagePercentage(node?.disk ?? 0, node?.disk_total ?? 0)
  const nodeTrafficUsed = ctx.snapshot?.nodes.find(n => String(n.id) === node?.uuid)?.traffic.used ?? Number.NaN
  const nodeTrafficLimit = node?.traffic_limit ?? 0
  const nodeHasTrafficLimit = nodeTrafficLimit > 0
  const nodeTrafficPercentage = nodeHasTrafficLimit
    ? Math.min(nodeTrafficUsed / nodeTrafficLimit * 100, 100)
    : 0

  switch (key) {
    case 'nodePrice':
      return { key, label: t('节点价格'), ...splitMetricValue(nodePriceText(ctx)), icon: 'tabler:cash', finance: true }
    case 'monthlyCost': {
      const info = publicCostInfo(ctx)
      const cost = info ? calculateNodeCost(info) : null
      return { key, label: t('月均支出'), ...splitMetricValue(monthlyAverageCostText(ctx)), icon: 'tabler:receipt-2', finance: true,
        note: info && !cost?.monthly ? costReasonText(cost?.remainingReason ?? 'missing-price') : undefined }
    }
    case 'remainingTime':
      return { key, label: t('剩余时间'), value: remainingTime.value, unit: remainingTime.unit, icon: 'tabler:calendar-dollar', valueClass: remainingTimeValueClass(ctx) }
    case 'remainingValue': {
      const info = publicCostInfo(ctx)
      const cost = info ? calculateNodeCost(info) : null
      return { key, label: t('finance.remainingEstimate'), ...splitMetricValue(remainingValueText(ctx)), icon: 'tabler:coins', finance: true,
        tooltip: cost?.remaining ? t('finance.estimateHelp') : undefined,
        note: info && !cost?.remaining ? costReasonText(cost?.remainingReason ?? 'missing-price') : undefined }
    }
    case 'cpuUsage':
      return { key, label: 'CPU 使用率', value: formatNumber(node?.cpu ?? Number.NaN, 1), unit: '%', icon: 'tabler:cpu' }
    case 'gpuUsage': {
      const gpu = gpuUsageFromStatus(node)
      const hasGpu = Boolean(node?.gpu_name?.trim()) || gpu > 0
      return { key, label: 'GPU 使用率', value: hasGpu ? gpu.toFixed(1) : '-', unit: hasGpu ? '%' : undefined, icon: 'tabler:device-desktop-analytics', tooltip: node?.gpu_name?.trim() || undefined }
    }
    case 'memoryUsage':
      return { key, label: '内存使用率', value: memoryUsage === null ? '-' : memoryUsage.toFixed(1), unit: memoryUsage === null ? undefined : '%', icon: 'icon-park-outline:memory', tooltip: `${formatBytes(node?.ram ?? 0)} / ${formatBytes(node?.mem_total ?? 0)}` }
    case 'swapUsage':
      return { key, label: '交换内存', value: swapUsage === null ? '-' : swapUsage.toFixed(1), unit: swapUsage === null ? undefined : '%', icon: 'icon-park-outline:switch', tooltip: `${formatBytes(node?.swap ?? 0)} / ${formatBytes(node?.swap_total ?? 0)}` }
    case 'diskUsage':
      return { key, label: '硬盘使用率', value: diskUsage === null ? '-' : diskUsage.toFixed(1), unit: diskUsage === null ? undefined : '%', icon: 'tabler:server-2', tooltip: `${formatBytes(node?.disk ?? 0)} / ${formatBytes(node?.disk_total ?? 0)}` }
    case 'load':
      return { key, label: '系统负载', value: formatNumber(node?.load ?? Number.NaN, 2), unit: '1m', icon: 'tabler:chart-line', tooltip: `5m ${formatNumber(node?.load5 ?? Number.NaN, 2)} / 15m ${formatNumber(node?.load15 ?? Number.NaN, 2)}` }
    case 'temperature': {
      const temperature = node?.temp ?? 0
      return { key, label: '系统温度', value: temperature > 0 ? temperature.toFixed(1) : '-', unit: temperature > 0 ? '°C' : undefined, icon: 'tabler:temperature' }
    }
    case 'processes':
      return { key, label: '进程数', value: Math.round(node?.process ?? 0).toLocaleString('zh-CN'), icon: 'tabler:list-numbers' }
    case 'connections':
      return { key, label: t('连接数'), value: Math.round((node?.connections ?? 0) + (node?.connections_udp ?? 0)).toLocaleString('zh-CN'), icon: 'tabler:plug-connected', tooltip: `TCP ${node?.connections ?? 0} / UDP ${node?.connections_udp ?? 0}` }
    case 'uptime':
      return { key, label: t('运行时间'), value: formatUptime(node?.uptime ?? 0), icon: 'tabler:clock-up' }
    case 'uploadSpeed': {
      const speed = splitMeasurement(formatBytesPerSecond(node?.net_out ?? 0))
      return { key, label: t('实时上行'), value: speed.value, unit: speed.unit, icon: 'tabler:chevrons-up' }
    }
    case 'downloadSpeed': {
      const speed = splitMeasurement(formatBytesPerSecond(node?.net_in ?? 0))
      return { key, label: t('实时下行'), value: speed.value, unit: speed.unit, icon: 'tabler:chevrons-down' }
    }
    case 'totalTraffic': {
      const traffic = splitMeasurement(formatBytes((node?.net_total_up ?? Number.NaN) + (node?.net_total_down ?? Number.NaN)))
      return { key, label: t('累计流量'), value: traffic.value, unit: traffic.unit, icon: 'tabler:arrows-transfer-up-down', tooltip: `${t('本周期出站')} ${formatBytes(node?.net_total_up ?? Number.NaN)} / ${t('本周期入站')} ${formatBytes(node?.net_total_down ?? Number.NaN)}\n${t('周期流量说明')}` }
    }
    case 'trafficQuota':
      return {
        key,
        label: t('流量配额'),
        value: nodeHasTrafficLimit ? nodeTrafficPercentage.toFixed(1) : '∞',
        unit: nodeHasTrafficLimit ? '%' : undefined,
        icon: 'tabler:gauge',
        tooltip: nodeHasTrafficLimit ? `${formatBytes(nodeTrafficUsed)} / ${formatBytes(nodeTrafficLimit)}` : '无限流量',
      }
    default:
      return { key: 'cpuUsage', label: 'CPU 使用率', value: formatNumber(node?.cpu ?? Number.NaN, 1), unit: '%', icon: 'tabler:cpu' }
  }
}

export function providerDisplay(ctx: DetailContext): string {
  const parts: string[] = []
  const cityName = formatCityNameZh(ctx.providerMetadata?.geo?.city)
  if (cityName)
    parts.push(cityName)
  if (ctx.providerMetadata?.provider?.displayName)
    parts.push(ctx.providerMetadata.provider.displayName)
  if (ctx.providerMetadata?.geo?.asn)
    parts.push(ctx.providerMetadata.geo.asn)
  return parts.length ? parts.join(' · ') : '-'
}

export function hardwareSmallItems(ctx: DetailContext): InfoItem[] {
  const node = ctx.data
  const items: InfoItem[] = []

  const ip = node?.ipv4 || node?.ipv6
  if (ctx.privateFeaturesAllowed && ip)
    items.push({ label: 'IP', value: ip, icon: 'tabler:world' })
  else
    items.push({ label: t('架构'), value: node?.arch ?? '-', icon: 'icon-park-outline:application-two' })

  const physicalCores = node?.cpu_physical_cores
  if (typeof physicalCores === 'number' && physicalCores > 0)
    items.push({ label: '物理核心', value: `${physicalCores} 核`, icon: 'tabler:cpu' })

  items.push({ label: t('虚拟化'), value: node?.virtualization ?? '-', icon: 'icon-park-outline:server' })

  const gpu = node?.gpu_name?.trim()
  if (gpu && gpu.toLowerCase() !== 'none')
    items.push({ label: 'GPU', value: gpu, icon: 'icon-park-outline:video-one' })

  return items
}

export function systemInfo(ctx: DetailContext): InfoItem[] {
  const { formatUptime } = detailFormatters(ctx)
  const vpsProvider = ctx.providerMetadata?.provider ?? null
  return [
  { label: t('操作系统'), value: ctx.data?.os ?? '-', icon: 'icon-park-outline:computer' },
  { label: t('内核版本'), value: ctx.data?.kernel_version ?? '-', icon: 'icon-park-outline:code' },
  { label: t('运行时间'), value: formatUptime(ctx.data?.uptime ?? 0), icon: 'icon-park-outline:timer' },
  { label: t('厂商'), value: providerDisplay(ctx), icon: vpsProvider?.primary.icon ?? 'icon-park-outline:server' },
]
}

export function storageInfo(ctx: DetailContext): InfoItem[] {
  const { formatBytes } = detailFormatters(ctx)
  return [
  { label: t('内存'), value: formatBytes(ctx.data?.mem_total ?? 0), icon: 'icon-park-outline:memory' },
  { label: t('内存交换'), value: formatBytes(ctx.data?.swap_total ?? 0), icon: 'icon-park-outline:switch' },
  { label: t('硬盘'), value: formatBytes(ctx.data?.disk_total ?? 0), icon: 'icon-park-outline:hard-disk' },
]
}

export function metricCards(ctx: DetailContext, order: DetailMetricCardKey[], shows: (section: string) => boolean): MetricCard[] {
  return order.filter(key => shows(({cpuUsage:'cpu',memoryUsage:'memory',swapUsage:'memory',diskUsage:'disk',uploadSpeed:'network',downloadSpeed:'network',totalTraffic:'traffic',trafficQuota:'traffic',connections:'system',processes:'system',uptime:'system',load:'system',remainingTime:'info',nodePrice:'info',monthlyCost:'info',remainingValue:'info'} as Record<string,string>)[key] ?? 'system')).map(key => getDetailMetricCard(ctx, key))
}
