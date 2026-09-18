import { Button, Card, Group, NumberInput, Stack, Switch, TagsInput, Text, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface Settings { enabled: boolean; trigger_mbps: number; trigger_seconds: number; limit_mbps: number; limit_seconds: number; windows: string[]; whitelist: number[]; throttle_unlimited: boolean }

// Dynamic speed limit: a user averaging above the trigger across all nodes
// gets a temporary limit; the nodes shape it like a plan limit.
export function DynLimitCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['dynlimit'], queryFn: () => api.get<Settings>('/api/admin/settings/dynlimit') })
  const form = useForm<Settings>({ initialValues: { enabled: false, trigger_mbps: 100, trigger_seconds: 60, limit_mbps: 30, limit_seconds: 600, windows: [], whitelist: [], throttle_unlimited: false } })
  useEffect(() => { if (q.data) form.setValues({ ...q.data, windows: q.data.windows ?? [], whitelist: q.data.whitelist ?? [] }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: Settings) => api.put('/api/admin/settings/dynlimit', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['dynlimit'] }) }, onError: toast.err })
  return (
    <Card>
      <Title order={5} mb="xs">{t('dynlimit.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('dynlimit.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate({ ...v, whitelist: v.whitelist.map(Number).filter((n) => n > 0) }))}><Stack gap="sm">
        <Switch label={t('dynlimit.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
        <Switch label={t('dynlimit.unlimited')} description={t('dynlimit.unlimitedHint')} {...form.getInputProps('throttle_unlimited', { type: 'checkbox' })} />
        <Group grow>
          <NumberInput label={t('dynlimit.trigger')} min={1} {...form.getInputProps('trigger_mbps')} />
          <NumberInput label={t('dynlimit.triggerSeconds')} min={60} step={60} {...form.getInputProps('trigger_seconds')} />
          <NumberInput label={t('dynlimit.limit')} min={1} {...form.getInputProps('limit_mbps')} />
          <NumberInput label={t('dynlimit.limitSeconds')} min={60} step={60} {...form.getInputProps('limit_seconds')} />
        </Group>
        <Group grow>
          <TagsInput label={t('dynlimit.windows')} description={t('dynlimit.windowsHint')} placeholder="20:00-02:00" {...form.getInputProps('windows')} />
          <TagsInput label={t('dynlimit.whitelist')} description={t('dynlimit.whitelistHint')} placeholder="12, 34" value={form.values.whitelist.map(String)} onChange={(v) => form.setFieldValue('whitelist', v.map(Number).filter((n) => n > 0))} />
        </Group>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>
    </Card>
  )
}
