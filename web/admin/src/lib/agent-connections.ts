import type { AgentHTTPSChoice } from './agent-https'
import { api } from './api'
export interface AgentEndpoint { id: string; name: string; enabled: boolean; url: string; listen: string; certificate_id: number; revision: string; default: boolean; users: number; unknown_users?: number; configured_users?: number; pending: number }
export interface EndpointStatus { state: string; last_error?: string; certificate_not_after?: string }
export interface EndpointView { entries: AgentEndpoint[]; statuses: Record<string, EndpointStatus> }
export interface ConnectionChoice { mode: 'default' | 'managed' | 'custom'; endpoint_id: string; url: string }
export interface AgentConnection { name?: string; paired: boolean; node_id: number; url: string; seen_at: number; capable: boolean; mode: string; endpoint_id: string; target_url: string; migration_id?: string; status: string; error?: string }
export interface Migration { name?: string; id: string; batch_id: string; node_id: number; source_url: string; target_url: string; status: string; error?: string }
export const connectionChoice = (): ConnectionChoice => ({ mode: 'default', endpoint_id: '', url: '' })
export async function endpointChoices(signal?: AbortSignal) { const start = { wall: Date.now(), mono: performance.now() }; const value = await api.get<AgentHTTPSChoice>('/api/admin/nodes/agent-https', signal); return { ...value, start } }
