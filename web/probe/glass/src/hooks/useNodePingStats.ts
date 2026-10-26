// Captain stores counted time buckets, not individual Komari ping events.
// Preserve the original 20-bar presentation without fabricating raw attempts.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { historyFor, snapshot } from '@shared/captain'
import type { NetworkHistory, PublicPingTask } from '@shared/captain'
import { selectPings, selectedHistory } from '@shared/ping-selection'
import { summarizePings } from '@shared/ping-summary'
import type { NodePingStatsState } from '@shared/ping-summary'
import { useBox } from './useBox'

const NO_HISTORY: NodePingStatsState['history'] = []

export function useNodePingStats(uuid: string, options?: { hours?: number, enabled?: boolean, maxCount?: number, source?: PublicPingTask | null }) {
  const snap = useBox(snapshot)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [state, setState] = useState<{ identity: string; value: NodePingStatsState } | null>(null)
  const epoch = useRef(0)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const source = options?.source !== undefined
    ? options.source
    : selectPings(snap?.nodes.find(n => String(n.id) === uuid), snap?.carrier_ping ?? false).first
  // Keying on a primitive identity prevents each snapshot poll restarting charts.
  const resolved = JSON.stringify({ id: uuid, hours: options?.hours ?? 1, enabled: options?.enabled !== false, source })

  const refresh = useCallback(async () => {
    const current = ++epoch.current
    const { id, hours, enabled, source: selected } = JSON.parse(resolved) as { id: string, hours: number, enabled: boolean, source: PublicPingTask | null }
    clearTimeout(timer.current)
    if (!enabled || !selected || selected.state === 'unsupported') {
      setState(null)
      setLoading(false)
      setError(null)
      return
    }
    setLoading(true)
    try {
      const history = await historyFor<NetworkHistory>(id, 'pings', hours)
      if (current === epoch.current) {
        setState({ identity: resolved, value: summarizePings(selectedHistory(history, selected), hours) })
        setError(null)
      }
    }
    catch (e) {
      if (current === epoch.current) {
        setError(String(e))
        setState(null)
      }
    }
    finally {
      if (current === epoch.current) {
        setLoading(false)
        timer.current = setTimeout(() => { void refresh() }, 60_000)
      }
    }
  }, [resolved])

  useEffect(() => {
    setState(null)
    void refresh()
    return () => {
      epoch.current++
      clearTimeout(timer.current)
    }
  }, [refresh])

  const parsedSource = useMemo(() => (JSON.parse(resolved) as { source: PublicPingTask | null }).source, [resolved])
  const currentState = state?.identity === resolved ? state.value : null
  return {
    loading,
    error,
    refresh,
    source: parsedSource,
    history: currentState?.history ?? NO_HISTORY,
    hasData: currentState?.hasData ?? false,
    avgLatency: currentState?.avgLatency ?? Number.NaN,
    avgLoss: currentState?.avgLoss ?? Number.NaN,
    avgVolatility: 0,
  }
}
