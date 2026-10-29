import { Anchor, Badge, Box, Button, Card, Group, Stack, Text, ThemeIcon } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { IconAlertTriangle, IconCircleCheck, IconRefresh } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { bytes, when } from '../lib/format'
import classes from './SelfCheckCard.module.css'

interface Retention { last_run?: string; last_success?: string; rows: number; duration_ms: number; backlog: string }
interface Check { id: string; status: 'ok' | 'warn' | 'fail' | 'skip'; code?: string; args?: Record<string, string | number> }

// Where each finding is fixed in the console.
const fixAt: Record<string, string> = {
  backup_local: '/settings/backup', backup_remote: '/settings/backup', backup_encrypt: '/settings/backup', heartbeat: '/settings/heartbeat',
  mail: '/settings/mail', staff_2fa: '/admins', version: '/settings/update', nodes: '/nodes', dns: '/monitoring?tab=dns',
}

// Panel self-check on the dashboard (admins only): the panel's own gaps —
// backups that stay on this host, nothing watching the panel, staff
// without 2FA, disk, version, certificate. Only findings are listed.
export function SelfCheckCard() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const admin = (me?.role ?? 'admin') === 'admin'
  const q = useQuery({ queryKey: ['selfcheck'], queryFn: () => api.get<{ checks: Check[]; retention?: Retention }>('/api/admin/system/selfcheck'), enabled: admin, staleTime: 5 * 60_000, retry: false })
  if (!admin || !q.data) return null
  const issues = q.data.checks.filter((c) => c.status === 'warn' || c.status === 'fail')
  const passed = q.data.checks.filter((c) => c.status === 'ok').length
  const failing = issues.some((c) => c.status === 'fail')
  const args = (c: Check) => {
    const a: Record<string, string | number> = { ...c.args }
    if (typeof a.at === 'string') a.at = when(a.at)
    if (typeof a.free === 'number') a.free = bytes(a.free)
    return a
  }
  return (
    <Card mb="lg">
      <Group justify="space-between" mb={issues.length > 0 ? 'sm' : 0} wrap="nowrap">
        <Group gap="sm" wrap="nowrap">
          <ThemeIcon variant="light" color={failing ? 'red' : issues.length > 0 ? 'orange' : 'teal'} radius="xl">
            {issues.length > 0 ? <IconAlertTriangle size={16} /> : <IconCircleCheck size={16} />}
          </ThemeIcon>
          <div>
            <Text fw={600}>{t('selfcheck.title')}</Text>
            <Text size="xs" c="dimmed">{issues.length > 0 ? t('selfcheck.issues', { count: issues.length }) : t('selfcheck.allOk', { count: passed })}</Text>
          </div>
        </Group>
        <Button variant="subtle" size="xs" leftSection={<IconRefresh size={14} />} loading={q.isFetching} onClick={() => q.refetch()}>{t('selfcheck.recheck')}</Button>
      </Group>
      {issues.length > 0 && <Stack gap={6}>
        {issues.map((c) => (
          <Box key={c.id} className={classes.issue}>
            <Badge color={c.status === 'fail' ? 'red' : 'orange'} variant="light" miw={72} mt={2}>{t(`selfcheck.status.${c.status}`)}</Badge>
            <Text size="sm" className={classes.details}><Text span fw={500} inherit>{t(`selfcheck.names.${c.id}`)}</Text> <Text span c="dimmed" inherit>{t(`selfcheck.msg.${c.id}_${c.code}`, args(c))}</Text></Text>
            {fixAt[c.id] && <Anchor component={Link} to={fixAt[c.id]} size="sm" className={classes.fix}>{t('selfcheck.fix')}</Anchor>}
          </Box>
        ))}
      </Stack>}
      {q.data.retention && <Text size="xs" c="dimmed" mt="sm">{q.data.retention.last_run
        ? t('selfcheck.retentionSummary', { at: when(q.data.retention.last_run), rows: q.data.retention.rows, duration: q.data.retention.duration_ms, success: q.data.retention.last_success ? when(q.data.retention.last_success) : t('common.unknown') })
        : t('selfcheck.retentionUnknown')}</Text>}
    </Card>
  )
}
