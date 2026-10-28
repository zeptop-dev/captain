import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import { useAuth } from './auth'
import { serviceControlKey, serviceControlURL, type NodeServiceControl } from './node-service-control'

export function useNodeServiceControl(nodeID: number) {
  const { me } = useAuth()
  return useQuery({ queryKey: serviceControlKey(nodeID), queryFn: ({ signal }) => api.get<NodeServiceControl>(serviceControlURL(nodeID), signal), enabled: me?.role === 'admin', refetchInterval: 10_000 })
}
