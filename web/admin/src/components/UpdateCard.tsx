import { Alert, Badge, Box, Button, Card, Code, Group, Modal, Stack, Text, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconAlertTriangle, IconRefresh } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type SystemUpdate, type UpdateInfo } from '../lib/api'
import { when } from '../lib/format'
import { setToastMuted, toast } from '../lib/notify'

import { useSettingsHaveDrafts } from '../lib/settings-draft'

const ME_URL = '/api/admin/me'

// After apply/rollback/restart the process exits. A blocking modal keeps the
// operator informed, error toasts are muted while requests fail, and once the
// panel answers again the page reloads so the new bundle is served (sessions
// survive restarts, so the operator stays signed in).
function RestartOverlay({ version, submitting, onGiveUp }: { version?: string; submitting?: boolean; onGiveUp: () => void }) {
  const { t } = useTranslation()
  const [seconds, setSeconds] = useState(0)
  const [timedOut, setTimedOut] = useState(false)
  const [phase, setPhase] = useState('')
  const [unreachable, setUnreachable] = useState(false)
  const submittingRef = useRef(submitting)
  useEffect(() => { submittingRef.current = submitting }, [submitting])
  useEffect(() => {
    setToastMuted(true)
    const started = Date.now()
    let stop = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let request: AbortController | undefined
    const tick = async () => {
      if (stop) return
      const elapsed = Math.round((Date.now() - started) / 1000)
      setSeconds(elapsed)
      if (elapsed >= 2) {
        request = new AbortController()
        const timeout = setTimeout(() => request?.abort(), 8000)
        try {
          if (version) {
            const result = await api.get<SystemUpdate>('/api/admin/system/update', request.signal)
            const info = result.captain
            if (stop) return
            setUnreachable(false)
            const job = info?.host_update?.job
            if (!submittingRef.current && job?.target === version) { setPhase(job.phase); if (job.phase === 'failed') { setTimedOut(true); return } }
            if (info?.current === version && (!info.in_container || job?.phase === 'complete')) { window.location.reload(); return }
          } else { await api.get(ME_URL, request.signal); if (!stop) window.location.reload(); return }
        } catch { if (!stop) setUnreachable(true) } finally { clearTimeout(timeout) }
      }
      if (elapsed >= 1800) { setTimedOut(true); return }
      if (!stop) timer = setTimeout(tick, 1000)
    }
    timer = setTimeout(tick, 1000)
    return () => { stop = true; request?.abort(); clearTimeout(timer); setToastMuted(false) }
  }, [version])
  const stages = ['checking', 'pulling', 'backing_up', 'recreating', 'verifying', 'complete']
  const current = stages.indexOf(phase)
  const label = submitting && !phase ? t('update.preparing') : unreachable ? t('update.waitingService') : phase ? t(`update.hostPhases.${phase}`) : t('update.waitingService')
  return (
    <Modal opened onClose={() => {}} withCloseButton={false} closeOnClickOutside={false} closeOnEscape={false} centered>
      <Stack align="center" gap="sm" py="md">
        {timedOut ? <IconAlertTriangle size={36} color="var(--mantine-color-orange-6)" /> : null}
        <Title order={4}>{version ? t('update.upgradingTo', { version }) : t('update.restartingTitle')}</Title>
        <Text size="sm" c="dimmed" ta="center">{phase === 'failed' ? t('update.hostFailed') : timedOut ? t('update.restartTimeout') : t('update.restartingHint')}</Text>
        <Box w="100%" role="status" aria-live="polite">
          {(current < 0 || unreachable) && !timedOut ? <progress style={{ width: '100%' }} aria-label={label} /> : <Group gap={4} wrap="nowrap" aria-hidden="true">
            {stages.map((stage, i) => <Box key={stage} style={{ flex: 1, height: 8, borderRadius: 4, background: timedOut ? 'var(--mantine-color-orange-5)' : i < current ? 'var(--mantine-color-teal-6)' : i === current ? 'var(--mantine-primary-color-filled)' : 'var(--mantine-color-default-hover)' }} />)}
          </Group>}
          <Text size="sm" ta="center" mt="sm">{label}</Text>
          <Text size="xs" c="dimmed" ta="center" mt={4}>{t('update.elapsed', { seconds })}</Text>
          {current >= 0 && !timedOut && <Text size="xs" c="dimmed" ta="center">{t('update.stageCount', { current: current + 1, total: stages.length })}</Text>}
        </Box>
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
  const hasDrafts = useSettingsHaveDrafts()
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [restarting, setRestarting] = useState<{ version?: string } | null>(null)
  const q = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), retry: false, refetchInterval: (query) => { const phase = query.state.data?.captain?.host_update?.job?.phase; return phase && !['complete', 'failed'].includes(phase) ? 3000 : false } })
  const check = useMutation({ mutationFn: () => api.get<SystemUpdate>('/api/admin/system/update?force=1'), onSuccess: (d) => qc.setQueryData(['update'], d), onError: toast.err })
  const ensureSaved = () => { if (hasDrafts) throw new Error(t('settingsLayout.saveBeforeRestart')) }
  const afterExit = (r: { installed?: string }) => setRestarting({ version: r?.installed })
  const apply = useMutation({ mutationFn: () => { ensureSaved(); return api.post<{ installed?: string }>('/api/admin/system/update/apply') }, onSuccess: afterExit, onError: (e) => { setRestarting(null); setToastMuted(false); toast.err(e) } })
  const rollback = useMutation({ mutationFn: () => { ensureSaved(); return api.post<{ installed?: string }>('/api/admin/system/update/rollback') }, onSuccess: afterExit, onError: toast.err })
  const restart = useMutation({ mutationFn: () => { ensureSaved(); return api.post<{ installed?: string }>('/api/admin/system/restart') }, onSuccess: () => afterExit({}), onError: toast.err })
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
      {hasDrafts && <Alert color="yellow" mb="sm">{t('settingsLayout.saveBeforeRestart')}</Alert>}
      {restarting && <RestartOverlay submitting={apply.isPending} version={restarting.version} onGiveUp={() => { setRestarting(null); qc.invalidateQueries() }} />}
      {d?.warning && <Text size="xs" c="orange" mb="xs">{d.warning}</Text>}
      {d && (
        <Stack gap="sm">
          <Text size="sm">{t('update.current')} <b>{d.current}</b>{d.latest !== d.current && <> · {t('update.latest')} <b>{d.latest}</b>{d.published_at && <Text span c="dimmed"> ({when(d.published_at).split(',')[0]})</Text>}</>}</Text>
          {!d.release_build && <Text size="xs" c="dimmed">{t('update.devBuild')}</Text>}
          {d.has_update && d.notes && <Code block style={{ whiteSpace: 'pre-wrap', maxHeight: 160, overflow: 'auto' }}>{d.notes}</Code>}
          {d.in_container && !d.host_update?.available && (
            <Alert color="orange" title={t('update.containerTitle')}>
              <Text size="sm" mb="xs">{t('update.containerHint')}</Text>
              <Code block>{'curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sudo sh -s -- enable-web-upgrade'}</Code>
            </Alert>
          )}
          {d.host_update?.available && <Text size="sm" c="teal">{t('update.hostReady')}</Text>}
          {d.host_update?.job && <Alert color={d.host_update.job.phase === 'failed' ? 'red' : 'blue'} title={d.host_update.job.target}>
            <Text size="sm">{t(`update.hostPhases.${d.host_update.job.phase}`)}</Text>
            {d.host_update.job.phase === 'failed' && <Text size="sm">{t('update.hostFailed')}</Text>}
            {!['complete', 'failed'].includes(d.host_update.job.phase) && <Button mt="xs" size="xs" disabled={hasDrafts} onClick={() => setRestarting({ version: d.host_update?.job?.target })}>{t('update.hostProgress')}</Button>}
          </Alert>}
          <Group gap="xs">
            {d.has_update && (!d.in_container || d.host_update?.available) && d.release_build && (
              <Button size="xs" color="orange" disabled={hasDrafts} loading={apply.isPending} onClick={() => modals.openConfirmModal({ title: t('update.apply'), children: <Text size="sm">{t('update.applyConfirm', { version: d.latest })}</Text>, labels: { confirm: t('update.apply'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => { setRestarting({ version: d.latest }); apply.mutate() } })}>{t('update.apply')}</Button>
            )}
            {d.has_backup && !d.in_container && (
              <Button size="xs" variant="default" disabled={hasDrafts} loading={rollback.isPending} onClick={() => modals.openConfirmModal({ title: t('update.rollback'), children: <Text size="sm">{t('update.rollbackConfirm', { version: d.backup_version || '?' })}</Text>, labels: { confirm: t('update.rollback'), cancel: t('common.cancel') }, onConfirm: () => rollback.mutate() })}>{t('update.rollback', { version: d.backup_version ? ` ${d.backup_version}` : '' })}</Button>
            )}
            <Button size="xs" variant="subtle" disabled={hasDrafts} loading={restart.isPending} onClick={() => modals.openConfirmModal({ title: t('update.restart'), children: <Text size="sm">{t('update.restartConfirm')}</Text>, labels: { confirm: t('update.restart'), cancel: t('common.cancel') }, onConfirm: () => restart.mutate() })}>{t('update.restart')}</Button>
          </Group>
        </Stack>
      )}
    </Card>
  )
}
