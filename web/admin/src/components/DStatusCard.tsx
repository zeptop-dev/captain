import { Button, Card, Group, NumberInput, PasswordInput, SegmentedControl, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface DStatus { enabled: boolean; mode: string; listen: string; server: string; interval: number; has_key: boolean }

// One panel-wide DStatus setting, pushed to every node. Passive: nodes
// serve the neko-status endpoint and the DStatus panel scrapes them (port
// reachable, opened by bosun). Active: nodes report to the panel under the
// SID set on each node's page, and open no port.
export function DStatusCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['dstatus'], queryFn: () => api.get<DStatus>('/api/admin/settings/dstatus') })
  const form = useForm({ initialValues: { enabled: false, mode: 'passive', listen: '', server: '', interval: 3, key: '' } })
  useEffect(() => { if (q.data) form.setValues({ enabled: q.data.enabled, mode: q.data.mode || 'passive', listen: q.data.listen, server: q.data.server ?? '', interval: q.data.interval || 3, key: '' }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put('/api/admin/settings/dstatus', v), onSuccess: () => { toast.ok(t('common.saved')); form.setFieldValue('key', ''); qc.invalidateQueries({ queryKey: ['dstatus'] }) }, onError: toast.err })
  const active = form.values.mode === 'active'
  return (
    <Card>
      <Title order={5} mb="xs">{t('dstatus.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('dstatus.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Group align="flex-start">
          <SegmentedControl size="xs" data={[{ value: 'passive', label: t('dstatus.passive') }, { value: 'active', label: t('dstatus.active') }]} {...form.getInputProps('mode')} />
          <Text size="xs" c="dimmed" mt={4}>{active ? t('dstatus.activeHint') : t('dstatus.passiveHint')}</Text>
        </Group>
        <Group grow align="flex-start">
          {active
            ? <TextInput label={t('dstatus.server')} description={t('dstatus.serverHint')} placeholder="https://status.example.com" {...form.getInputProps('server')} />
            : <TextInput label={t('dstatus.listen')} description={t('dstatus.listenHint')} placeholder=":9999" {...form.getInputProps('listen')} />}
          <PasswordInput label={t('dstatus.key')} description={q.data?.has_key ? t('dstatus.keySet') : t('dstatus.keyHint')} placeholder={q.data?.has_key ? '••••••••' : ''} {...form.getInputProps('key')} />
        </Group>
        {active && <NumberInput label={t('dstatus.interval')} description={t('dstatus.intervalHint')} min={1} max={300} {...form.getInputProps('interval')} />}
        <Group justify="space-between" align="flex-start">
          <Switch label={t('dstatus.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
          <Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
      </Stack></form>
    </Card>
  )
}
