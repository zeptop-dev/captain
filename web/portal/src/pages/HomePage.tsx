import { Alert, Anchor, Badge, Button, Card, Group, Progress, Skeleton, Stack, Text, ThemeIcon, Title } from '@mantine/core'
import { useMutation, useQuery } from '@tanstack/react-query'
import { IconArrowRight, IconArrowUpRight, IconBook2, IconCalendar, IconCheck, IconChevronDown, IconChevronUp, IconCopy, IconDownload, IconHeadset, IconLayersSubtract, IconNetwork, IconRefresh, IconSpeakerphone, IconWallet, IconWorld } from '@tabler/icons-react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, type Sub } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { useAuth } from '../lib/auth'
import { bytes, money } from '../lib/format'
import { toast } from '../lib/notify'
import classes from './HomePage.module.css'

function PlanUsage({ subscription: s, now }: { subscription: Sub; now: number }) {
  const { t, i18n } = useTranslation()
  const expiry = s.expires_at ? new Date(s.expires_at).getTime() : null
  const expired = expiry !== null && expiry <= now
  const exhausted = s.quota_bytes > 0 && s.used_bytes >= s.quota_bytes
  const usable = s.usable && !expired && !exhausted
  const days = expiry === null ? null : Math.max(0, Math.ceil((expiry - now) / 86400000))
  const expiring = usable && days !== null && days <= 7
  const color = !usable || expiring ? 'orange' : 'teal'
  const status = expired ? t('overview.expired') : exhausted ? t('overview.exhausted') : usable ? t('home.statusActive') : t('overview.unavailable')
  const date = (value: string) => new Date(value).toLocaleDateString(i18n.language, { year: 'numeric', month: 'short', day: 'numeric' })
  return (
    <article className={classes.plan}>
      <Group justify="space-between" align="flex-start" gap="sm">
        <Text fw={650} size="lg" className={classes.planName}>{s.plan_name}</Text>
        <Badge variant="light" color={color}>{status}</Badge>
      </Group>
      <div className={classes.usage}>
        <Text size="sm" c="dimmed">{t('overview.remaining')}</Text>
        <Text className={classes.amount}>{s.quota_bytes > 0 ? bytes(Math.max(0, s.quota_bytes - s.used_bytes)) : t('home.unlimited')}</Text>
        <Group justify="space-between" gap={4} mb={10}>
          <Text size="sm" c="dimmed">{s.quota_bytes > 0 ? t('overview.usedOf', { used: bytes(s.used_bytes), total: bytes(s.quota_bytes) }) : t('overview.usedUnlimited', { used: bytes(s.used_bytes) })}</Text>
          {s.quota_bytes > 0 && <Text size="sm" fw={600} c={exhausted ? 'orange' : 'dimmed'}>{Math.min(100, Math.max(0, s.used_bytes / s.quota_bytes * 100)).toFixed(0)}%</Text>}
        </Group>
        {s.quota_bytes > 0 && <Progress value={Math.min(100, Math.max(0, s.used_bytes / s.quota_bytes * 100))} color={exhausted ? 'orange' : undefined} size={7} radius="xl" aria-label={t('home.usage')} />}
      </div>
      <div className={classes.planDetails}>
        <div>
          <Group gap={6} c="dimmed" mb={5}><IconRefresh size={15} stroke={1.7} /><Text size="xs">{t('overview.reset')}</Text></Group>
          <Text size="sm" fw={550}>{s.reset_at ? date(s.reset_at) : t('overview.noReset')}</Text>
        </div>
        <div>
          <Group gap={6} c="dimmed" mb={5}><IconCalendar size={15} stroke={1.7} /><Text size="xs">{t('home.expires')}</Text></Group>
          <Group gap={6}><Text size="sm" fw={550}>{s.expires_at ? date(s.expires_at) : t('home.never')}</Text>{usable && days !== null && <Text size="xs" c={expiring ? 'orange' : 'dimmed'}>{t('home.daysLeft', { count: days })}</Text>}</Group>
        </div>
      </div>
      {(!usable || expiring) && <Group justify="space-between" gap="xs" className={classes.renewRow}><Text size="sm" c="dimmed">{expired ? t('overview.expiredHint') : exhausted ? t('overview.exhaustedHint') : expiring ? t('overview.expiringHint') : t('overview.unavailableHint')}</Text><Button component={Link} to="/plans" variant="light" size="xs" color="orange">{t('home.renew')}</Button></Group>}
    </article>
  )
}

