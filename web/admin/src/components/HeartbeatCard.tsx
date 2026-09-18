import { Badge, Button, Card, Group, NumberInput, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'

interface Settings { enabled: boolean; url: string; interval_seconds: number }
interface Status { at?: string; code?: number; error?: string }

// The probe watches the nodes; this is what watches the panel. Captain
// fetches the URL on a schedule, so an external monitor alerts when the
// beats stop — whatever the reason.
export function HeartbeatCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['heartbeat'], queryFn: () => api.get<{ settings: Settings; status: Status }>('/api/admin/settings/heartbeat') })
  const form = useForm<Settings>({ initialValues: { enabled: false, url: '', interval_seconds: 60 } })
  useEffect(() => { if (q.data) form.setValues({ ...q.data.settings, interval_seconds: q.data.settings.interval_seconds || 60 }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: Settings) => api.put('/api/admin/settings/heartbeat', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['heartbeat'] }) }, onError: toast.err })
  const test = useMutation({ mutationFn: () => api.post('/api/admin/settings/heartbeat/test', {}), onSuccess: () => { toast.ok(t('heartbeat.sent')); qc.invalidateQueries({ queryKey: ['heartbeat'] }) }, onError: toast.err })
  const st = q.data?.status
  return (
    <Card>
      <Title order={5} mb="xs">{t('heartbeat.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('heartbeat.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Switch label={t('heartbeat.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
        <Group grow align="flex-end">
          <TextInput label={t('heartbeat.url')} placeholder="https://hc-ping.com/<uuid>" {...form.getInputProps('url')} />
          <NumberInput label={t('heartbeat.interval')} min={60} step={60} w={160} {...form.getInputProps('interval_seconds')} />
        </Group>
        {st?.at && (
          <Group gap="xs">
            <Text size="xs" c="dimmed">{t('heartbeat.last')} {when(st.at)}</Text>
            {st.error ? <Badge size="xs" color="red">{st.error}</Badge> : <Badge size="xs" color="green">{st.code || 200}</Badge>}
          </Group>
        )}
        <Group justify="flex-end">
          <Button size="xs" variant="default" loading={test.isPending} onClick={() => test.mutate()}>{t('heartbeat.test')}</Button>
          <Button size="xs" type="submit" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
      </Stack></form>
    </Card>
  )
}
