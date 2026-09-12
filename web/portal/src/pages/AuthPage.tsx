import { Anchor, Button, Card, Center, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Divider, Group } from '@mantine/core'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Captcha } from '../components/Captcha'

export default function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { refresh } = useAuth()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const oauth = useQuery({ queryKey: ['oauth-providers'], queryFn: () => api.get<{ providers: { id: string; name: string }[]; password_login: boolean }>('/api/oauth/providers') })
  useEffect(() => { const e = new URLSearchParams(window.location.search).get('error'); if (e) setError(e) }, [])
  const providers = oauth.data?.providers ?? []
  const policy = useQuery({ queryKey: ['register-policy'], queryFn: () => api.get<{ open: boolean; verify: boolean; reset: boolean; invite_only: boolean; email_suffixes: string[]; captcha?: { provider: string; site_key: string } }>('/api/portal/register/policy') })
  const [captcha, setCaptcha] = useState('')
  const [codeSent, setCodeSent] = useState(false)
  const [sending, setSending] = useState(false)
  const sendCode = async () => {
    setSending(true); setError('')
    try { await api.post('/api/portal/verify/send', { Email: form.values.Email, Purpose: 'register' }); setCodeSent(true) } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setSending(false) }
  }
  const passwordLogin = oauth.data?.password_login ?? true
  const form = useForm({ initialValues: { Email: '', Password: '', Code: '', Invite: new URLSearchParams(window.location.search).get('ref') ?? '', Captcha: '' } })
  const submit = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post(`/api/portal/${mode}`, { ...v, Captcha: captcha }); refresh(); nav('/') } catch (e) {
      setError(e instanceof ApiError && e.status === 403 ? t('auth.closed') : e instanceof ApiError && e.status !== 401 ? e.message : t('auth.failed'))
    } finally { setBusy(false) }
  })
  return (
    <Center mih="100vh" p="md" bg="var(--mantine-color-gray-0)">
      <Card w={400}>
        <form onSubmit={submit}><Stack>
          <Title order={2}>{t(`auth.${mode}`)}</Title>
          {providers.length > 0 && (
            <Stack gap="xs">
              {providers.map((p) => <Button key={p.id} component="a" href={`/api/oauth/${p.id}/start?next=/portal/`} variant="default" size="md">{t('auth.with', { name: p.name })}</Button>)}
              {passwordLogin && <Divider label={t('auth.or')} labelPosition="center" />}
            </Stack>
          )}
          {!passwordLogin && error && <Text c="red" size="sm">{error}</Text>}
          {passwordLogin && (<>
          <TextInput label={t('auth.email')} type="email" size="md" required autoFocus {...form.getInputProps('Email')} />
          <PasswordInput label={t('auth.password')} size="md" required minLength={8} description={mode === 'register' ? t('auth.passwordHint') : undefined} {...form.getInputProps('Password')} />
          {mode === 'register' && policy.data?.verify && (
            <Group align="flex-end" wrap="nowrap">
              <TextInput flex={1} label={t('auth.code')} size="md" required placeholder="000000" {...form.getInputProps('Code')} />
              <Button variant="default" size="md" loading={sending} disabled={!form.values.Email.includes('@')} onClick={sendCode}>{codeSent ? t('auth.resend') : t('auth.sendCode')}</Button>
            </Group>
          )}
          {mode === 'register' && (policy.data?.email_suffixes ?? []).length > 0 && <Text size="xs" c="dimmed">{t('auth.suffixes', { list: policy.data!.email_suffixes.join(', ') })}</Text>}
          {mode === 'register' && <TextInput label={policy.data?.invite_only ? t('auth.inviteRequired') : t('auth.invite')} placeholder={t('auth.inviteHint')} required={policy.data?.invite_only} {...form.getInputProps('Invite')} />}
          {mode === 'register' && policy.data?.captcha && <Captcha provider={policy.data.captcha.provider} siteKey={policy.data.captcha.site_key} onToken={setCaptcha} />}
          {mode === 'login' && policy.data?.reset && <Text size="sm" ta="right"><Anchor component={Link} to="/forgot">{t('auth.forgot')}</Anchor></Text>}
          {error && <Text c="red" size="sm">{error}</Text>}
          <Button type="submit" size="md" loading={busy}>{t(`auth.${mode}`)}</Button>
          <Text size="sm" ta="center"><Anchor component={Link} to={mode === 'login' ? '/register' : '/login'}>{t(mode === 'login' ? 'auth.toRegister' : 'auth.toLogin')}</Anchor></Text>
          </>)}
          <Group justify="center"><Anchor href="/" size="xs" c="dimmed">{t('auth.home')}</Anchor></Group>
        </Stack></form>
      </Card>
    </Center>
  )
}