export default function HomePage() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
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
  return (
    <Stack gap={24}>
      <Group justify="space-between" align="center" gap="md" className={classes.welcome}>
        <div><Title order={1} className={classes.heading}>{t('overview.greeting')}</Title><Text size="sm" c="dimmed" mt={5}>{t('overview.intro')}</Text></div>
        <Anchor component={Link} to="/account" className={classes.balance} underline="never"><IconWallet size={19} stroke={1.7} /><span>{t('home.balance')}</span><Text span fw={650}>{money(me.balance_cents)}</Text><IconArrowUpRight size={15} /></Anchor>
      </Group>
      <div className={classes.mainGrid}>
        <Card className={classes.plansCard}>
          <Group justify="space-between" gap="xs" mb={22}><Group gap={10}><ThemeIcon variant="light" size={34} radius="md"><IconLayersSubtract size={19} stroke={1.7} /></ThemeIcon><Title order={2} size="h4">{t('plans.mine')}</Title></Group><Anchor component={Link} to="/plans" size="sm" underline="hover">{t('overview.managePlans')} <IconArrowUpRight size={14} style={{ verticalAlign: 'middle' }} /></Anchor></Group>
          {active.length > 0 ? <Stack gap={0}>{active.map((s) => <PlanUsage key={s.id} subscription={s} now={now} />)}</Stack> : <div className={classes.emptyPlan}><ThemeIcon variant="light" size={54} radius="xl"><IconWorld size={28} stroke={1.5} /></ThemeIcon><Title order={3} size="h3" mt="lg">{queued.length ? t('overview.queueOnly') : t('overview.emptyTitle')}</Title><Text c="dimmed" size="sm" mt="xs" mb="lg">{queued.length ? t('overview.queueOnlyHint') : t('home.noPlanHint')}</Text><Button component={Link} to="/plans" rightSection={<IconArrowRight size={16} />}>{t('home.browsePlans')}</Button></div>}
          {queued.length > 0 && <div className={classes.queued}><Text size="sm" fw={650} mb="sm">{t('overview.queued')}</Text><Stack gap="sm">{queued.map((s) => <div key={s.id} className={classes.queueItem}><Group justify="space-between" gap="xs"><Text fw={600} size="sm">{s.plan_name}</Text><Badge variant="light" color="gray" size="sm">{t('home.statusQueued')}</Badge></Group><Text size="xs" c="dimmed" mt={6}>{s.period_days ? t('home.queuedHint', { days: s.period_days }) : t('home.queuedHintForever')}</Text><Group justify="space-between" gap="xs" mt={6}><Text size="xs" c="dimmed">{s.quota_bytes ? t('home.queuedQuota', { quota: bytes(s.quota_bytes) }) : t('home.unlimited')}</Text><Button size="compact-xs" variant="subtle" color="gray" disabled={cancelQueued.isPending} loading={cancelQueued.isPending && cancelQueued.variables === s.id} onClick={() => cancelQueued.mutate(s.id)}>{t('home.cancelQueued')}</Button></Group></div>)}</Stack></div>}
        </Card>
        <Card className={classes.connectCard}>
          <div className={classes.connectTop}><ThemeIcon size={48} radius="lg" variant="light"><IconNetwork size={27} stroke={1.5} /></ThemeIcon><Title order={2} mt={22} size="h3">{t('overview.connect')}</Title><Text c="dimmed" size="sm" mt="xs">{t('overview.connectHint')}</Text></div>
          <Stack gap={10} mt={28}>
            <Button component={Link} to="/connect" size="md" fullWidth rightSection={<IconArrowRight size={17} />}>{t('overview.configure')}</Button>
            <Button size="md" variant="default" fullWidth disabled={!me.subscription_url} leftSection={copied ? <IconCheck size={17} /> : <IconCopy size={17} />} onClick={async () => { if (await copyText(me.subscription_url)) setCopied(true) }}>{copied ? t('home.copied') : t('overview.copySubscription')}</Button>
          </Stack>
          {!anyUsable && <Text size="xs" c="dimmed" mt="sm">{t('overview.connectInactive')}</Text>}
          <div className={classes.connectLinks}><Anchor component={Link} to="/connect" className={classes.connectLink} underline="never"><Group gap={8}><IconDownload size={17} stroke={1.7} /><Text span size="sm">{t('overview.clientDownloads')}</Text></Group><IconArrowUpRight size={15} /></Anchor><Anchor component={Link} to="/connect#nodes" className={classes.connectLink} underline="never"><Group gap={8}><IconWorld size={17} stroke={1.7} /><Text span size="sm">{t('overview.browseNodes')}</Text></Group><IconArrowUpRight size={15} /></Anchor></div>
          <Text size="xs" c="dimmed" className={classes.recent}>{t('overview.onlineRecent', { count: me.online_devices ?? me.subscription?.online_devices ?? 0 })}</Text>
        </Card>
      </div>
      <div className={`${classes.secondaryGrid} ${!showNotice ? classes.secondarySolo : ''}`}>
        {showNotice && <Card className={classes.noticeCard}><Group gap={9} mb="md"><IconSpeakerphone size={19} stroke={1.7} /><Title order={2} size="h4">{t('overview.notice')}</Title></Group>{notice.isPending ? <Stack gap="xs"><Skeleton height={18} width="65%" /><Skeleton height={14} /><Skeleton height={14} width="82%" /></Stack> : notice.isError ? <Alert color="orange"><Text size="sm">{t('overview.noticeError')}</Text><Button variant="subtle" size="compact-sm" mt={6} onClick={() => notice.refetch()}>{t('auth.retry')}</Button></Alert> : <><Text fw={600} size="sm" mb={8}>{notice.data?.title}</Text><Text size="sm" c="dimmed" lineClamp={expanded ? undefined : 3} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{notice.data?.body}</Text>{notice.data?.body && <Button variant="subtle" size="compact-sm" mt="md" px={0} rightSection={expanded ? <IconChevronUp size={14} /> : <IconChevronDown size={14} />} aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>{t(expanded ? 'overview.noticeCollapse' : 'overview.noticeRead')}</Button>}</>}</Card>}
        <Card className={classes.helpCard}><Title order={2} size="h4" mb="md">{t('overview.help')}</Title><Anchor component={Link} to="/help" className={classes.helpLink} underline="never"><ThemeIcon variant="light" color="gray" size={38} radius="md"><IconBook2 size={20} stroke={1.7} /></ThemeIcon><div><Text fw={600} size="sm">{t('overview.tutorial')}</Text><Text size="xs" c="dimmed" mt={3}>{t('overview.helpHint')}</Text></div><IconArrowUpRight size={16} /></Anchor><Anchor component={Link} to="/tickets" className={classes.helpLink} underline="never"><ThemeIcon variant="light" color="gray" size={38} radius="md"><IconHeadset size={20} stroke={1.7} /></ThemeIcon><div><Text fw={600} size="sm">{t('overview.support')}</Text><Text size="xs" c="dimmed" mt={3}>{t('overview.supportHint')}</Text></div><IconArrowUpRight size={16} /></Anchor></Card>
      </div>
    </Stack>
  )
}
