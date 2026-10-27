export type StaffRole = 'admin' | 'operator' | 'support'
export interface NavigationItem { to: string; labelKey: string; secondary?: boolean }
export interface NavigationGroup { id: string; labelKey: string; items: NavigationItem[] }
export const navigation: NavigationGroup[] = [
  { id: 'overview', labelKey: 'nav.dashboard', items: [{ to: '/', labelKey: 'nav.dashboard' }] },
  { id: 'users', labelKey: 'workspace.userManagement', items: [
    { to: '/users', labelKey: 'nav.users' }, { to: '/users?tab=groups', labelKey: 'users.groups' }, { to: '/users?tab=renewals', labelKey: 'users.viewRenewals', secondary: true },
  ] },
  { id: 'nodes', labelKey: 'workspace.nodeManagement', items: [
    { to: '/nodes', labelKey: 'nav.nodes' }, { to: '/infrastructure', labelKey: 'nav.infrastructure' }, { to: '/domains', labelKey: 'nav.domains', secondary: true },
  ] },
  { id: 'subscription', labelKey: 'workspace.subscription', items: [
    { to: '/entries', labelKey: 'nav.entries' }, { to: '/sub-templates?mode=profiles', labelKey: 'subProfiles.profiles' }, { to: '/sub-templates', labelKey: 'workspace.templates' }, { to: '/sub-templates?mode=rules', labelKey: 'subTemplates.modeRules' }, { to: '/external', labelKey: 'nav.external', secondary: true },
  ] },
  { id: 'monitoring', labelKey: 'workspace.monitoring', items: [
    { to: '/monitoring', labelKey: 'nav.monitoring' }, { to: '/speedtest', labelKey: 'nav.speedtest', secondary: true }, { to: '/monitoring?tab=alerts', labelKey: 'alerts.title' },
  ] },
  { id: 'business', labelKey: 'workspace.business', items: [
    { to: '/plans', labelKey: 'nav.plans' }, { to: '/orders', labelKey: 'nav.orders' }, { to: '/business/marketing', labelKey: 'workspace.marketing', secondary: true }, { to: '/tickets', labelKey: 'nav.tickets' }, { to: '/business/content', labelKey: 'workspace.content', secondary: true },
  ] },
]

// Preserve the existing role matrix when reorganizing settings.
// Hiding navigation is not authorization: the server remains authoritative.
export function canVisit(to: string, role: string) {
  const path = to.split('?')[0]
  if (role === 'admin') return true
  if (role === 'operator') return !['/settings', '/site', '/admins', '/sub-templates', '/infrastructure'].some(p => path === p || path.startsWith(`${p}/`))
  if (path === '/users' && ['groups', 'renewals'].includes(new URLSearchParams(to.split('?')[1]).get('tab') ?? '')) return false
  return ['/', '/users', '/orders', '/tickets', '/account'].includes(path)
}

export function navigationGroup(path: string) {
  if (path === '/settings' || path.startsWith('/settings/') || path === '/site') return 'system'
  if (path === '/account') return 'account'
  if (path.startsWith('/business/')) return 'business'
  if (['/coupons', '/gifts', '/withdrawals', '/articles'].includes(path)) return 'business'
  return navigation.find(g => g.items.some(i => i.to.split('?')[0] === path || (i.to !== '/' && path.startsWith(`${i.to}/`))))?.id ?? 'system'
}

export function isServerSettingsSection(section: string | null): section is 'routing' | 'ingresses' | 'overrides' {
  return section === 'routing' || section === 'ingresses' || section === 'overrides'
}
