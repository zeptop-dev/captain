export const settingsCategories = ['site', 'account', 'subscription', 'business', 'proxy', 'monitoring', 'integrations', 'maintenance'] as const
export type SettingsCategory = typeof settingsCategories[number]
export interface SettingsEntry { id: string; category: SettingsCategory; titleKey: string; hintKey: string; keywords: string }

// Metadata only: no configuration values or credentials enter navigation/search.
export const settingsCatalog: SettingsEntry[] = [
  { id: 'site', category: 'site', titleKey: 'settingsLayout.siteEditor', hintKey: 'site.subtitle', keywords: 'brand logo favicon homepage portal font theme map FAQ branding 品牌 图标 首页 门户 字体 主题 地图' },
  { id: 'notice', category: 'site', titleKey: 'settings.notice', hintKey: 'settings.noticeHint', keywords: 'notice 公告' },
  { id: 'registration', category: 'account', titleKey: 'registration.title', hintKey: 'registration.hint', keywords: 'captcha turnstile hcaptcha register 注册' },
  { id: 'trial', category: 'account', titleKey: 'trial.title', hintKey: 'trial.hint', keywords: 'trial 试用' },
  { id: 'oidc', category: 'account', titleKey: 'settings.oidc', hintKey: 'settings.oidcHint', keywords: 'OIDC SSO password 登录 身份' },
  { id: 'access', category: 'account', titleKey: 'security.title', hintKey: 'security.hint', keywords: 'CIDR whitelist access 白名单' },
  { id: 'subscription', category: 'subscription', titleKey: 'settings.subUrls', hintKey: 'settings.subUrlsHint', keywords: 'HWID devices URLs short links 设备 短链接' },
  { id: 'clients', category: 'subscription', titleKey: 'clients.title', hintKey: 'clients.hint', keywords: 'client import 客户端' },
  { id: 'purchase', category: 'business', titleKey: 'settings.singlePlan', hintKey: 'settings.singlePlanHint', keywords: 'single plan purchase 套餐 购买 单套餐' },
  { id: 'surplus', category: 'business', titleKey: 'settings.surplus', hintKey: 'settings.surplusHint', keywords: 'plan purchase credit 折抵 套餐' },
  { id: 'invite', category: 'business', titleKey: 'settings.invite', hintKey: 'settings.inviteHint', keywords: 'invite commission rebate 返佣 邀请' },
  { id: 'acme', category: 'proxy', titleKey: 'settings.acme', hintKey: 'settings.acmeHint', keywords: 'TLS DNS ACME Cloudflare certificate 证书' },
  { id: 'connections', category: 'proxy', titleKey: 'connlog.title', hintKey: 'connlog.hint', keywords: 'connection log 连接记录' },
  { id: 'audit', category: 'proxy', titleKey: 'audit.title', hintKey: 'audit.hint', keywords: 'traffic rules audit 审计' },
  { id: 'limits', category: 'proxy', titleKey: 'dynlimit.title', hintKey: 'dynlimit.hint', keywords: 'speed throttle 限速' },
  { id: 'probe', category: 'monitoring', titleKey: 'settingsLayout.probeTitle', hintKey: 'probe.hint', keywords: 'status page appearance theme 采集 状态页 主题' },
  { id: 'service-status', category: 'monitoring', titleKey: 'settingsLayout.statusTitle', hintKey: 'serviceStatus.hint', keywords: 'service status public maintenance node health 服务状态 维护 节点健康' },
  { id: 'ping-tasks', category: 'monitoring', titleKey: 'probe.tasks', hintKey: 'probe.tasksHint', keywords: 'ping tcp http network 网络 探测' },
  { id: 'heartbeat', category: 'monitoring', titleKey: 'heartbeat.title', hintKey: 'heartbeat.hint', keywords: 'heartbeat uptime watchdog 心跳' },
  { id: 'mail', category: 'integrations', titleKey: 'mail.title', hintKey: 'mail.hint', keywords: 'SMTP Resend email 邮件' },
  { id: 'telegram', category: 'integrations', titleKey: 'telegram.title', hintKey: 'telegram.hint', keywords: 'telegram bot 通知 机器人' },
  { id: 'webhooks', category: 'integrations', titleKey: 'webhooks.title', hintKey: 'webhooks.hint', keywords: 'webhook events 通知 事件' },
  { id: 'login-clients', category: 'integrations', titleKey: 'loginClients.title', hintKey: 'loginClients.hint', keywords: 'New API OAuth Captain login 应用 登录' },
  { id: 'backup', category: 'maintenance', titleKey: 'backup.title', hintKey: 'workspace.backupHint', keywords: 'backup restore S3 WebDAV 备份 恢复' },
  { id: 'update', category: 'maintenance', titleKey: 'update.title', hintKey: 'workspace.updateHint', keywords: 'version upgrade 版本 更新' },
  { id: 'admin-log', category: 'maintenance', titleKey: 'adminlog.title', hintKey: 'adminlog.hint', keywords: 'staff administrator audit 操作日志' },
  { id: 'reset', category: 'maintenance', titleKey: 'siteReset.title', hintKey: 'siteReset.description', keywords: 'reset fresh install 重置 全新安装' },
]

export const legacySettingsAreas: Record<string, SettingsCategory> = {
  users: 'account', nodes: 'proxy', subscription: 'subscription', monitoring: 'monitoring',
  business: 'business', security: 'account', integrations: 'integrations', maintenance: 'maintenance',
}
export function settingsCategoryPath(category: SettingsCategory) { return `/settings/category/${category}` }
export function settingsEntryPath(entry: SettingsEntry) { return `${settingsCategoryPath(entry.category)}#${entry.id}` }

// Keep bookmarks and links from older versions working without a second settings hierarchy.
export function resolveSettingsRoute(section: string): { category: SettingsCategory; redirect?: string } | null {
  if (!section) return { category: 'site', redirect: settingsCategoryPath('site') }
  if (section.startsWith('category/')) {
    const category = section.slice(9) as SettingsCategory
    return settingsCategories.includes(category) ? { category } : null
  }
  const entry = settingsCatalog.find(item => item.id === section)
  if (entry) return { category: entry.category, redirect: settingsEntryPath(entry) }
  const category = section.startsWith('area/') ? legacySettingsAreas[section.slice(5)] : undefined
  return category ? { category, redirect: settingsCategoryPath(category) } : null
}

export function searchSettings(query: string, translate: (key: string) => string): SettingsEntry[] {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return []
  return settingsCatalog.filter(entry => {
    const text = `${translate(entry.titleKey)} ${translate(entry.hintKey)} ${translate(`settingsLayout.categories.${entry.category}`)} ${entry.keywords}`.toLocaleLowerCase()
    return words.every(word => text.includes(word))
  })
}
