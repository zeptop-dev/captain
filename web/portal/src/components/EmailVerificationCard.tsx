import { Alert, Badge, Button, Group, Stack, Text, TextInput } from '@mantine/core'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useEmailCode } from '../lib/use-email-code'
import { registrationErrorKey, type RegistrationPolicy } from '../lib/registration'
import { Captcha } from './Captcha'

export function EmailVerificationCard() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const policy = useQuery({ queryKey: ['register-policy'], queryFn: () => api.get<RegistrationPolicy>('/api/portal/register/policy') })
  const [code, setCode] = useState('')
  const [captcha, setCaptcha] = useState('')
  const [captchaKey, setCaptchaKey] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const delivery = useEmailCode(me?.email ?? '')
  const feedback = (e: unknown) => { const key = e instanceof ApiError ? registrationErrorKey(e.code, e.message) : undefined; setError(key ? t(key) : e instanceof Error ? e.message : t('auth.failed')) }
  const send = async () => {
    setError('')
    try { await delivery.send(() => api.post('/api/portal/me/email/verification/send', { Captcha: captcha })) }
    catch (e) { feedback(e) } finally { setCaptcha(''); setCaptchaKey(k => k + 1) }
  }
  const verify = async () => {
    setBusy(true); setError('')
    try { await api.post('/api/portal/me/email/verification', { Code: code }); await refresh(); setCode('') }
    catch (e) { feedback(e) } finally { setBusy(false) }
  }
  if (!me) return null
  return <Stack gap="sm">
    <Group gap="xs"><Badge color={me.email_verification?.verified ? 'teal' : 'gray'}>{t(me.email_verification?.verified ? 'auth.emailVerified' : 'auth.emailUnverified')}</Badge>{me.email_verification?.verified_at && <Text size="xs" c="dimmed">{new Date(me.email_verification.verified_at).toLocaleString()}</Text>}</Group>
    {!me.email_verification?.verified && <>
      <Text size="sm" c="dimmed">{t('auth.emailVerificationHint')}</Text>
      {!me.email_verification_available ? <Text size="sm" c="dimmed">{t('auth.mailUnavailable')}</Text> : policy.isError ? <Button variant="subtle" onClick={() => policy.refetch()}>{t('auth.retry')}</Button> : <>
        {policy.data?.captcha && <Captcha key={captchaKey} provider={policy.data.captcha.provider} siteKey={policy.data.captcha.site_key} onToken={setCaptcha} size="compact" />}
        <Button variant="light" onClick={send} loading={delivery.sending} disabled={busy || policy.isPending || delivery.seconds > 0 || (!!policy.data?.captcha && !captcha)}>{delivery.seconds > 0 ? t('auth.resendSeconds', { seconds: delivery.seconds }) : t('auth.sendCode')}</Button>
        {delivery.sent && <Text size="xs" role="status">{t('auth.codeSentTo', { email: me.email })}</Text>}
        <form onSubmit={e => { e.preventDefault(); void verify() }}><Group align="flex-end"><TextInput label={t('auth.code')} value={code} onChange={e => setCode(e.currentTarget.value)} inputMode="numeric" autoComplete="one-time-code" required minLength={6} maxLength={6} /><Button type="submit" loading={busy} disabled={delivery.sending}>{t('auth.verifyEmail')}</Button></Group></form>
      </>}
    </>}
    {error && <Alert color="red">{error}</Alert>}
  </Stack>
}
