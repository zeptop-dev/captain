export type StaffRole = 'admin' | 'operator' | 'support'
export interface NavigationItem { to: string; labelKey: string }
export interface NavigationGroup { id: string; labelKey: string; items: NavigationItem[] }

// The sidebar lists workspaces. Their destinations are exposed once, inside
// the workspace, using the existing URLs and permission matrix.
export const navigation: NavigationGroup[] = [
  { id: 'overview', labelKey: 'nav.dashboard', items: [{ to: '/', labelKey: 'nav.dashboard' }] },
  { id: 'users', labelKey: 'workspace.userManagement', items: [
    { to: '/users', labelKey: 'users.viewList' }, { to: '/users?tab=groups', labelKey: 'users.groups' }, { to: '/users?tab=renewals', labelKey: 'users.viewRenewals' },
  ] },
  { id: 'nodes', labelKey: 'nav.nodes', items: [
    { to: '/nodes', labelKey: 'workspace.serverList' }, { to: '/infrastructure', labelKey: 'nav.infrastructure' }, { to: '/domains', labelKey: 'nav.domains' },
  ] },
  { id: 'entries', labelKey: 'entryFlow.title', items: [
    { to: '/entries', labelKey: 'entryFlow.nodesTab' }, { to: '/entries?tab=lines', labelKey: 'lines.tab' }, { to: '/external', labelKey: 'nav.external' },
  ] },
  { id: 'subscription', labelKey: 'workspace.subscriptionManagement', items: [
    { to: '/sub-templates', labelKey: 'workspace.templateEditor' }, { to: '/sub-templates?mode=library', labelKey: 'subProfiles.library' }, { to: '/sub-templates?mode=profiles', labelKey: 'subProfiles.profiles' }, { to: '/sub-templates?mode=rules', labelKey: 'subTemplates.modeRules' },
  ] },
  { id: 'monitoring', labelKey: 'workspace.monitoring', items: [
    { to: '/monitoring', labelKey: 'nav.monitoring' }, { to: '/monitoring?tab=alerts', labelKey: 'alerts.title' }, { to: '/monitoring?tab=dns', labelKey: 'dnsHealth.title' }, { to: '/speedtest', labelKey: 'nav.speedtest' },
  ] },
  { id: 'business', labelKey: 'workspace.transactions', items: [
    { to: '/plans', labelKey: 'nav.plans' }, { to: '/orders', labelKey: 'nav.orders' }, { to: '/coupons', labelKey: 'nav.coupons' }, { to: '/gifts', labelKey: 'nav.gifts' }, { to: '/withdrawals', labelKey: 'nav.withdrawals' },
  ] },
  { id: 'support', labelKey: 'workspace.customerService', items: [
    { to: '/tickets', labelKey: 'nav.tickets' }, { to: '/articles', labelKey: 'nav.articles' },
  ] },
]

// Hiding navigation is not authorization: Protected and the server continue
// to enforce the existing role matrix independently.
export function canVisit(to: string, role: string) {
  const path = to.split('?')[0]
  if (role === 'admin') return true
  if (role === 'operator') return !['/settings', '/site', '/admins', '/sub-templates', '/infrastructure'].some(p => path === p || path.startsWith(`${p}/`))
  if (path === '/users' && ['groups', 'renewals'].includes(new URLSearchParams(to.split('?')[1]).get('tab') ?? '')) return false
  return ['/', '/users', '/orders', '/tickets', '/account'].includes(path)
}

export function visibleNavigation(role: string) {
  return navigation.map(group => ({ ...group, items: group.items.filter(item => canVisit(item.to, role)) })).filter(group => group.items.length > 0)
}

export function navigationGroup(path: string) {
  if (path === '/settings' || path.startsWith('/settings/') || path === '/site' || path === '/admins') return 'system'
  if (path === '/account') return 'account'
  if (path === '/business/content') return 'support'
  if (path.startsWith('/business/')) return 'business'
  return navigation.find(g => g.items.some(i => i.to.split('?')[0] === path || (i.to !== '/' && path.startsWith(`${i.to.split('?')[0]}/`))))?.id
}

export function activeNavigationItem(items: NavigationItem[], pathname: string, search: string) {
  const params = new URLSearchParams(search)
  return items.find(item => {
    const [path, query] = item.to.split('?')
    return path === pathname && !!query && [...new URLSearchParams(query)].every(([key, value]) => params.get(key) === value)
  }) ?? items.find(item => !item.to.includes('?') && (item.to === pathname || (item.to !== '/' && pathname.startsWith(`${item.to}/`))))
}

export function workspaceDestination(item: NavigationItem, current: { pathname: string; search: string; hash: string }) {
  const [path, query] = item.to.split('?')
  if (path !== current.pathname) return item.to
  const group = navigation.find(group => group.items.includes(item))
  if (group && activeNavigationItem(group.items, current.pathname, current.search) === item) return current.pathname + current.search + current.hash
  const params = new URLSearchParams(current.search)
  const target = new URLSearchParams(query)
  // Only change the workspace selector; retain filters and the editor format.
  const selector = path === '/sub-templates' ? 'mode' : 'tab'
  if (target.has(selector)) params.set(selector, target.get(selector)!)
  else params.delete(selector)
  if (path === '/entries') { params.delete('section'); params.delete('inbound'); params.delete('create') }
  const search = params.toString()
  return path + (search ? `?${search}` : '')
}

export function isServerSettingsSection(section: string | null): section is 'routing' | 'ingresses' | 'overrides' {
  return section === 'routing' || section === 'ingresses' || section === 'overrides'
}
