export type SettingsArea = 'users' | 'nodes' | 'subscription' | 'monitoring' | 'business' | 'security' | 'integrations' | 'maintenance'
export const settingsAreas: SettingsArea[] = ['users', 'nodes', 'subscription', 'monitoring', 'business', 'security', 'integrations', 'maintenance']
export const systemAreas: SettingsArea[] = ['security', 'integrations', 'maintenance']
export interface SettingsEntry { id: string; area: SettingsArea; titleKey: string; hintKey: string; keywords: string }

// Metadata only: no configuration values or credentials enter navigation/search.
export const settingsCatalog: SettingsEntry[] = [
  { id: 'registration', area: 'users', titleKey: 'registration.title', hintKey: 'registration.hint', keywords: 'captcha turnstile hcaptcha register 注册' },
  { id: 'trial', area: 'users', titleKey: 'trial.title', hintKey: 'trial.hint', keywords: 'trial 试用' },
  { id: 'acme', area: 'nodes', titleKey: 'settings.acme', hintKey: 'settings.acmeHint', keywords: 'TLS DNS ACME Cloudflare certificate 证书' },
  { id: 'connections', area: 'nodes', titleKey: 'connlog.title', hintKey: 'connlog.hint', keywords: 'connection log 连接记录' },
  { id: 'audit', area: 'nodes', titleKey: 'audit.title', hintKey: 'audit.hint', keywords: 'traffic rules audit 审计' },
  { id: 'limits', area: 'nodes', titleKey: 'dynlimit.title', hintKey: 'dynlimit.hint', keywords: 'speed throttle 限速' },
  { id: 'subscription', area: 'subscription', titleKey: 'settings.subUrls', hintKey: 'settings.subUrlsHint', keywords: 'single plan purchase HWID devices URLs short links 设备 短链接' },
  { id: 'clients', area: 'subscription', titleKey: 'clients.title', hintKey: 'clients.hint', keywords: 'client import 客户端' },
  { id: 'probe', area: 'monitoring', titleKey: 'probe.title', hintKey: 'probe.hint', keywords: 'status page appearance theme 采集 状态页 主题' },
  { id: 'ping-tasks', area: 'monitoring', titleKey: 'probe.tasks', hintKey: 'probe.tasksHint', keywords: 'ping tcp http network 网络 探测' },
  { id: 'heartbeat', area: 'monitoring', titleKey: 'heartbeat.title', hintKey: 'heartbeat.hint', keywords: 'heartbeat uptime watchdog 心跳' },
  { id: 'notice', area: 'business', titleKey: 'settings.notice', hintKey: 'settings.noticeHint', keywords: 'notice 公告' },
  { id: 'invite', area: 'business', titleKey: 'settings.invite', hintKey: 'settings.inviteHint', keywords: 'invite commission rebate 返佣 邀请' },
  { id: 'surplus', area: 'business', titleKey: 'settings.surplus', hintKey: 'settings.surplusHint', keywords: 'plan purchase credit 折抵 套餐' },
  { id: 'access', area: 'security', titleKey: 'security.title', hintKey: 'security.hint', keywords: 'CIDR whitelist access 白名单' },
  { id: 'login-clients', area: 'integrations', titleKey: 'loginClients.title', hintKey: 'loginClients.hint', keywords: 'New API OAuth Captain login 应用 登录' },
  { id: 'oidc', area: 'security', titleKey: 'settings.oidc', hintKey: 'settings.oidcHint', keywords: 'OIDC SSO password 登录 身份' },
  { id: 'admin-log', area: 'security', titleKey: 'adminlog.title', hintKey: 'adminlog.hint', keywords: 'staff administrator audit 操作日志' },
  { id: 'mail', area: 'integrations', titleKey: 'mail.title', hintKey: 'mail.hint', keywords: 'SMTP Resend email 邮件' },
  { id: 'telegram', area: 'integrations', titleKey: 'telegram.title', hintKey: 'telegram.hint', keywords: 'telegram bot 通知 机器人' },
  { id: 'webhooks', area: 'integrations', titleKey: 'webhooks.title', hintKey: 'webhooks.hint', keywords: 'webhook events 通知 事件' },
  { id: 'backup', area: 'maintenance', titleKey: 'backup.title', hintKey: 'workspace.backupHint', keywords: 'backup restore S3 WebDAV 备份 恢复' },
  { id: 'update', area: 'maintenance', titleKey: 'update.title', hintKey: 'workspace.updateHint', keywords: 'version upgrade 版本 更新' },
  { id: 'reset', area: 'maintenance', titleKey: 'siteReset.title', hintKey: 'siteReset.description', keywords: 'reset fresh install 重置 全新安装' },
]
