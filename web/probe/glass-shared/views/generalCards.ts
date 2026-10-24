// Pure computations behind the home page overview cards, shared by the Vue
// and React NodeGeneralCards so both show identical values and tooltips.
import { t } from '@shared/i18n'
import type { Snapshot } from '@shared/captain'
import type { Lang } from '@shared/stores/app-settings'
import { summarizeNodeCosts, formatCostAmount } from '../../src/lib/node-cost'
import type { GeneralCardKey } from '@shared/stores/app-settings'
import type { NodeData } from '@shared/types/node'
import type { ByteDecimalsConfig } from '@shared/utils/helper'
import type { TopNodeMetric } from '@shared/utils/nodeMetricsHelper'
import { gpuUsageFromStatus } from '@shared/utils/gpuHelper'
import { formatBytesPerSecondSplit, formatBytesSplit } from '@shared/utils/helper'
import {
  getConnectionCount,
  getExpiryDays,
  getHighLoadMetrics,
  getRealtimeTotalSpeed,
  getTrafficUsed,
  getTrafficUsedPercentage,
  isExpiringNode,
  isHighLoadNode,
  isTrafficWarningNode,
} from '@shared/utils/nodeMetricsHelper'
import { getRegionDisplayName } from '@shared/utils/regionHelper'

export interface GeneralMetricCard {
  key: GeneralCardKey
  label: string
  icon: string
  value: string
  unit?: string
  tooltip?: string
  moneyRows?: Array<{ currency: string; value: string }>
  action?: 'financeDetails'
}

export interface OnlineStats {
  count: number
  totalSpeed: { up: number, down: number }
  avgCpu: number
  totalGpu: number
  gpuNodeCount: number
  avgLoad: number
  avgLoad5: number
  avgLoad15: number
  totalProcesses: number
  totalConnectionsTcp: number
  totalConnectionsUdp: number
  trafficPeak: TopNodeMetric | null
  uploadPeakNode: TopNodeMetric | null
  downloadPeakNode: TopNodeMetric | null
  gpuPeakNode: TopNodeMetric | null
  connectionPeakNode: TopNodeMetric | null
  highLoadNodes: NodeData[]
}


export interface GeneralCardsContext {
  snapshot: Snapshot | null
  lang: Lang
  nodes: NodeData[]
  byteDecimals: ByteDecimalsConfig
  homeHighLoadThreshold: number
  homeExpiringDays: number
  homeTrafficWarningThreshold: number
  showPrice: boolean
  now: Date
}

