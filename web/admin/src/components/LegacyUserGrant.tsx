import { Alert, Anchor, Badge, Button, Card, Group, Modal, Stack, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { toast } from '../lib/notify'

type LegacyGrant = { group_id: number | null; name: string; revision: string; can_migrate: boolean; covering_plans: { id: number; name: string }[] }

export function LegacyUserGrant({ userID, groupID }: { userID: number; groupID: number | null }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [opened, setOpened] = useState(false)
  const [error, setError] = useState('')
  const q = useQuery({ queryKey: ['legacy-user-grant', userID, groupID], queryFn: () => api.get<LegacyGrant>(`/api/admin/users/${userID}/legacy-grant`), enabled: groupID !== null, refetchInterval: 15_000 })
  const migrate = useMutation({
    mutationFn: () => api.post(`/api/admin/users/${userID}/legacy-grant/migrate`, { revision: q.data!.revision, confirm: true }),
    onSuccess: () => { setOpened(false); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['users'] }); qc.invalidateQueries({ queryKey: ['user', userID] }); qc.invalidateQueries({ queryKey: ['user-entries', userID] }); qc.invalidateQueries({ queryKey: ['legacy-user-grant', userID] }) },
    onError: (e: Error) => { setError(e instanceof ApiError && e.status === 409 ? t('legacyGrant.changed') : t('legacyGrant.failed')); void q.refetch() },
  })
  if (groupID === null || q.data?.group_id === null) return null
  return <Card withBorder padding="sm">
    <Stack gap="sm">
      <Group justify="space-between"><Text fw={600} size="sm">{t('legacyGrant.title')}</Text><Badge color="gray">{t('legacyGrant.readOnly')}</Badge></Group>
      <Text size="sm">{q.data?.name || `#${groupID}`}</Text>
      <Text size="xs" c="dimmed">{t('legacyGrant.hint')}</Text>
      {q.isError && <Alert color="red">{t('legacyGrant.failed')} <Button variant="subtle" size="compact-xs" onClick={() => q.refetch()}>{t('legacyGrant.retry')}</Button></Alert>}
      <Button variant="light" size="xs" loading={q.isPending} disabled={!q.data || q.isError} onClick={() => { setError(''); setOpened(true); void q.refetch() }}>{t('legacyGrant.migrate')}</Button>
    </Stack>
    <Modal opened={opened} onClose={() => { if (!migrate.isPending) setOpened(false) }} title={t('legacyGrant.migrate')} size="md" closeOnClickOutside={!migrate.isPending} closeOnEscape={!migrate.isPending}>
      <Stack>
        <Text size="sm">{t('legacyGrant.migrationHint')}</Text>
        {q.data?.can_migrate ? <Alert color="teal">{t('legacyGrant.covered', { plans: q.data.covering_plans.map(p => p.name).join('、') })}</Alert> : <Alert color="yellow">{t('legacyGrant.needsPlan')}</Alert>}
        <Text size="sm" c="dimmed">{t('legacyGrant.expiryHint')}</Text>
        <Anchor component={Link} to="/plans">{t('legacyGrant.managePlans')}</Anchor>
        {error && <Alert color="red">{error}</Alert>}
        <Group justify="flex-end"><Button variant="default" disabled={migrate.isPending} onClick={() => setOpened(false)}>{t('common.cancel')}</Button><Button disabled={!q.data?.can_migrate || q.isError || q.isFetching} loading={migrate.isPending} onClick={() => migrate.mutate()}>{t('legacyGrant.confirm')}</Button></Group>
      </Stack>
    </Modal>
  </Card>
}
