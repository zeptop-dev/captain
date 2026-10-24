import { Alert, Anchor, Button, Divider, Loader, PasswordInput, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { IconArrowRight, IconKey, IconLock, IconMail, IconTicket } from '@tabler/icons-react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { loginReturn } from '../lib/login-return'
import { Captcha } from '../components/Captcha'
import { AuthLayout } from '../components/AuthLayout'
import styles from '../components/AuthLayout.module.css'

export default function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const next = loginReturn(window.location.search)
  const { refresh } = useAuth()
  const [busy, setBusy] = useState(false)
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [error, setError] = useState(() => new URLSearchParams(window.location.search).get('error') ?? '')
  const oauth = useQuery({ queryKey: ['oauth-providers'], meta: { handlesError: true }, queryFn: () => api.get<{ providers: { id: string; name: string }[]; password_login: boolean }>('/api/oauth/providers') })
  const providers = oauth.data?.providers ?? []
  const policy = useQuery({ queryKey: ['register-policy'], meta: { handlesError: true }, refetchInterval: 30_000, queryFn: () => api.get<{ open: boolean; verify: boolean; reset: boolean; invite_only: boolean; email_suffixes: string[]; captcha?: { provider: string; site_key: string } }>('/api/portal/register/policy') })
  const registering = mode === 'register'
  const registrationOpen = policy.data?.open === true && !policy.isError
  const [captcha, setCaptcha] = useState('')
  const [codeSent, setCodeSent] = useState(false)
  const [sending, setSending] = useState(false)
  const passwordLogin = oauth.data?.password_login ?? !oauth.isPending
  const showMethods = !registering || registrationOpen
  const showPasswordForm = passwordLogin && showMethods
  const loading = oauth.isPending || (registering && policy.isPending)
  const form = useForm({ initialValues: { Email: '', Password: '', Code: '', Invite: new URLSearchParams(window.location.search).get('ref') ?? '', Captcha: '' } })
  const sendCode = async () => {
    setSending(true); setError('')
    try { await api.post('/api/portal/verify/send', { Email: form.values.Email, Purpose: 'register' }); setCodeSent(true) } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setSending(false) }
  }
  const submit = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post(`/api/portal/${mode}`, { ...v, Captcha: captcha }); await refresh(); nav(next.slice('/portal'.length) || '/') } catch (e) {
      setError(e instanceof ApiError && e.status === 403 ? t('auth.closed') : e instanceof ApiError && e.status !== 401 ? e.message : t('auth.failed'))
    } finally { setBusy(false) }
  })
  const footer = (registering || registrationOpen) && <Anchor component={Link} to={`${registering ? '/login' : '/register'}?next=${encodeURIComponent(next)}`}>{t(registering ? 'auth.toLogin' : 'auth.toRegister')}</Anchor>
  return (
    <AuthLayout title={t(registering ? 'auth.registerTitle' : 'auth.loginTitle')} description={t(registering ? 'auth.registerHint' : 'auth.loginHint')} footer={footer}>
      <form onSubmit={submit} className={styles.form}>
        {loading && <div className={styles.loading} role="status"><Loader size="sm" /><span>{t('auth.loading')}</span></div>}
        {registering && policy.isError && <Alert color="red" className={styles.feedback}>{t('auth.policyFailed')} <Anchor component="button" type="button" onClick={() => policy.refetch()}>{t('auth.retry')}</Anchor></Alert>}
        {registering && !policy.isError && policy.data && !registrationOpen && <Alert color="gray" className={styles.feedback}>{t('auth.closed')}</Alert>}
        {oauth.isError && showMethods && <Alert color="red" className={styles.feedback}>{t('auth.providersFailed')} <Anchor component="button" type="button" onClick={() => oauth.refetch()}>{t('auth.retry')}</Anchor></Alert>}
        {providers.length > 0 && showMethods && <>
          <div className={styles.providers} data-stacked={providers.length === 1 || providers.some((p) => t('auth.with', { name: p.name }).length > 32) || undefined}>
            {providers.map((p) => <Button key={p.id} component="a" href={`/api/oauth/${p.id}/start?next=${encodeURIComponent(next)}`} variant="default" className={styles.provider} leftSection={<IconKey size={18} aria-hidden />}>{t('auth.with', { name: p.name })}</Button>)}
          </div>
          {passwordLogin && <Divider label={t('auth.emailAlternative')} labelPosition="center" className={styles.divider} />}
        </>}
        {!loading && oauth.data && showMethods && !passwordLogin && providers.length === 0 && <Alert color="gray" className={styles.feedback}>{t('auth.loginUnavailable')}</Alert>}
        {showPasswordForm && <>
          <TextInput className={styles.field} label={t('auth.email')} placeholder={t('auth.emailPlaceholder')} leftSection={<IconMail size={18} aria-hidden />} type="email" autoComplete="username" size="md" required {...form.getInputProps('Email')} />
          <PasswordInput inputWrapperOrder={['label', 'input', 'description', 'error']} className={styles.field} label={t('auth.password')} placeholder={t(registering ? 'auth.passwordCreatePlaceholder' : 'auth.passwordPlaceholder')} leftSection={<IconLock size={18} aria-hidden />} autoComplete={registering ? 'new-password' : 'current-password'} size="md" required minLength={8} visible={passwordVisible} onVisibilityChange={setPasswordVisible} visibilityToggleButtonProps={{ tabIndex: 0, 'aria-hidden': false, 'aria-label': t(passwordVisible ? 'auth.hidePassword' : 'auth.showPassword') }} description={registering ? t('auth.passwordHint') : undefined} {...form.getInputProps('Password')} />
          {registering && policy.data?.verify && <div className={styles.codeRow}>
            <TextInput className={`${styles.field} ${styles.codeField}`} label={t('auth.code')} size="md" required placeholder="000000" autoComplete="one-time-code" inputMode="numeric" {...form.getInputProps('Code')} />
            <Button type="button" variant="default" size="md" className={styles.codeButton} loading={sending} disabled={!form.values.Email.includes('@')} onClick={sendCode}>{codeSent ? t('auth.resend') : t('auth.sendCode')}</Button>
          </div>}
          {registering && (policy.data?.email_suffixes ?? []).length > 0 && <Text size="xs" c="dimmed" className={styles.feedback}>{t('auth.suffixes', { list: policy.data!.email_suffixes.join(', ') })}</Text>}
          {registering && <TextInput className={styles.field} leftSection={<IconTicket size={18} aria-hidden />} label={policy.data?.invite_only ? t('auth.inviteRequired') : t('auth.invite')} placeholder={t('auth.inviteHint')} required={policy.data?.invite_only} {...form.getInputProps('Invite')} />}
          {registering && policy.data?.captcha && <div className={styles.captcha}><Captcha provider={policy.data.captcha.provider} siteKey={policy.data.captcha.site_key} onToken={setCaptcha} size="compact" /></div>}
          {!registering && policy.data?.reset && <div className={styles.forgot}><Anchor component={Link} to="/forgot">{t('auth.forgot')}</Anchor></div>}
        </>}
        {error && <Alert color="red" className={styles.feedback}>{error}</Alert>}
        {showPasswordForm && <Button type="submit" size="md" autoContrast className={styles.submit} loading={busy} rightSection={<IconArrowRight size={18} aria-hidden />}>{t(`auth.${mode}`)}</Button>}
      </form>
    </AuthLayout>
  )
}
