import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from './api'
import type { DNSHealth } from './dns-health'

export function useDNSHealth(nodeID?: number, enabled = true) {
  return useQuery({
    queryKey: ['dns-health', nodeID], enabled,
    queryFn: async ({ signal }) => nodeID
      ? [await api.get<DNSHealth>(`/api/admin/nodes/${nodeID}/dns-health`, signal)]
      : api.get<DNSHealth[]>('/api/admin/monitoring/dns', signal),
    refetchInterval: 30_000,
  })
}

// Age the visible result even when polling fails or the server stops answering.
export function useDNSClock() {
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now() / 1000), 1000); return () => window.clearInterval(timer) }, [])
  return now
}
