import { SettingsFields } from './SettingsFields'
import { SettingsLoadState } from './SettingsLoadState'
import { Anchor, Badge, Button, Card, Group, NumberInput, PasswordInput, Select, Stack, Switch, Text, TextInput, Textarea, Title } from '@mantine/core'
import { useSettingsDirty, useSettingsForm as useForm } from '../lib/settings-draft'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, type RegistrationStatus } from '../lib/api'
import { toast } from '../lib/notify'

type Values = { enabled: boolean; suffixes: string; invite_only: boolean; ip_limit: number; ip_window_hours: number; provider: string; site_key: string; secret_key: string }

export function RegistrationCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [enabledEdited, setEnabledEdited] = useState(false)
  useSettingsDirty(enabledEdited)
  const q = useQuery({ queryKey: ['registration-settings'], queryFn: () => api.get<RegistrationStatus>('/api/admin/settings/registration') })
  const form = useForm<Values>({ initialValues: { enabled: false, suffixes: '', invite_only: false, ip_limit: 0, ip_window_hours: 24, provider: '', site_key: '', secret_key: '' } })
  useEffect(() => { const s = q.data?.settings; if (s) form.hydrate({ enabled: q.data!.enabled, suffixes: s.email_suffixes.join('\n'), invite_only: s.invite_only, ip_limit: s.ip_limit, ip_window_hours: s.ip_window_hours || 24, provider: s.captcha.provider, site_key: s.captcha.site_key, secret_key: '' }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({
    mutationFn: (v: Values) => api.put('/api/admin/settings/registration', { ...(enabledEdited ? { enabled: v.enabled } : {}), email_suffixes: v.suffixes.split('\n').map((s) => s.trim()).filter(Boolean), invite_only: v.invite_only, ip_limit: v.ip_limit, ip_window_hours: v.ip_window_hours, captcha: { provider: v.provider, site_key: v.site_key, secret_key: v.secret_key } }),
    onSuccess: () => { setEnabledEdited(false); form.resetDirty(); form.hydrate({ ...form.getValues(), secret_key: '' }); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['registration-settings'] }) }, onError: toast.err,
  })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Card>
      <Title order={5} mb="xs">{t('registration.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('registration.hint')}</Text>
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <Switch label={t('registration.enabled')} description={t('registration.enabledHint')} checked={form.values.enabled} onChange={event => { form.setFieldValue('enabled', event.currentTarget.checked); setEnabledEdited(true) }} />
        <Stack gap={4}>
          {q.data.settings.enabled === undefined && <Text size="xs" c="dimmed">{t('registration.inherited')}</Text>}
          <Group gap="xs"><Text size="sm">{t('registration.passwordStatus')}</Text><Badge color={q.data.password_open ? 'teal' : 'gray'}>{q.data.password_open ? t('registration.open') : t('registration.closed')}</Badge></Group>
          <Group gap="xs"><Text size="sm">{t('registration.oidcStatus')}</Text><Badge color={q.data.oidc_open ? 'teal' : 'gray'}>{q.data.oidc_open ? t('registration.open') : t('registration.closed')}</Badge><Anchor component={Link} to="/settings/category/account#oidc" size="xs">{t('settings.oidc')}</Anchor></Group>
        </Stack>
        <Textarea label={t('registration.suffixes')} description={t('registration.suffixesHint')} placeholder={'gmail.com\noutlook.com'} autosize minRows={1} {...form.getInputProps('suffixes')} />
        <SettingsFields>
          <NumberInput label={t('registration.ipLimit')} description={t('registration.ipLimitHint')} min={0} {...form.getInputProps('ip_limit')} />
          <NumberInput label={t('registration.ipWindow')} min={1} {...form.getInputProps('ip_window_hours')} />
        </SettingsFields>
        <Switch label={t('registration.inviteOnly')} {...form.getInputProps('invite_only', { type: 'checkbox' })} />
        <SettingsFields>
          <Select label={t('registration.captcha')} data={[{ value: '', label: t('common.none') }, { value: 'turnstile', label: 'Cloudflare Turnstile' }, { value: 'recaptcha', label: 'Google reCAPTCHA v2' }, { value: 'hcaptcha', label: 'hCaptcha' }]} allowDeselect={false} {...form.getInputProps('provider')} />
          {form.values.provider && <TextInput label="Site key" {...form.getInputProps('site_key')} />}
          {form.values.provider && <PasswordInput label="Secret key" placeholder={q.data?.has_captcha_secret ? t('mail.keep') : ''} {...form.getInputProps('secret_key')} />}
        </SettingsFields>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('settingsLayout.saveSection', { section: t('registration.title') })}</Button></Group>
      </Stack></form>
    </Card>
  )
}
