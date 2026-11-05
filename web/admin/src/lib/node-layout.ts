import { api } from './api'

export interface NodeLayout { revision: string; groups: string[]; group_names?: Record<string, { zh: string; en: string }>; nodes: { id: number; group: string }[] }
export const nodeLayoutQuery = { queryKey: ['node-layout'], queryFn: () => api.get<NodeLayout>('/api/admin/nodes/layout') }