export function buildGeneralCards(ctx: GeneralCardsContext, order: GeneralCardKey[]): GeneralMetricCard[] {
  function formatBytesText(bytes: number): string {
    const formatted = formatBytesSplit(bytes, ctx.byteDecimals)
    return `${formatted.value} ${formatted.unit}`
  }

  function formatSpeedText(bytes: number): string {
    const formatted = formatBytesPerSecondSplit(bytes, ctx.byteDecimals)
    return `${formatted.value} ${formatted.unit}`
  }

  function formatCount(value: number): string {
    return Math.round(value).toLocaleString('zh-CN')
  }

  function formatDecimal(value: number, digits = 1): string {
    if (!Number.isFinite(value))
      return '0'
    return value.toFixed(digits)
  }

  function formatTopNodeSpeed(metric: TopNodeMetric | null, fallback = '-'): { value: string, unit?: string, tooltip?: string } {
    if (!metric || metric.value <= 0)
      return { value: fallback }

    const formatted = formatBytesPerSecondSplit(metric.value, ctx.byteDecimals)
    return {
      value: formatted.value,
      unit: formatted.unit,
      tooltip: `${metric.node.name}\n↑ ${formatSpeedText(metric.node.net_out || 0)}\n↓ ${formatSpeedText(metric.node.net_in || 0)}`,
    }
  }

  function formatTopNodePercentage(metric: TopNodeMetric | null): { value: string, unit?: string, tooltip?: string } {
    if (!metric)
      return { value: '-' }

    const gpuName = metric.node.gpu_name?.trim()
    return {
      value: formatDecimal(metric.value),
      unit: '%',
      tooltip: [metric.node.name, gpuName, `GPU ${formatDecimal(metric.value)}%`].filter(Boolean).join('\n'),
    }
  }

  function formatNodeNames(nodes: NodeData[], formatter?: (node: NodeData) => string, max = 8): string {
    if (nodes.length === 0)
      return t('暂无节点')

    const lines = nodes.slice(0, max).map(node => formatter ? formatter(node) : node.name)
    if (nodes.length > max)
      lines.push(`… 还有 ${nodes.length - max} 台`)
    return lines.join('\n')
  }

  function getDistribution(nodes: NodeData[], selector: (node: NodeData) => string | null | undefined): Array<[string, number]> {
    const map = new Map<string, number>()
    for (const node of nodes) {
      const key = selector(node)?.trim() || '未知'
      map.set(key, (map.get(key) || 0) + 1)
    }

    return Array.from(map.entries()).sort((a, b) => b[1] - a[1])
  }

  function getKnownDistribution(nodes: NodeData[], selector: (node: NodeData) => string | null | undefined): Array<[string, number]> {
    const map = new Map<string, number>()
    for (const node of nodes) {
      const key = selector(node)?.trim()
      if (!key)
        continue
      map.set(key, (map.get(key) || 0) + 1)
    }

    return Array.from(map.entries()).sort((a, b) => b[1] - a[1])
  }

  function formatDistributionTooltip(entries: Array<[string, number]>): string {
    if (entries.length === 0)
      return t('暂无数据')

    return entries.slice(0, 8).map(([key, count]) => `${key}: ${count} 台`).join('\n')
  }

  function formatExpiryNode(node: NodeData): string {
    const days = getExpiryDays(node)
    if (days === null)
      return `${node.name}: 未知`
    if (days <= 0)
      return `${node.name}: 已过期`
    return `${node.name}: ${days} 天`
  }

  const priceVisible = ctx.showPrice && Boolean(ctx.snapshot)
    && (ctx.snapshot?.public_sections == null || ctx.snapshot.public_sections.includes('info'))
  const currentNodes = new Map(ctx.snapshot?.nodes.map(node => [String(node.id), node]))
  const costInfos = priceVisible ? ctx.nodes.flatMap(node => {
    const current = currentNodes.get(node.uuid)
    return current ? [current.info] : []
  }) : []

  function costCard(kind: 'monthly' | 'yearly' | 'remaining'): Pick<GeneralMetricCard, 'value' | 'unit' | 'moneyRows' | 'tooltip'> {
    const summary = summarizeNodeCosts(costInfos, kind, ctx.now)
    const moneyRows = summary.amounts.map(money => ({ currency: money.currency, value: formatCostAmount(money, ctx.lang) }))
    const first = moneyRows[0]
    return {
      value: first?.value ?? '—',
      unit: moneyRows.length === 1 ? first?.currency : undefined,
      moneyRows: moneyRows.length > 1 ? moneyRows : undefined,
      tooltip: kind === 'remaining' ? t('finance.estimateHelp') : undefined,
    }
  }

  function updateTopMetric(current: TopNodeMetric | null, node: NodeData, value: number): TopNodeMetric | null {
    if (!Number.isFinite(value))
      return current

    if (!current || value > current.value)
      return { node, value: Math.max(0, value) }

    return current
  }

  const onlineStats: OnlineStats = (() => {
    const stats: OnlineStats = {
      count: 0,
      totalSpeed: { up: 0, down: 0 },
      avgCpu: 0,
      totalGpu: 0,
      gpuNodeCount: 0,
      avgLoad: 0,
      avgLoad5: 0,
      avgLoad15: 0,
      totalProcesses: 0,
      totalConnectionsTcp: 0,
      totalConnectionsUdp: 0,
      trafficPeak: null,
      uploadPeakNode: null,
      downloadPeakNode: null,
      gpuPeakNode: null,
      connectionPeakNode: null,
      highLoadNodes: [],
    }

    for (const node of ctx.nodes) {
      if (!node.online)
        continue

      stats.count += 1
      stats.totalSpeed.up += node.net_out || 0
      stats.totalSpeed.down += node.net_in || 0
      stats.avgCpu += node.cpu || 0
      stats.avgLoad += node.load || 0
      stats.avgLoad5 += node.load5 || 0
      stats.avgLoad15 += node.load15 || 0
      stats.totalProcesses += node.process || 0
      stats.totalConnectionsTcp += node.connections || 0
      stats.totalConnectionsUdp += node.connections_udp || 0
      stats.trafficPeak = updateTopMetric(stats.trafficPeak, node, getRealtimeTotalSpeed(node))
      stats.uploadPeakNode = updateTopMetric(stats.uploadPeakNode, node, node.net_out || 0)
      stats.downloadPeakNode = updateTopMetric(stats.downloadPeakNode, node, node.net_in || 0)
      stats.connectionPeakNode = updateTopMetric(stats.connectionPeakNode, node, getConnectionCount(node))
      const gpu = gpuUsageFromStatus(node)
      const hasGpu = Boolean(node.gpu_name?.trim()) || gpu > 0
      if (hasGpu) {
        stats.totalGpu += gpu
        stats.gpuNodeCount += 1
        stats.gpuPeakNode = updateTopMetric(stats.gpuPeakNode, node, gpu)
      }
      if (isHighLoadNode(node, ctx.homeHighLoadThreshold))
        stats.highLoadNodes.push(node)
    }

    if (stats.count > 0) {
      stats.avgCpu /= stats.count
      stats.avgLoad /= stats.count
      stats.avgLoad5 /= stats.count
      stats.avgLoad15 /= stats.count
    }

    return stats
  })()

  const totalSpeed = ctx.nodes.some(n => n.online && Number.isFinite(n.net_in)) ? onlineStats.totalSpeed : {up: Number.NaN, down: Number.NaN}

  const totalTraffic = (() => {
    const up = ctx.nodes.reduce((sum, node) => sum + node.net_total_up, 0)
    const down = ctx.nodes.reduce((sum, node) => sum + node.net_total_down, 0)
    return ctx.nodes.length > 0 ? {up, down} : {up: Number.NaN, down: Number.NaN}
  })()

  const formattedTrafficUp = formatBytesSplit(totalTraffic.up, ctx.byteDecimals)
  const formattedTrafficDown = formatBytesSplit(totalTraffic.down, ctx.byteDecimals)
  const totalTrafficTooltip = formatBytesSplit(totalTraffic.up + totalTraffic.down, ctx.byteDecimals)

  const formattedSpeedUp = formatBytesPerSecondSplit(totalSpeed.up, ctx.byteDecimals)
  const formattedSpeedDown = formatBytesPerSecondSplit(totalSpeed.down, ctx.byteDecimals)

  // ==================== 内存 / 硬盘 / 交换内存 汇总 ====================
  // 离线节点的 ram / disk / swap 为 0，不影响 used 求和；total 是静态库存信息，按全量统计
  const totalMemory = (() => {
    let used = 0
    let total = 0
    for (const node of ctx.nodes) {
      used += node.ram || 0
      total += node.mem_total || 0
    }
    return ctx.nodes.some(n => Number.isFinite(n.ram) && Number.isFinite(n.mem_total)) ? { used, total } : { used: Number.NaN, total: Number.NaN }
  })()

  const totalDisk = (() => {
    let used = 0
    let total = 0
    for (const node of ctx.nodes) {
      used += node.disk || 0
      total += node.disk_total || 0
    }
    return ctx.nodes.some(n => Number.isFinite(n.disk) && Number.isFinite(n.disk_total)) ? { used, total } : { used: Number.NaN, total: Number.NaN }
  })()

  const totalSwap = (() => {
    let used = 0
    let total = 0
    for (const node of ctx.nodes) {
      used += node.swap || 0
      total += node.swap_total || 0
    }
    return ctx.nodes.some(n => Number.isFinite(n.swap) && Number.isFinite(n.swap_total)) ? { used, total } : { used: Number.NaN, total: Number.NaN }
  })()

  const formattedMemoryUsed = formatBytesSplit(totalMemory.used, ctx.byteDecimals)
  const formattedMemoryTotal = formatBytesSplit(totalMemory.total, ctx.byteDecimals)
  const formattedDiskUsed = formatBytesSplit(totalDisk.used, ctx.byteDecimals)
  const formattedDiskTotal = formatBytesSplit(totalDisk.total, ctx.byteDecimals)
  const formattedSwapUsed = formatBytesSplit(totalSwap.used, ctx.byteDecimals)
  const formattedSwapTotal = formatBytesSplit(totalSwap.total, ctx.byteDecimals)

  const onlineNodeCount = onlineStats.count
  const totalNodeCount = ctx.nodes.length
  const avgCpu = onlineStats.avgCpu
  const avgGpu = onlineStats.gpuNodeCount > 0
    ? onlineStats.totalGpu / onlineStats.gpuNodeCount
    : null
  const gpuNodes = ctx.nodes.filter(node => Boolean(node.gpu_name?.trim()) || (node.gpu || 0) > 0)
  const onlineGpuNodes = gpuNodes.filter(node => node.online)
  const gpuPeakNode = onlineStats.gpuPeakNode
  const avgLoad = onlineStats.avgLoad
  const avgLoad5 = onlineStats.avgLoad5
  const avgLoad15 = onlineStats.avgLoad15
  const totalProcesses = onlineStats.totalProcesses
  const totalConnectionsTcp = onlineStats.totalConnectionsTcp
  const totalConnectionsUdp = onlineStats.totalConnectionsUdp
  const totalCpuCores = ctx.nodes.reduce((sum, node) => sum + (node.cpu_cores || 0), 0)
  const trafficQuota = (() => {
    let used = 0
    let limit = 0

    for (const node of ctx.nodes) {
      if ((node.traffic_limit || 0) <= 0)
        continue
      used += getTrafficUsed(node)
      limit += node.traffic_limit || 0
    }

    return { used, limit }
  })()
  const trafficQuotaPercentage = (() => {
    if (trafficQuota.limit <= 0)
      return 0
    return trafficQuota.used / trafficQuota.limit * 100
  })()

  const trafficPeak = onlineStats.trafficPeak
  const uploadPeakNode = onlineStats.uploadPeakNode
  const downloadPeakNode = onlineStats.downloadPeakNode
  const connectionPeakNode = onlineStats.connectionPeakNode
  const offlineNodes = ctx.nodes.filter(node => !node.online)
  const highLoadNodes = onlineStats.highLoadNodes
  const expiringNodes = ctx.nodes.filter(node => isExpiringNode(node, ctx.homeExpiringDays))
  const trafficWarningNodes = ctx.nodes.filter(node => isTrafficWarningNode(node, ctx.homeTrafficWarningThreshold))
  const regionDistribution = getKnownDistribution(ctx.nodes, node => getRegionDisplayName(node.region))
  const systemDistribution = getDistribution(ctx.nodes, node => node.os)
  const virtualizationDistribution = getDistribution(ctx.nodes, node => node.virtualization)

  const trafficPeakCard = formatTopNodeSpeed(trafficPeak)
  const uploadPeakCard = formatTopNodeSpeed(uploadPeakNode)
  const downloadPeakCard = formatTopNodeSpeed(downloadPeakNode)
  const gpuPeakCard = formatTopNodePercentage(gpuPeakNode)
  const currentTimeText = ctx.now.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
  const currentDateText = ctx.now.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'short',
  })
  const connectionPeakTooltip = (() => {
    const metric = connectionPeakNode
    if (!metric)
      return t('暂无数据')
    return `${metric.node.name}\nTCP ${formatCount(metric.node.connections || 0)}\nUDP ${formatCount(metric.node.connections_udp || 0)}`
  })()

  function getCardDefinition(key: GeneralCardKey): GeneralMetricCard {
    switch (key) {
      case 'currentTime':
        return {
          key: 'currentTime',
          label: '当前时间',
          icon: 'tabler:clock',
          value: currentTimeText,
          tooltip: currentDateText,
        }
      case 'memory':
        return {
          key: 'memory',
          label: t('内存用量'),
          icon: 'icon-park-outline:memory',
          value: formattedMemoryUsed.value,
          unit: `${formattedMemoryUsed.unit} / ${formattedMemoryTotal.value} ${formattedMemoryTotal.unit}`,
        }
      case 'disk':
        return {
          key: 'disk',
          label: t('硬盘用量'),
          icon: 'tabler:server-2',
          value: formattedDiskUsed.value,
          unit: `${formattedDiskUsed.unit} / ${formattedDiskTotal.value} ${formattedDiskTotal.unit}`,
        }
      case 'remainingValue':
        return {
          key: 'remainingValue',
          label: t('finance.remainingEstimate'),
          icon: 'tabler:cash',
          ...costCard('remaining'),
        }
      case 'totalTraffic':
        return {
          key: 'totalTraffic',
          label: t('累计流量'),
          icon: 'tabler:download',
          value: totalTrafficTooltip.value,
          unit: totalTrafficTooltip.unit,
          tooltip: `${t('本周期出站')} ${formattedTrafficUp.value} ${formattedTrafficUp.unit}\n${t('本周期入站')} ${formattedTrafficDown.value} ${formattedTrafficDown.unit}\n${t('汇总周期流量说明')}`,
        }
      case 'uploadSpeed':
        return {
          key: 'uploadSpeed',
          label: t('实时上行'),
          icon: 'tabler:chevrons-up',
          value: formattedSpeedUp.value,
          unit: formattedSpeedUp.unit,
        }
      case 'downloadSpeed':
        return {
          key: 'downloadSpeed',
          label: t('实时下行'),
          icon: 'tabler:chevrons-down',
          value: formattedSpeedDown.value,
          unit: formattedSpeedDown.unit,
        }
      case 'onlineNodes':
        return {
          key: 'onlineNodes',
          label: '在线节点',
          icon: 'tabler:activity-heartbeat',
          value: formatCount(onlineNodeCount),
          unit: `/ ${formatCount(totalNodeCount)}`,
        }
      case 'avgCpu':
        return {
          key: 'avgCpu',
          label: '平均 CPU',
          icon: 'tabler:cpu',
          value: formatDecimal(avgCpu),
          unit: '%',
        }
      case 'avgGpu':
        return {
          key: 'avgGpu',
          label: '平均 GPU',
          icon: 'tabler:device-desktop-analytics',
          value: avgGpu === null ? '-' : formatDecimal(avgGpu),
          unit: avgGpu === null ? undefined : '%',
          tooltip: formatNodeNames(onlineGpuNodes, node => `${node.name}: ${formatDecimal(node.gpu || 0)}%`),
        }
      case 'avgLoad':
        return {
          key: 'avgLoad',
          label: '平均负载',
          icon: 'tabler:chart-line',
          value: formatDecimal(avgLoad, 2),
          tooltip: `1m ${formatDecimal(avgLoad, 2)}\n5m ${formatDecimal(avgLoad5, 2)}\n15m ${formatDecimal(avgLoad15, 2)}`,
        }
      case 'swap':
        return {
          key: 'swap',
          label: '交换内存',
          icon: 'icon-park-outline:switch',
          value: formattedSwapUsed.value,
          unit: `${formattedSwapUsed.unit} / ${formattedSwapTotal.value} ${formattedSwapTotal.unit}`,
        }
      case 'processes':
        return {
          key: 'processes',
          label: '进程总数',
          icon: 'tabler:list-numbers',
          value: formatCount(totalProcesses),
        }
      case 'connections':
        return {
          key: 'connections',
          label: t('连接数'),
          icon: 'tabler:plug-connected',
          value: formatCount(totalConnectionsTcp + totalConnectionsUdp),
          tooltip: `TCP ${formatCount(totalConnectionsTcp)}\nUDP ${formatCount(totalConnectionsUdp)}`,
        }
      case 'cpuCores':
        return {
          key: 'cpuCores',
          label: 'CPU 核心',
          icon: 'tabler:cpu',
          value: formatCount(totalCpuCores),
          unit: 'Core',
        }
      case 'gpuNodes':
        return {
          key: 'gpuNodes',
          label: 'GPU 节点',
          icon: 'tabler:device-imac',
          value: formatCount(gpuNodes.length),
          unit: `/ ${formatCount(totalNodeCount)}`,
          tooltip: formatNodeNames(gpuNodes, node => `${node.name}: ${node.gpu_name?.trim() || 'GPU'}`),
        }
      case 'gpuPeakNode':
        return {
          key: 'gpuPeakNode',
          label: 'GPU 峰值',
          icon: 'tabler:chart-histogram',
          value: gpuPeakCard.value,
          unit: gpuPeakCard.unit,
          tooltip: gpuPeakCard.tooltip,
        }
      case 'trafficQuota':
        return {
          key: 'trafficQuota',
          label: t('流量配额'),
          icon: 'tabler:gauge',
          value: trafficQuota.limit > 0 ? formatDecimal(trafficQuotaPercentage) : '-',
          unit: trafficQuota.limit > 0 ? '%' : undefined,
          tooltip: trafficQuota.limit > 0
            ? `${formatBytesText(trafficQuota.used)} / ${formatBytesText(trafficQuota.limit)}`
            : '无限流量',
        }
      case 'trafficPeak':
        return {
          key: 'trafficPeak',
          label: '实时峰值',
          icon: 'tabler:activity',
          value: trafficPeakCard.value,
          unit: trafficPeakCard.unit,
          tooltip: trafficPeakCard.tooltip,
        }
      case 'uploadPeakNode':
        return {
          key: 'uploadPeakNode',
          label: '上行最高',
          icon: 'tabler:arrow-big-up-lines',
          value: uploadPeakCard.value,
          unit: uploadPeakCard.unit,
          tooltip: uploadPeakCard.tooltip,
        }
      case 'downloadPeakNode':
        return {
          key: 'downloadPeakNode',
          label: '下行最高',
          icon: 'tabler:arrow-big-down-lines',
          value: downloadPeakCard.value,
          unit: downloadPeakCard.unit,
          tooltip: downloadPeakCard.tooltip,
        }
      case 'offlineNodes':
        return {
          key: 'offlineNodes',
          label: '离线节点',
          icon: 'tabler:plug-connected-x',
          value: formatCount(offlineNodes.length),
          unit: `/ ${formatCount(totalNodeCount)}`,
          tooltip: formatNodeNames(offlineNodes),
        }
      case 'highLoadNodes':
        return {
          key: 'highLoadNodes',
          label: '高负载节点',
          icon: 'tabler:alert-triangle',
          value: formatCount(highLoadNodes.length),
          unit: `/ ${formatCount(onlineNodeCount)}`,
          tooltip: formatNodeNames(highLoadNodes, (node) => {
            const metrics = getHighLoadMetrics(node, ctx.homeHighLoadThreshold)
            return `${node.name}: ${metrics.map(metric => `${metric.label} ${formatDecimal(metric.percentage)}%`).join(' / ')}`
          }),
        }
      case 'expiringNodes':
        return {
          key: 'expiringNodes',
          label: t('即将到期'),
          icon: 'tabler:calendar-exclamation',
          value: formatCount(expiringNodes.length),
          unit: '台',
          tooltip: formatNodeNames(expiringNodes, formatExpiryNode),
        }
      case 'trafficWarnings':
        return {
          key: 'trafficWarnings',
          label: '流量预警',
          icon: 'tabler:traffic-cone',
          value: formatCount(trafficWarningNodes.length),
          unit: '台',
          tooltip: formatNodeNames(trafficWarningNodes, node => `${node.name}: ${formatDecimal(getTrafficUsedPercentage(node))}%`),
        }
      case 'connectionPeakNode':
        return {
          key: 'connectionPeakNode',
          label: '连接峰值',
          icon: 'tabler:plug-connected',
          value: connectionPeakNode ? formatCount(connectionPeakNode.value) : '-',
          tooltip: connectionPeakTooltip,
        }
      case 'regionDistribution':
        return {
          key: 'regionDistribution',
          label: '地区分布',
          icon: 'tabler:map-pin',
          value: formatCount(regionDistribution.length),
          unit: '个',
          tooltip: formatDistributionTooltip(regionDistribution),
        }
      case 'systemDistribution':
        return {
          key: 'systemDistribution',
          label: '系统分布',
          icon: 'tabler:device-desktop',
          value: systemDistribution[0]?.[0] ?? '-',
          unit: systemDistribution[0] ? `${systemDistribution[0][1]} 台` : undefined,
          tooltip: formatDistributionTooltip(systemDistribution),
        }
      case 'virtualizationDistribution':
        return {
          key: 'virtualizationDistribution',
          label: t('虚拟化'),
          icon: 'tabler:box-multiple',
          value: virtualizationDistribution[0]?.[0] ?? '-',
          unit: virtualizationDistribution[0] ? `${virtualizationDistribution[0][1]} 台` : undefined,
          tooltip: formatDistributionTooltip(virtualizationDistribution),
        }
      case 'monthlyCost':
        return {
          key: 'monthlyCost',
          label: t('月均支出'),
          icon: 'tabler:calendar-dollar',
          ...costCard('monthly'),
        }
      case 'yearlyCost':
        return {
          key: 'yearlyCost',
          label: t('finance.yearlyCost'),
          icon: 'tabler:receipt-2',
          ...costCard('yearly'),
        }
      default:
        return getCardDefinition('memory')
    }
  }

  return order.filter(key => !['remainingValue', 'monthlyCost', 'yearlyCost'].includes(key) || priceVisible).map(getCardDefinition)
}
