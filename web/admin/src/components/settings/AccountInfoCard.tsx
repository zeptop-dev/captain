import { Button, Card, Checkbox, Group, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type AccountInfoSettings, type SubscriptionSettings } from '../../lib/api'
import { useSettingsDirty } from '../../lib/settings-draft'
import { toast } from '../../lib/notify'
import { SettingsLoadState } from '../SettingsLoadState'

const empty: AccountInfoSettings = { traffic: false, reset: false, expiry: false }

export function AccountInfoFields({ value, onChange, disabled = false }: { value: AccountInfoSettings; onChange: (value: AccountInfoSettings) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  return <Stack gap="xs">
    <Text size="xs" c="dimmed">{t('accountInfo.hint')}</Text>
    <Checkbox label={t('accountInfo.traffic')} checked={value.traffic} disabled={disabled} onChange={e => onChange({ ...value, traffic: e.currentTarget.checked })} />
    <Checkbox label={t('accountInfo.reset')} checked={value.reset} disabled={disabled} onChange={e => onChange({ ...value, reset: e.currentTarget.checked })} />
    <Checkbox label={t('accountInfo.expiry')} checked={value.expiry} disabled={disabled} onChange={e => onChange({ ...value, expiry: e.currentTarget.checked })} />
    <Text size="xs" c="dimmed">{t('accountInfo.refreshHint')}</Text>
  </Stack>
}

export function AccountInfoCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const query = useQuery({ queryKey: ['subscription-settings'], queryFn: () => api.get<SubscriptionSettings>('/api/admin/settings/subscription') })
  const [draft, setDraft] = useState<AccountInfoSettings | null>(null)
  useSettingsDirty(draft !== null)
  const save = useMutation({
    mutationFn: (account_info: AccountInfoSettings) => api.put('/api/admin/settings/subscription', { account_info }),
    onSuccess: () => { setDraft(null); qc.invalidateQueries({ queryKey: ['subscription-settings'] }); toast.ok(t('common.saved')) },
    onError: toast.err,
  })
  if (query.data === undefined) return <SettingsLoadState query={query} />
  return <Card>
    <Title order={5} mb="sm">{t('accountInfo.title')}</Title>
    <AccountInfoFields value={draft ?? { ...empty, ...query.data.account_info }} onChange={setDraft} disabled={save.isPending} />
    <Group justify="flex-end" mt="sm"><Button size="xs" loading={save.isPending} disabled={draft === null} onClick={() => { if (draft) save.mutate(draft) }}>{t('settingsLayout.saveSection', { section: t('accountInfo.title') })}</Button></Group>
  </Card>
}
