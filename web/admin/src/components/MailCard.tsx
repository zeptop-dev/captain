import { Button, Card, Group, NumberInput, PasswordInput, Select, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type MailSettings } from '../lib/api'
import { toast } from '../lib/notify'

const empty: MailSettings = { provider: '', from_name: '', from_address: '', smtp: { host: '', port: 587, username: '', password: '', security: '' }, resend: { api_key: '' }, verify_registration: false, reminders: true }

export function MailCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['mail-settings'], queryFn: () => api.get<{ settings: MailSettings; has_smtp_password: boolean; has_resend_key: boolean }>('/api/admin/settings/mail') })
  const form = useForm<MailSettings>({ initialValues: empty })
  useEffect(() => { if (q.data) form.setValues({ ...empty, ...q.data.settings, smtp: { ...empty.smtp, ...q.data.settings.smtp, password: '' }, resend: { api_key: '' } }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: MailSettings) => api.put('/api/admin/settings/mail', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['mail-settings'] }) }, onError: toast.err })
  const [to, setTo] = useState('')
  const test = useMutation({ mutationFn: () => api.post('/api/admin/settings/mail/test', { To: to }), onSuccess: () => toast.ok(t('mail.testSent')), onError: toast.err })
  const v = form.values
  return (
    <Card>
      <Title order={5} mb="xs">{t('mail.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('mail.hint')}</Text>
      <form onSubmit={form.onSubmit((vals) => save.mutate(vals))}><Stack gap="sm">
        <Group grow>
          <Select label={t('mail.provider')} data={[{ value: '', label: t('mail.off') }, { value: 'smtp', label: 'SMTP' }, { value: 'resend', label: 'Resend API' }]} allowDeselect={false} {...form.getInputProps('provider')} />
          <TextInput label={t('mail.fromName')} {...form.getInputProps('from_name')} />
          <TextInput label={t('mail.fromAddress')} placeholder="noreply@example.com" {...form.getInputProps('from_address')} />
        </Group>
        {v.provider === 'smtp' && (<>
          <Group grow>
            <TextInput label="SMTP host" placeholder="smtp.example.com" {...form.getInputProps('smtp.host')} />
            <NumberInput label={t('mail.port')} min={1} max={65535} {...form.getInputProps('smtp.port')} />
            <Select label={t('mail.security')} data={[{ value: '', label: t('mail.securityAuto') }, { value: 'starttls', label: 'STARTTLS (587)' }, { value: 'tls', label: 'TLS (465)' }, { value: 'none', label: t('mail.securityNone') }]} allowDeselect={false} {...form.getInputProps('smtp.security')} />
          </Group>
          <Group grow>
            <TextInput label={t('mail.username')} {...form.getInputProps('smtp.username')} />
            <PasswordInput label={t('mail.password')} placeholder={q.data?.has_smtp_password ? t('mail.keep') : ''} {...form.getInputProps('smtp.password')} />
          </Group>
        </>)}
        {v.provider === 'resend' && <PasswordInput label="Resend API key" placeholder={q.data?.has_resend_key ? t('mail.keep') : 're_...'} {...form.getInputProps('resend.api_key')} />}
        <Group>
          <Switch label={t('mail.verify')} {...form.getInputProps('verify_registration', { type: 'checkbox' })} />
          <Switch label={t('mail.reminders')} {...form.getInputProps('reminders', { type: 'checkbox' })} />
        </Group>
        <Group justify="space-between" align="flex-end">
          <Group align="flex-end"><TextInput label={t('mail.testTo')} placeholder="you@example.com" value={to} onChange={(e) => setTo(e.currentTarget.value)} /><Button variant="default" size="sm" loading={test.isPending} disabled={!to.includes('@')} onClick={() => test.mutate()}>{t('mail.test')}</Button></Group>
          <Button type="submit" size="sm" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
      </Stack></form>
    </Card>
  )
}
