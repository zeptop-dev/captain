import { SettingsFields } from './SettingsFields'
import { SettingsLoadState } from './SettingsLoadState'
import { Button, Card, Group, NumberInput, PasswordInput, Select, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useSettingsForm as useForm } from '../lib/settings-draft'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Plan } from '../lib/api'
import { toast } from '../lib/notify'

interface TG { bot_token: string; bot_username: string; admin_chat_id: number; notify_orders: boolean; notify_tickets: boolean }

export function TelegramCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['telegram-settings'], queryFn: () => api.get<{ settings: TG; has_token: boolean }>('/api/admin/settings/telegram') })
  const form = useForm<TG>({ initialValues: { bot_token: '', bot_username: '', admin_chat_id: 0, notify_orders: true, notify_tickets: true } })
  useEffect(() => { if (q.data) form.hydrate({ ...q.data.settings, bot_token: '' }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: TG) => api.put('/api/admin/settings/telegram', v), onSuccess: () => { form.resetDirty(); form.hydrate({ ...form.getValues(), bot_token: '' }); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['telegram-settings'] }) }, onError: toast.err })
  const test = useMutation({ mutationFn: () => api.post('/api/admin/settings/telegram/test'), onSuccess: () => toast.ok(t('telegram.testSent')), onError: toast.err })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Card>
      <Title order={5} mb="xs">{t('telegram.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('telegram.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <SettingsFields>
          <PasswordInput label={t('telegram.token')} placeholder={q.data?.has_token ? t('mail.keep') : '123456:ABC-DEF…'} {...form.getInputProps('bot_token')} />
          <TextInput label={t('telegram.username')} readOnly value={q.data?.settings.bot_username ? '@' + q.data.settings.bot_username : ''} />
        </SettingsFields>
        <SettingsFields>
          <NumberInput label={t('telegram.adminChat')} description={t('telegram.adminChatHint')} {...form.getInputProps('admin_chat_id')} />
          <Stack gap={6} pb={4}>
            <Switch label={t('telegram.notifyOrders')} {...form.getInputProps('notify_orders', { type: 'checkbox' })} />
            <Switch label={t('telegram.notifyTickets')} {...form.getInputProps('notify_tickets', { type: 'checkbox' })} />
          </Stack>
        </SettingsFields>
        <Group justify="flex-end"><Button size="xs" variant="light" loading={test.isPending} onClick={() => test.mutate()}>{t('telegram.test')}</Button><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>
    </Card>
  )
}

export function TrialCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['trial-settings'], queryFn: () => api.get<{ plan_id: number; period_days: number }>('/api/admin/settings/trial') })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const form = useForm<{ plan: string | null; days: number }>({ initialValues: { plan: null, days: 1 } })
  useEffect(() => { if (q.data) form.hydrate({ plan: q.data.plan_id ? String(q.data.plan_id) : null, days: q.data.period_days || 1 }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: { plan: string | null; days: number }) => api.put('/api/admin/settings/trial', { plan_id: v.plan ? Number(v.plan) : 0, period_days: v.days }), onSuccess: () => { form.resetDirty(); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['trial-settings'] }) }, onError: toast.err })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Card>
      <Title order={5} mb="xs">{t('trial.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('trial.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Group align="flex-end">
        <Select label={t('trial.plan')} placeholder={t('trial.off')} clearable data={(plans.data ?? []).map((p) => ({ value: String(p.ID), label: p.Name }))} style={{ flex: 2 }} {...form.getInputProps('plan')} />
        <NumberInput label={t('trial.days')} min={1} style={{ flex: 1 }} {...form.getInputProps('days')} />
        <Button type="submit" size="xs" mb={2} loading={save.isPending}>{t('common.save')}</Button>
      </Group></form>
    </Card>
  )
}
