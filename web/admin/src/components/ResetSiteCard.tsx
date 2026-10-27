import { Alert, Button, Card, Checkbox, Group, Modal, PasswordInput, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconAlertTriangle } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'

import { useSettingsHaveDrafts } from '../lib/settings-draft'

const categories = ['users', 'staff', 'nodes', 'plans', 'orders', 'subscriptions'] as const
type Counts = Record<(typeof categories)[number], number>

export function ResetSiteCard() {
  const hasDrafts = useSettingsHaveDrafts()
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const [opened, setOpened] = useState(false)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [nodesAcknowledged, setNodesAcknowledged] = useState(false)
  const preview = useQuery({ queryKey: ['site-reset-preview'], queryFn: () => api.get<Counts>('/api/admin/system/reset'), enabled: opened, staleTime: 0 })
  const reset = useMutation({
    mutationFn: () => { if (hasDrafts) throw new Error(t('settingsLayout.saveBeforeRestart')); return api.post('/api/admin/system/reset', { password, code, confirmation, nodes_acknowledged: nodesAcknowledged }) },
    onSuccess: () => { qc.clear(); window.location.replace('/admin/login') },
    onError: (error) => { setCode(''); toast.err(error) },
  })
  const close = () => {
    if (reset.isPending) return
    setOpened(false); setPassword(''); setCode(''); setConfirmation(''); setNodesAcknowledged(false)
  }
  const hasNodes = (preview.data?.nodes ?? 0) > 0
  const canReset = !hasDrafts && preview.isSuccess && !preview.isFetching && confirmation === 'RESET' && !!password && (!me?.totp || !!code.trim()) && (!hasNodes || nodesAcknowledged)
  return (
    <Card>
      <Title order={5} c="red" mb="xs">{t('siteReset.title')}</Title>
      <Text size="sm" mb="sm">{t('siteReset.description')}</Text>
      {hasDrafts && <Text size="sm" c="dimmed" mb="sm">{t('settingsLayout.saveBeforeRestart')}</Text>}
      <Button color="red" variant="light" size="xs" disabled={hasDrafts} onClick={() => setOpened(true)}>{t('siteReset.open')}</Button>
      <Modal opened={opened} onClose={close} title={t('siteReset.title')} size="lg" centered closeOnClickOutside={!reset.isPending} closeOnEscape={!reset.isPending} withCloseButton={!reset.isPending}>
        <form onSubmit={(e) => { e.preventDefault(); if (canReset && !reset.isPending) reset.mutate() }}>
          <Stack gap="md">
            <Alert color="red" icon={<IconAlertTriangle size={18} />} title={t('siteReset.irreversible')}>{t('siteReset.warning')}</Alert>
            {preview.isPending ? <Text size="sm">{t('siteReset.loading')}</Text> : preview.isError ? (
              <Alert color="red">{t('siteReset.previewFailed')} <Button variant="subtle" size="xs" onClick={() => preview.refetch()}>{t('siteReset.retry')}</Button></Alert>
            ) : <SimpleGrid cols={{ base: 2, sm: 3 }}>{categories.map((key) => (
              <div key={key}><Text size="xs" c="dimmed">{t(`siteReset.counts.${key}`)}</Text><Text fw={600}>{preview.data[key]}</Text></div>
            ))}</SimpleGrid>}
            <Text size="sm">{t('siteReset.admin', { email: me?.email })}</Text>
            <Text size="xs" c="dimmed">{t('siteReset.preserved')}</Text>
            {hasNodes && <Alert color="orange">{t('siteReset.nodesWarning')}<Checkbox mt="sm" label={t('siteReset.nodesAcknowledge')} checked={nodesAcknowledged} onChange={(e) => setNodesAcknowledged(e.currentTarget.checked)} disabled={reset.isPending} /></Alert>}
            <PasswordInput label={t('siteReset.password')} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.currentTarget.value)} required disabled={reset.isPending} />
            {me?.totp && <TextInput label={t('twofa.code')} description={t('siteReset.codeHint')} inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.currentTarget.value)} required disabled={reset.isPending} />}
            <TextInput label={t('siteReset.confirmation')} description={t('siteReset.confirmationHint')} autoComplete="off" value={confirmation} onChange={(e) => setConfirmation(e.currentTarget.value)} required disabled={reset.isPending} />
            <Group justify="flex-end"><Button variant="default" onClick={close} disabled={reset.isPending}>{t('common.cancel')}</Button><Button color="red" type="submit" loading={reset.isPending} disabled={!canReset}>{t('siteReset.execute')}</Button></Group>
          </Stack>
        </form>
      </Modal>
    </Card>
  )
}
