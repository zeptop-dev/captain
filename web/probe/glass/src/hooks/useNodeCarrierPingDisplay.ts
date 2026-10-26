// Carrier colours and bars from the MIT three-network theme; Captain supplies counted history.
import { useMemo } from 'react'
import { shows, snapshot } from '@shared/captain'
import { CARRIER_DOT_CLASSES, buildEmptyBars, buildHistoryBars } from '@shared/composables/pingBars'
import { t } from '@shared/i18n'
import { carriers, matchesPing, selectPings } from '@shared/ping-selection'
import { useBox } from './useBox'
import { useNodePingStats } from './useNodePingStats'

if (carriers.length !== 3)
  throw new Error('useNodeCarrierPingDisplay expects exactly three carriers')

const reasonText = (reason?: string) => ({
  agent_upgrade_required: t('升级服务器端以启用所选探测'), waiting: t('等待采样'),
  permission: t('ICMP 权限不足'), permission_denied: t('ICMP 权限不足'),
  unreachable: t('探测目标不可达'), network_unreachable: t('探测目标不可达'), no_route: t('探测目标不可达'),
  dns: t('目标解析失败'), dns_error: t('目标解析失败'), dns_failed: t('目标解析失败'),
  timeout: t('探测超时'),
})[reason ?? '']

export function useNodeCarrierPingDisplay(uuid: string, enabled: boolean) {
  const snap = useBox(snapshot)
  const node = snap?.nodes.find(n => String(n.id) === uuid)
  const selection = useMemo(() => selectPings(node, snap?.carrier_ping ?? false), [node, snap?.carrier_ping])
  const on = enabled && selection.threeNetwork && shows('history') && shows('latency')
  const sourceOf = (i: number, slot: number) => selection.carriers[i]?.sources[slot] ?? null
  // Fixed six-hook order: a family missing samples must not borrow the other family.
  const states = [
    [useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(0, 0) }), useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(0, 1) })],
    [useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(1, 0) }), useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(1, 1) })],
    [useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(2, 0) }), useNodePingStats(uuid, { hours: 1, enabled: on, source: sourceOf(2, 1) })],
  ]
  const historyShown = shows('history')
  const carrierDisplays = carriers.map((c, i) => {
    const label = t(c.label)
    const series = selection.families.map((family, slot) => {
      const state = states[i]![slot]!, source = sourceOf(i, slot)
      const sample = source ? node?.host?.pings?.find(p => matchesPing(p, source)) : undefined
      const unavailable = source?.state === 'unsupported' || sample?.quality?.outcome === 'permission'
      const reason = reasonText(source?.reason) ?? reasonText(sample?.quality?.outcome)
      const empty = state.loading ? t('加载中') : state.error ? t('加载失败') : !historyShown ? t('未启用记录') : reason ?? t('无采样数据')
      const latency = !unavailable && state.hasData && Number.isFinite(state.avgLatency) ? `${Math.round(state.avgLatency)} ms` : '—'
      const loss = !unavailable && state.hasData && Number.isFinite(state.avgLoss) ? `${state.avgLoss.toFixed(1)}%` : '—'
      const familyLabel = family === 'ipv4' ? 'v4' : family === 'ipv6' ? 'v6' : ''
      const title = [label, familyLabel, source?.type?.replace('tcp_reachability', 'tcp').toUpperCase()].filter(Boolean).join(' · ')
      return { family, familyLabel, latencyDisplay: latency, lossDisplay: loss,
        latencyTooltip: `${title}: ${latency}${reason ? ` · ${reason}` : ''}`, lossTooltip: `${title}: ${loss}${reason ? ` · ${reason}` : ''}`,
        latencyBars: !unavailable && state.history.length ? buildHistoryBars(title, c.key, state.history, 'latency') : buildEmptyBars(c.key, 'latency', empty),
        lossBars: !unavailable && state.history.length ? buildHistoryBars(title, c.key, state.history, 'loss') : buildEmptyBars(c.key, 'loss', empty),
      }
    })
    const display = (metric: 'latency' | 'loss') => series.map(s => `${selection.dual ? `${s.familyLabel} ` : ''}${metric === 'latency' ? s.latencyDisplay : s.lossDisplay}`).join(' / ')
    return { key: c.key, label, dotClass: CARRIER_DOT_CLASSES[c.key], series,
      latencyDisplay: display('latency'), lossDisplay: display('loss'),
      latencyTooltip: series.map(s => s.latencyTooltip).join('\n'), lossTooltip: series.map(s => s.lossTooltip).join('\n'),
    }
  })
  return { carrierDisplays, dual: selection.dual }
}
