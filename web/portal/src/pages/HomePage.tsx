import { Alert, Anchor, Badge, Button, Card, Group, Progress, Select, Skeleton, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { IconArrowRight, IconArrowUpRight, IconBook2, IconCheck, IconChevronDown, IconChevronUp, IconCopy, IconDownload, IconHeadset, IconSpeakerphone, IconWorld } from '@tabler/icons-react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, type Sub } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { useAuth } from '../lib/auth'
import { useSite } from '../lib/theme'
import { detectDevicePlatform, importClients, subscriptionImportURL, type ImportClient } from '../lib/connection'
import { bytes, money } from '../lib/format'
import { toast } from '../lib/notify'
import classes from './HomePage.module.css'

function PlanUsage({ subscription: s, now, primary }: { subscription: Sub; now: number; primary: boolean }) {
  const { t, i18n } = useTranslation()
  const expiry = s.expires_at ? new Date(s.expires_at).getTime() : null
  const expired = expiry !== null && expiry <= now
  const exhausted = s.quota_bytes > 0 && s.used_bytes >= s.quota_bytes
  const usable = s.usable && !expired && !exhausted
  const days = expiry === null ? null : Math.max(0, Math.ceil((expiry - now) / 86400000))
  const expiring = usable && days !== null && days <= 7
  const color = !usable || expiring ? 'orange' : undefined
  const remaining = Math.max(0, s.quota_bytes - s.used_bytes)
  const [remainingValue, remainingUnit] = bytes(remaining).split(' ')
  const status = expired ? t('overview.expired') : exhausted ? t('overview.exhausted') : usable ? t('home.statusActive') : t('overview.unavailable')
  const date = (value: string) => new Date(value).toLocaleDateString(i18n.language, { year: 'numeric', month: 'short', day: 'numeric' })
  return (
    <article className={`${classes.plan} ${primary ? classes.primaryPlan : classes.additionalPlan}`}>
      <Group justify="space-between" align="flex-start" gap="sm">
        <Title order={3} className={classes.planName}>{s.plan_name}</Title>
        <Badge variant="light" color={color} radius={3}>{status}</Badge>
      </Group>
      <div className={classes.usage}>
        <Text size="xs" c="dimmed">{t('overview.remaining')}</Text>
        <div className={classes.amount}>{s.quota_bytes > 0 ? <>{remainingValue} <span>{remainingUnit}</span></> : <span className={classes.unlimited}>{t('home.unlimited')}</span>}</div>
        {s.quota_bytes > 0 && <Progress value={Math.min(100, Math.max(0, remaining / s.quota_bytes * 100))} color={exhausted ? 'orange' : undefined} size={4} radius={0} aria-label={t('overview.remaining')} />}
      </div>
      <div className={classes.planDetails}>
        <div>
          <Text size="xs" c="dimmed" mb={6}>{t('home.usage')}</Text>
          <Text size="sm">{s.quota_bytes > 0 ? t('overview.usedOf', { used: bytes(s.used_bytes), total: bytes(s.quota_bytes) }) : t('overview.usedUnlimited', { used: bytes(s.used_bytes) })}</Text>
        </div>
        <div>
          <Text size="xs" c="dimmed" mb={6}>{t('overview.reset')}</Text>
          <Text size="sm">{s.reset_at ? date(s.reset_at) : t('overview.noReset')}</Text>
        </div>
        <div>
          <Text size="xs" c="dimmed" mb={6}>{t('home.expires')}</Text>
          <Text size="sm">{s.expires_at ? date(s.expires_at) : t('home.never')}</Text>
          {usable && days !== null && <Text size="xs" mt={4} c={expiring ? 'orange' : 'dimmed'}>{t('home.daysLeft', { count: days })}</Text>}
        </div>
      </div>
      <Group justify="space-between" gap="xs" className={classes.renewRow}>
        {(!usable || expiring) && <Text size="sm" c="dimmed">{expired ? t('overview.expiredHint') : exhausted ? t('overview.exhaustedHint') : expiring ? t('overview.expiringHint') : t('overview.unavailableHint')}</Text>}
        <Button component={Link} to="/plans" variant="subtle" size="compact-sm" color={!usable || expiring ? 'orange' : undefined} rightSection={<IconArrowUpRight size={15} />}>{t('home.renew')}</Button>
      </Group>
    </article>
  )
}

export default function HomePage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const site = useSite()
  const [client, setClient] = useState<ImportClient>(() => {
    const platform = detectDevicePlatform(navigator.userAgent, navigator.maxTouchPoints)
    return platform === 'ios' ? 'shadowrocket' : platform === 'other' ? 'manual' : 'clash'
  })
  const [now, setNow] = useState(() => Date.now())
  const [copied, setCopied] = useState(false)
  const [expanded, setExpanded] = useState(false)
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 60_000); return () => window.clearInterval(timer) }, [])
  useEffect(() => { if (!copied) return; const timer = window.setTimeout(() => setCopied(false), 1800); return () => window.clearTimeout(timer) }, [copied])
  const notice = useQuery({ queryKey: ['notice'], queryFn: () => api.get<{ enabled: boolean; title?: string; body?: string }>('/api/portal/notice'), meta: { handlesError: true } })
  const cancelQueued = useMutation({ mutationFn: (id: number) => api.del(`/api/portal/subscriptions/${id}`), onSuccess: () => refresh(), onError: toast.err })
  if (!me) return null
  const subs = me.subscriptions ?? []
  const active = subs.filter((s) => s.status !== 'queued')
  const queued = subs.filter((s) => s.status === 'queued')
  const anyUsable = active.some((s) => s.usable && (!s.expires_at || new Date(s.expires_at).getTime() > now) && (s.quota_bytes <= 0 || s.used_bytes < s.quota_bytes))
  const showNotice = notice.isPending || notice.isError || notice.data?.enabled
  const selectedClient = importClients.find(choice => choice.value === client)
  const importURL = subscriptionImportURL(me.subscription_url, client, site.data?.name)
  return (
    <Stack gap={24}>
      <Group justify="space-between" align="center" gap="md" className={classes.welcome}>
        <div><Text className={classes.eyebrow}>{t('portalNav.overview')}</Text><Title order={1} className={classes.heading}>{t('overview.greeting')}</Title><Text size="sm" c="dimmed" mt={10}>{t('overview.intro')}</Text></div>
        <Button component={Link} to="/connect" rightSection={<IconArrowRight size={16} />}>{t('connection.title')}</Button>
      </Group>
      <div className={classes.mainGrid}>
        <section className={classes.plansSection}>
          <Group justify="space-between" gap="xs" mb={20}><Title order={2} className={classes.sectionTitle}><span className={classes.sectionNumber}>01</span>{t('plans.mine')}</Title><Anchor component={Link} to="/plans" size="xs" underline="hover">{t('overview.managePlans')} <IconArrowUpRight size={14} style={{ verticalAlign: 'middle' }} /></Anchor></Group>
          {active.length > 0 ? <Stack gap={0}>{active.map((s, index) => <PlanUsage key={s.id} subscription={s} now={now} primary={index === 0} />)}</Stack> : <div className={classes.emptyPlan}><IconWorld size={30} stroke={1.3} aria-hidden /><Title order={3} size="h3" mt="lg">{queued.length ? t('overview.queueOnly') : t('overview.emptyTitle')}</Title><Text c="dimmed" size="sm" mt="xs" mb="lg">{queued.length ? t('overview.queueOnlyHint') : t('home.noPlanHint')}</Text><Button component={Link} to="/plans" rightSection={<IconArrowRight size={16} />}>{t('home.browsePlans')}</Button></div>}
          {queued.length > 0 && <details className={classes.queued}><summary><span>{t('overview.queued')} <Text span size="xs" c="dimmed">({queued.length})</Text></span><IconChevronDown size={15} aria-hidden /></summary><Stack gap={0}>{queued.map((s) => <div key={s.id} className={classes.queueItem}><Group justify="space-between" gap="xs"><Text fw={500} size="sm">{s.plan_name}</Text><Badge variant="light" color="gray" size="sm" radius={3}>{t('home.statusQueued')}</Badge></Group><Text size="xs" c="dimmed" mt={6}>{s.period_days ? t('home.queuedHint', { days: s.period_days }) : t('home.queuedHintForever')}</Text><Group justify="space-between" gap="xs" mt={8}><Text size="xs" c="dimmed">{s.quota_bytes ? t('home.queuedQuota', { quota: bytes(s.quota_bytes) }) : t('home.unlimited')}</Text><Button size="compact-xs" variant="subtle" color="gray" disabled={cancelQueued.isPending} loading={cancelQueued.isPending && cancelQueued.variables === s.id} onClick={() => cancelQueued.mutate(s.id)}>{t('home.cancelQueued')}</Button></Group></div>)}</Stack></details>}
        </section>
        <aside className={classes.connectCard}>
          <Title order={2} className={classes.sectionTitle}><span className={classes.sectionNumber}>02</span>{t('overview.connect')}</Title>
          <Text c="dimmed" size="sm" mt={18}>{t('overview.connectHint')}</Text>
          <Stack gap={12} mt={24}>
            <Select label={t('connection.installedClient')} value={client} onChange={value => value && setClient(value as ImportClient)} data={[...importClients.map(choice => ({ value: choice.value, label: choice.label })), { value: 'manual', label: t('connection.otherClient') }]} allowDeselect={false} />
            {importURL ? <Button component="a" href={importURL} fullWidth rightSection={<IconArrowUpRight size={17} />}>{t('connection.importInto', { name: selectedClient?.label })}</Button> : <Button component={Link} to="/connect" fullWidth rightSection={<IconArrowRight size={17} />}>{t('overview.configure')}</Button>}
            <Button variant="default" fullWidth disabled={!me.subscription_url} leftSection={copied ? <IconCheck size={17} /> : <IconCopy size={17} />} onClick={async () => { if (await copyText(me.subscription_url)) setCopied(true) }}>{copied ? t('home.copied') : t('overview.copySubscription')}</Button>
          </Stack>
          {!anyUsable && <Text size="xs" c="dimmed" mt="sm">{t('overview.connectInactive')}</Text>}
          <div className={classes.connectLinks}><Anchor component={Link} to="/connect" className={classes.connectLink} underline="never"><Group gap={8}><IconDownload size={16} stroke={1.5} /><Text span size="xs">{t('overview.clientDownloads')}</Text></Group><IconArrowUpRight size={15} /></Anchor><Anchor component={Link} to="/connect#nodes" className={classes.connectLink} underline="never"><Group gap={8}><IconWorld size={16} stroke={1.5} /><Text span size="xs">{t('overview.browseNodes')}</Text></Group><IconArrowUpRight size={15} /></Anchor></div>
          <div className={classes.accountSummary}>
            <Group justify="space-between" gap="sm"><Text size="sm" c="dimmed">{t('home.balance')}</Text><Anchor component={Link} to="/account" size="sm" underline="hover">{money(me.balance_cents)}</Anchor></Group>
            <Text size="xs" c="dimmed" mt={14}>{t('overview.onlineRecent', { count: me.online_devices ?? me.subscription?.online_devices ?? 0 })}</Text>
          </div>
        </aside>
      </div>
      <div className={`${classes.secondaryGrid} ${!showNotice ? classes.secondarySolo : ''}`}>
        {showNotice && <Card className={classes.noticeCard}><Group gap={9} mb="md"><IconSpeakerphone size={19} stroke={1.7} /><Title order={2} size="h4">{t('overview.notice')}</Title></Group>{notice.isPending ? <Stack gap="xs"><Skeleton height={18} width="65%" /><Skeleton height={14} /><Skeleton height={14} width="82%" /></Stack> : notice.isError ? <Alert color="orange"><Text size="sm">{t('overview.noticeError')}</Text><Button variant="subtle" size="compact-sm" mt={6} onClick={() => notice.refetch()}>{t('auth.retry')}</Button></Alert> : <><Text fw={500} size="sm" mb={8}>{notice.data?.title}</Text><Text size="sm" c="dimmed" lineClamp={expanded ? undefined : 3} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{notice.data?.body}</Text>{notice.data?.body && <Button variant="subtle" size="compact-sm" mt="md" px={0} rightSection={expanded ? <IconChevronUp size={14} /> : <IconChevronDown size={14} />} aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>{t(expanded ? 'overview.noticeCollapse' : 'overview.noticeRead')}</Button>}</>}</Card>}
        <Card className={classes.helpCard}><Title order={2} size="h4" mb="md">{t('overview.help')}</Title><Anchor component={Link} to="/help" className={classes.helpLink} underline="never"><ThemeIcon variant="transparent" color="gray" size={30}><IconBook2 size={20} stroke={1.7} /></ThemeIcon><div><Text fw={500} size="sm">{t('overview.tutorial')}</Text><Text size="xs" c="dimmed" mt={3}>{t('overview.helpHint')}</Text></div><IconArrowUpRight size={16} /></Anchor><Anchor component={Link} to="/tickets" className={classes.helpLink} underline="never"><ThemeIcon variant="transparent" color="gray" size={30}><IconHeadset size={20} stroke={1.7} /></ThemeIcon><div><Text fw={500} size="sm">{t('overview.support')}</Text><Text size="xs" c="dimmed" mt={3}>{t('overview.supportHint')}</Text></div><IconArrowUpRight size={16} /></Anchor></Card>
      </div>
    </Stack>
  )
}
