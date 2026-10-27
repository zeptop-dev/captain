import type { CertStatus, DoctorReport, Inbound, Ingress, Node } from './api'
import type { CoreInventory } from '../components/CoreManagementCard'
import type { ResourceHost } from './resources'

export interface NodeDetail {
  traffic?: Record<string, { today: number; total: number }>
  node: Node
  inbounds: Inbound[]
  ingresses?: Ingress[]
  status: { core_inventory?: CoreInventory | null; host: ResourceHost | null; cores: Record<string, { running: boolean; inbounds?: string[] }> | null; certs: CertStatus[] | null; doctor?: DoctorReport | null } | null
}
export const serverTabs = ['overview', 'information', 'diagnostics', 'settings'] as const
export type ServerTab = typeof serverTabs[number]
export const serverSettings = ['proxy', 'ingresses', 'routing', 'overrides', 'runtime'] as const
export type ServerSetting = typeof serverSettings[number]
export function serverDetailLocation(id: string, pathname: string, search: string, hash: string) {
  const base = `/nodes/${id}`
  const suffix = pathname.slice(base.length).replace(/^\//, '')
  const [tab, section] = suffix.split('/')
  const params = new URLSearchParams(search)
  const legacy = params.get('section')
  if (!suffix && legacy && ['ingresses', 'routing', 'overrides'].includes(legacy)) return { tab: 'settings' as ServerTab, section: legacy as ServerSetting, redirect: `${base}/settings/${legacy}` }
  if (!suffix && hash === '#diagnostics') return { tab: 'diagnostics' as ServerTab, section: 'proxy' as ServerSetting, redirect: `${base}/diagnostics?view=network` }
  if (!suffix && hash === '#billing-link') return { tab: 'information' as ServerTab, section: 'proxy' as ServerSetting, redirect: `${base}/information#billing-link` }
  if (suffix && !serverTabs.some(t => t === tab)) return { tab: 'overview' as ServerTab, section: 'proxy' as ServerSetting, redirect: base }
  if (tab === 'settings' && !serverSettings.some(s => s === section)) return { tab: 'settings' as ServerTab, section: 'proxy' as ServerSetting, redirect: `${base}/settings/proxy` }
  return { tab: (tab || 'overview') as ServerTab, section: (section || 'proxy') as ServerSetting, redirect: null }
}
