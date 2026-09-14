import { Alert, Badge, Button, Card, Code, Group, Loader, Modal, Stack, Text, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconAlertTriangle, IconRefresh } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type SystemUpdate, type UpdateInfo } from '../lib/api'
import { when } from '../lib/format'
import { setToastMuted, toast } from '../lib/notify'

const ME_URL = '/api/admin/me'

// After apply/rollback/restart the process exits. A blocking modal keeps the
// operator informed, error toasts are muted while requests fail, and once the
// panel answers again the page reloads so the new bundle is served (sessions
// survive restarts, so the operator stays signed in).
function RestartOverlay({ version, onGiveUp }: { version?: string; onGiveUp: () => void }) {
  const { t } = useTranslation()
  const [seconds, setSeconds] = useState(0)
  const [timedOut, setTimedOut] = useState(false)
  useEffect(() => {
    setToastMuted(true)
    const started = Date.now()
    let stop = false
    const tick = async () => {
      if (stop) return
      const elapsed = Math.round((Date.now() - started) / 1000)
      setSeconds(elapsed)
      if (elapsed >= 2) {
        try { await api.get(ME_URL); window.location.reload(); return } catch { /* still restarting */ }
      }
      if (elapsed >= 90) { setTimedOut(true); return }
      setTimeout(tick, 1000)
    }
    const h = setTimeout(tick, 1000)
    return () => { stop = true; clearTimeout(h); setToastMuted(false) }
  }, [])
  return (
    <Modal opened onClose={() => {}} withCloseButton={false} closeOnClickOutside={false} closeOnEscape={false} centered>
      <Stack align="center" gap="sm" py="md">
        {timedOut ? <IconAlertTriangle size={36} color="var(--mantine-color-orange-6)" /> : <Loader />}
        <Title order={4}>{version ? t('update.upgradingTo', { version }) : t('update.restartingTitle')}</Title>
        <Text size="sm" c="dimmed" ta="center">{timedOut ? t('update.restartTimeout') : t('update.restartingHint')}</Text>
        <Text size="xs" c="dimmed">{t('update.elapsed', { seconds })}</Text>
        {timedOut && (
          <Group gap="xs">
            <Button size="xs" onClick={() => window.location.reload()}>{t('update.reload')}</Button>
            <Button size="xs" variant="default" onClick={onGiveUp}>{t('common.cancel')}</Button>
          </Group>
        )}
      </Stack>
    </Modal>
  )
}

export function UpdateCard({ mb }: { mb?: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [restarting, setRestarting] = useState<{ version?: string } | null>(null)
  const q = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), retry: false })
  const check = useMutation({ mutationFn: () => api.get<SystemUpdate>('/api/admin/system/update?force=1'), onSuccess: (d) => qc.setQueryData(['update'], d), onError: toast.err })
  const afterExit = (r: { installed?: string }) => setRestarting({ version: r?.installed })
  const apply = useMutation({ mutationFn: () => api.post<{ installed?: string }>('/api/admin/system/update/apply'), onSuccess: afterExit, onError: toast.err })
  const rollback = useMutation({ mutationFn: () => api.post<{ installed?: string }>('/api/admin/system/update/rollback'), onSuccess: afterExit, onError: toast.err })
  const restart = useMutation({ mutationFn: () => api.post<{ installed?: string }>('/api/admin/system/restart'), onSuccess: () => afterExit({}), onError: toast.err })
  const d: UpdateInfo | undefined = q.data?.captain
  return (
    <Card mb={mb}>
      <Group justify="space-between" mb="xs">
        <Title order={5}>{t('update.title')}</Title>
        <Group gap="xs">
          {d && <Badge color={d.has_update ? 'orange' : 'teal'}>{d.has_update ? t('update.available', { version: d.latest }) : t('update.upToDate')}</Badge>}
          <Button size="xs" variant="default" leftSection={<IconRefresh size={14} />} loading={check.isPending} onClick={() => check.mutate()}>{t('update.check')}</Button>
        </Group>
      </Group>
      {restarting && <RestartOverlay version={restarting.version} onGiveUp={() => { setRestarting(null); qc.invalidateQueries() }} />}
      {d?.warning && <Text size="xs" c="orange" mb="xs">{d.warning}</Text>}
      {d && (
        <Stack gap="sm">
          <Text size="sm">{t('update.current')} <b>{d.current}</b>{d.latest !== d.current && <> · {t('update.latest')} <b>{d.latest}</b>{d.published_at && <Text span c="dimmed"> ({when(d.published_at).split(',')[0]})</Text>}</>}</Text>
          {!d.release_build && <Text size="xs" c="dimmed">{t('update.devBuild')}</Text>}
          {d.has_update && d.notes && <Code block style={{ whiteSpace: 'pre-wrap', maxHeight: 160, overflow: 'auto' }}>{d.notes}</Code>}
          {d.has_update && d.in_container && (
            <Alert color="orange" title={t('update.containerTitle')}>
              <Text size="sm" mb="xs">{t('update.containerHint')}</Text>
              <Code block>docker compose pull &amp;&amp; docker compose up -d</Code>
            </Alert>
          )}
          <Group gap="xs">
            {d.has_update && !d.in_container && d.release_build && (
              <Button size="xs" color="orange" loading={apply.isPending} onClick={() => modals.openConfirmModal({ title: t('update.apply'), children: <Text size="sm">{t('update.applyConfirm', { version: d.latest })}</Text>, labels: { confirm: t('update.apply'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => apply.mutate() })}>{t('update.apply')}</Button>
            )}
            {d.has_backup && !d.in_container && (
              <Button size="xs" variant="default" loading={rollback.isPending} onClick={() => modals.openConfirmModal({ title: t('update.rollback'), children: <Text size="sm">{t('update.rollbackConfirm', { version: d.backup_version || '?' })}</Text>, labels: { confirm: t('update.rollback'), cancel: t('common.cancel') }, onConfirm: () => rollback.mutate() })}>{t('update.rollback', { version: d.backup_version ? ` ${d.backup_version}` : '' })}</Button>
            )}
            <Button size="xs" variant="subtle" loading={restart.isPending} onClick={() => modals.openConfirmModal({ title: t('update.restart'), children: <Text size="sm">{t('update.restartConfirm')}</Text>, labels: { confirm: t('update.restart'), cancel: t('common.cancel') }, onConfirm: () => restart.mutate() })}>{t('update.restart')}</Button>
          </Group>
        </Stack>
      )}
    </Card>
  )
}
