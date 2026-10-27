import { SettingsLoadState } from './SettingsLoadState'
import { Button, Card, Group, TagsInput, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useSettingsDirty } from '../lib/settings-draft'
import { toast } from '../lib/notify'

// Admin console allow-list: login and every /api/admin call must come
// from one of these networks. The server refuses a list that would lock
// the caller out.
export function SecurityCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['security'], queryFn: () => api.get<{ admin_allow_cidrs: string[] }>('/api/admin/settings/security') })
  const [list, setList] = useState<string[] | null>(null)
  useSettingsDirty(list !== null)
  const save = useMutation({ mutationFn: () => api.put('/api/admin/settings/security', { admin_allow_cidrs: list ?? q.data?.admin_allow_cidrs ?? [] }), onSuccess: () => { setList(null); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['security'] }) }, onError: toast.err })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Card>
      <Title order={5} mb="xs">{t('security.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('security.hint')}</Text>
      <TagsInput label={t('security.allowCidrs')} description={t('security.allowCidrsHint')} placeholder="203.0.113.0/24" value={list ?? q.data?.admin_allow_cidrs ?? []} onChange={setList} />
      <Group justify="flex-end" mt="sm"><Button size="xs" loading={save.isPending} onClick={() => save.mutate()}>{t('settingsLayout.saveSection', { section: t('security.title') })}</Button></Group>
    </Card>
  )
}
