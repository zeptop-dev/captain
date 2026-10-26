import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { useAuth } from './auth'
import type { NodeAccessDirectory } from './node-access'

export function useNodeAccess(enabled = true) {
  const { me } = useAuth()
  return useQuery({ queryKey: ['node-access'], queryFn: ({ signal }) => api.get<NodeAccessDirectory>('/api/admin/node-access', signal), enabled: enabled && me?.role === 'admin', staleTime: 15_000 })
}
export function useInvalidateNodeAccess() {
  const qc = useQueryClient()
  return () => { for (const key of ['node-access', 'entries', 'plans', 'node', 'reverse', 'wireguard']) void qc.invalidateQueries({ queryKey: [key] }) }
}
