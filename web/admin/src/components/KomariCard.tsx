import { Button, Card, Group, NumberInput, PasswordInput, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface Komari { enabled: boolean; server: string; interval: number; has_key: boolean }

// One panel-wide Komari setting: every node registers itself under its own
// name and reports as a Komari agent, alongside Captain's own probe.
export function KomariCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['komari'], queryFn: () => api.get<Komari>('/api/admin/settings/komari') })
  const form = useForm({ initialValues: { enabled: false, server: '', key: '', interval: 3 } })
  useEffect(() => { if (q.data) form.setValues({ enabled: q.data.enabled, server: q.data.server, key: '', interval: q.data.interval || 3 }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put('/api/admin/settings/komari', v), onSuccess: () => { toast.ok(t('common.saved')); form.setFieldValue('key', ''); qc.invalidateQueries({ queryKey: ['komari'] }) }, onError: toast.err })
  return (
    <Card>
      <Title order={5} mb="xs">{t('komari.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('komari.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Group grow align="flex-end">
          <TextInput label={t('komari.server')} placeholder="https://komari.example.com" {...form.getInputProps('server')} />
          <PasswordInput label={t('komari.key')} description={q.data?.has_key ? t('komari.keySet') : t('komari.keyHint')} placeholder={q.data?.has_key ? '••••••••' : ''} {...form.getInputProps('key')} />
        </Group>
        <Group grow align="flex-end">
          <NumberInput label={t('komari.interval')} min={1} max={300} {...form.getInputProps('interval')} />
          <Switch label={t('komari.enabled')} mb={6} {...form.getInputProps('enabled', { type: 'checkbox' })} />
        </Group>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>
    </Card>
  )
}
