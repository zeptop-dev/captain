import { Button, Card, Group, PasswordInput, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface DStatus { enabled: boolean; listen: string; has_key: boolean }

// One panel-wide DStatus setting. Unlike Komari this is a pull: every node
// serves the neko-status endpoint and the DStatus panel scrapes it, so the
// port has to be reachable — bosun opens it in the firewall while this is on.
export function DStatusCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['dstatus'], queryFn: () => api.get<DStatus>('/api/admin/settings/dstatus') })
  const form = useForm({ initialValues: { enabled: false, listen: '', key: '' } })
  useEffect(() => { if (q.data) form.setValues({ enabled: q.data.enabled, listen: q.data.listen, key: '' }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put('/api/admin/settings/dstatus', v), onSuccess: () => { toast.ok(t('common.saved')); form.setFieldValue('key', ''); qc.invalidateQueries({ queryKey: ['dstatus'] }) }, onError: toast.err })
  return (
    <Card>
      <Title order={5} mb="xs">{t('dstatus.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('dstatus.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Group grow align="flex-end">
          <TextInput label={t('dstatus.listen')} description={t('dstatus.listenHint')} placeholder=":9999" {...form.getInputProps('listen')} />
          <PasswordInput label={t('dstatus.key')} description={q.data?.has_key ? t('dstatus.keySet') : t('dstatus.keyHint')} placeholder={q.data?.has_key ? '••••••••' : ''} {...form.getInputProps('key')} />
        </Group>
        <Group justify="space-between" align="flex-start">
          <Switch label={t('dstatus.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
          <Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
      </Stack></form>
    </Card>
  )
}
