import { ActionIcon, Button, Card, Group, MultiSelect, PasswordInput, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconSend, IconTrash } from '@tabler/icons-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface Endpoint { url: string; secret: string; events: string[]; enabled: boolean }

// Event webhooks: Captain POSTs signed JSON to each URL. This is the
// integration surface for external automation.
export function WebhooksCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['webhooks'], queryFn: () => api.get<{ settings: { endpoints: Endpoint[] }; events: string[] }>('/api/admin/settings/webhooks') })
  const form = useForm<{ endpoints: Endpoint[] }>({ initialValues: { endpoints: [] } })
  useEffect(() => { if (q.data) form.setValues({ endpoints: q.data.settings.endpoints.map((e) => ({ ...e, events: e.events ?? [] })) }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: { endpoints: Endpoint[] }) => api.put('/api/admin/settings/webhooks', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['webhooks'] }) }, onError: toast.err })
  const test = useMutation({ mutationFn: (e: Endpoint) => api.post('/api/admin/settings/webhooks/test', { URL: e.url, Secret: e.secret }), onSuccess: () => toast.ok(t('webhooks.testOk')), onError: toast.err })
  const events = (q.data?.events ?? []).map((e) => ({ value: e, label: e }))
  return (
    <Card>
      <Title order={5} mb="xs">{t('webhooks.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('webhooks.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        {form.values.endpoints.map((e, i) => (
          <Card key={i} withBorder padding="sm" radius="md">
            <Group gap="xs" align="flex-end" wrap="nowrap">
              <TextInput label="URL" placeholder="https://hooks.example.com/captain" style={{ flex: 3 }} {...form.getInputProps(`endpoints.${i}.url`)} />
              <PasswordInput label={t('webhooks.secret')} style={{ flex: 2 }} {...form.getInputProps(`endpoints.${i}.secret`)} />
              <Switch label={t('common.enabled')} mb={6} {...form.getInputProps(`endpoints.${i}.enabled`, { type: 'checkbox' })} />
              <ActionIcon variant="subtle" color="gray" mb={4} title={t('webhooks.test')} onClick={() => test.mutate(e)}><IconSend size={16} /></ActionIcon>
              <ActionIcon variant="subtle" color="red" mb={4} onClick={() => form.removeListItem('endpoints', i)}><IconTrash size={16} /></ActionIcon>
            </Group>
            <MultiSelect mt="xs" label={t('webhooks.events')} description={t('webhooks.eventsHint')} data={events} {...form.getInputProps(`endpoints.${i}.events`)} />
          </Card>
        ))}
        <Group justify="space-between">
          <Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => form.insertListItem('endpoints', { url: '', secret: '', events: [], enabled: true })}>{t('webhooks.add')}</Button>
          <Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
        <Text size="xs" c="dimmed">{t('webhooks.format')}</Text>
      </Stack></form>
    </Card>
  )
}
