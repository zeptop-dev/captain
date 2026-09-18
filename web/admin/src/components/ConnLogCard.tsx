import { Button, Card, Group, NumberInput, Stack, Switch, Text, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface ConnLog { enabled: boolean; retention_days: number; max_per_user: number }

// Per-connection destination log from the nodes: off by default, kept a
// few days, visible per user in the user drawer.
export function ConnLogCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['connlog-settings'], queryFn: () => api.get<ConnLog>('/api/admin/settings/connlog') })
  const form = useForm({ initialValues: { enabled: false, retention_days: 7, max_per_user: 1000 } })
  useEffect(() => { if (q.data) form.setValues({ enabled: q.data.enabled, retention_days: q.data.retention_days || 7, max_per_user: q.data.max_per_user || 1000 }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put('/api/admin/settings/connlog', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['connlog-settings'] }) }, onError: toast.err })
  return (
    <Card>
      <Title order={5} mb="xs">{t('connlog.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('connlog.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Group align="flex-end">
          <Switch label={t('connlog.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} pb={6} />
          <NumberInput w={180} label={t('connlog.retention')} min={1} max={365} {...form.getInputProps('retention_days')} />
          <NumberInput w={200} label={t('connlog.maxPerUser')} description={t('connlog.maxPerUserHint')} min={100} max={100000} step={100} {...form.getInputProps('max_per_user')} />
        </Group>
        <Text size="xs" c="orange">{t('connlog.privacy')}</Text>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>
    </Card>
  )
}
