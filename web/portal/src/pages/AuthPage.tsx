import { Alert, Anchor, Button, Divider, Loader, PasswordInput, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { IconArrowRight, IconKey } from '@tabler/icons-react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { authDestination, referralCode, rememberReferral } from '../lib/referral'
import { loginReturn } from '../lib/login-return'
import { Captcha } from '../components/Captcha'
import { RegistrationConsent } from '../components/RegistrationConsent'
import { useEmailCode } from '../lib/use-email-code'
import { registrationErrorKey, registrationErrorField, type RegistrationPolicy } from '../lib/registration'
import { AuthLayout } from '../components/AuthLayout'
import styles from '../components/AuthLayout.module.css'

export default function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const location = useLocation()
  const next = loginReturn(location.search)
  const { refresh } = useAuth()
  const [busy, setBusy] = useState(false)
  const [oauthBusy, setOAuthBusy] = useState<string | null>(null)
  const oauthAttempt = useRef(0)
  useEffect(() => () => { oauthAttempt.current += 1 }, [])
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [error, setError] = useState(() => new URLSearchParams(window.location.search).get('error') ?? '')
  const oauth = useQuery({ queryKey: ['oauth-providers'], meta: { handlesError: true }, queryFn: () => api.get<{ providers: { id: string; name: string }[]; password_login: boolean }>('/api/oauth/providers') })
  const providers = oauth.data?.providers ?? []
  const policy = useQuery({ queryKey: ['register-policy'], meta: { handlesError: true }, refetchInterval: 30_000, queryFn: () => api.get<RegistrationPolicy>('/api/portal/register/policy') })
  const registering = mode === 'register'
  const registrationOpen = policy.data?.open === true && !policy.isError
  const [captcha, setCaptcha] = useState('')
  const [captchaKey, setCaptchaKey] = useState(0)
  const [acceptedVersion, setAcceptedVersion] = useState('')
  const passwordLogin = oauth.data?.password_login ?? !oauth.isPending
  const showMethods = !registering || registrationOpen
  const showPasswordForm = passwordLogin && showMethods
  const loading = oauth.isPending || (registering && policy.isPending)
  const form = useForm({ initialValues: { Email: '', Password: '', Code: '', Invite: referralCode(location.search), Captcha: '' } })
  const delivery = useEmailCode(form.values.Email)
  const legalAccepted = !policy.data?.legal_version || acceptedVersion === policy.data.legal_version
  const resetCaptcha = () => { setCaptcha(''); setCaptchaKey(k => k + 1) }
  const showError = (e: unknown) => {
    const key = e instanceof ApiError ? registrationErrorKey(e.code, e.message) : undefined
    const message = key ? t(key) : e instanceof Error ? e.message : t('auth.failed')
    setError(message)
    const field = e instanceof ApiError ? registrationErrorField(e.code) : undefined
    if (field) form.setFieldError(field, message)
    if (e instanceof ApiError && e.code === 'legal_changed') { setAcceptedVersion(''); void policy.refetch() }
  }
  const invite = registering ? form.values.Invite.trim() : referralCode(location.search)
  const startOAuth = async (provider: string) => {
    if (registering && !legalAccepted) { setError(t('auth.legalRequired')); return }
    const attempt = ++oauthAttempt.current
    const fromURL = window.location.href
    setOAuthBusy(provider); setError('')
    try {
      await rememberReferral(invite)
      if (oauthAttempt.current !== attempt || window.location.href !== fromURL) return
      const start = `/api/oauth/${encodeURIComponent(provider)}/start?next=${encodeURIComponent(next)}`
      if (registering && policy.data?.legal_version) {
        const result = await api.post<{ url: string }>(start, { LegalAccepted: true, LegalVersion: policy.data.legal_version })
        if (oauthAttempt.current !== attempt || window.location.href !== fromURL) return
        window.location.assign(result.url)
      } else window.location.assign(start)
    } catch (e) { if (oauthAttempt.current === attempt) { if (e instanceof ApiError) showError(e); else setError(t('auth.inviteSaveFailed')); setOAuthBusy(null) } }
  }
  const sendCode = async () => {
    setError('')
    try { await delivery.send(() => api.post('/api/portal/verify/send', { Email: form.values.Email, Purpose: 'register', Captcha: captcha })) }
    catch (e) { showError(e) } finally { resetCaptcha() }
  }
  const submit = form.onSubmit(async (v) => {
    if (registering && !legalAccepted) { setError(t('auth.legalRequired')); return }
    setBusy(true); setError(''); form.clearErrors()
    try { await api.post(`/api/portal/${mode}`, { ...v, Captcha: captcha, LegalAccepted: legalAccepted, LegalVersion: policy.data?.legal_version ?? '' }); await refresh(); nav(next.slice('/portal'.length) || '/') }
    catch (e) { if (!registering && e instanceof ApiError && e.status === 401) setError(t('auth.failed')); else showError(e) }
    finally { setBusy(false); if (registering) resetCaptcha() }
  })
  const footer = (registering || registrationOpen) && <Anchor component={Link} to={authDestination(registering ? '/login' : '/register', next, invite)}>{t(registering ? 'auth.toLogin' : 'auth.toRegister')}</Anchor>
  return (
    <AuthLayout title={t(registering ? 'auth.registerTitle' : 'auth.loginTitle')} description={t(registering ? 'auth.registerHint' : 'auth.loginHint')} footer={footer}>
      <form onSubmit={submit} className={styles.form}>
        {loading && <div className={styles.loading} role="status"><Loader size="sm" /><span>{t('auth.loading')}</span></div>}
        {registering && policy.isError && <Alert color="red" className={styles.feedback}>{t('auth.policyFailed')} <Anchor component="button" type="button" onClick={() => policy.refetch()}>{t('auth.retry')}</Anchor></Alert>}
        {registering && !policy.isError && policy.data && !registrationOpen && <Alert color="gray" className={styles.feedback}>{t('auth.closed')}</Alert>}
        {oauth.isError && showMethods && <Alert color="red" className={styles.feedback}>{t('auth.providersFailed')} <Anchor component="button" type="button" onClick={() => oauth.refetch()}>{t('auth.retry')}</Anchor></Alert>}
        {!loading && oauth.data && showMethods && !passwordLogin && providers.length === 0 && <Alert color="gray" className={styles.feedback}>{t('auth.loginUnavailable')}</Alert>}
        {showPasswordForm && <>
          <TextInput className={styles.field} label={t('auth.email')} placeholder={t('auth.emailPlaceholder')} type="email" autoComplete="username" size="md" required {...form.getInputProps('Email')} onChange={event => { form.setFieldValue('Email', event.currentTarget.value); form.setFieldValue('Code', ''); form.clearFieldError('Email') }} />
          <div className={styles.passwordField}>
            {!registering && policy.data?.reset && <div className={styles.forgot}><Anchor component={Link} to={authDestination('/forgot', next, invite)}>{t('auth.forgot')}</Anchor></div>}
            <PasswordInput inputWrapperOrder={['label', 'input', 'description', 'error']} className={styles.field} label={t('auth.password')} placeholder={t(registering ? 'auth.passwordCreatePlaceholder' : 'auth.passwordPlaceholder')} autoComplete={registering ? 'new-password' : 'current-password'} size="md" required minLength={8} visible={passwordVisible} onVisibilityChange={setPasswordVisible} visibilityToggleButtonProps={{ tabIndex: 0, 'aria-hidden': false, 'aria-label': t(passwordVisible ? 'auth.hidePassword' : 'auth.showPassword'), onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => { if (event.key === 'Enter') { event.preventDefault(); setPasswordVisible((visible) => !visible) } } }} description={registering ? t('auth.passwordHint') : undefined} {...form.getInputProps('Password')} />
          </div>
          {registering && policy.data?.verify && <div className={styles.codeRow}>
            <TextInput className={`${styles.field} ${styles.codeField}`} label={t('auth.code')} size="md" required placeholder="000000" autoComplete="one-time-code" inputMode="numeric" {...form.getInputProps('Code')} />
            <Button type="button" variant="default" size="md" className={styles.codeButton} loading={delivery.sending} disabled={busy || delivery.seconds > 0 || !form.values.Email.includes('@') || (!!policy.data?.captcha && !captcha)} onClick={sendCode}>{delivery.seconds > 0 ? t('auth.resendSeconds', { seconds: delivery.seconds }) : delivery.sent ? t('auth.resend') : t('auth.sendCode')}</Button>
          </div>}
          {registering && delivery.sent && <Text size="xs" role="status">{t('auth.codeSentTo', { email: form.values.Email.trim() })}</Text>}
          {registering && (policy.data?.email_suffixes ?? []).length > 0 && <Text size="xs" c="dimmed" className={styles.feedback}>{t('auth.suffixes', { list: policy.data!.email_suffixes.join(', ') })}</Text>}
          {registering && <TextInput className={styles.field} label={policy.data?.invite_only ? t('auth.inviteRequired') : t('auth.invite')} placeholder={t('auth.inviteHint')} required={policy.data?.invite_only} {...form.getInputProps('Invite')} />}
          {registering && policy.data?.captcha && <div className={styles.captcha}><Captcha key={captchaKey} provider={policy.data.captcha.provider} siteKey={policy.data.captcha.site_key} onToken={setCaptcha} size="compact" /></div>}
        </>}
        {registering && <RegistrationConsent policy={policy.data} checked={legalAccepted} onChange={checked => setAcceptedVersion(checked ? policy.data?.legal_version ?? '' : '')} />}
        {error && <Alert color="red" className={styles.feedback}>{error}</Alert>}
        {showPasswordForm && <Button type="submit" size="md" autoContrast className={styles.submit} loading={busy} disabled={oauthBusy !== null || delivery.sending || (registering && (!legalAccepted || (!!policy.data?.captcha && !captcha)))} rightSection={<IconArrowRight size={18} aria-hidden />}>{t(`auth.${mode}`)}</Button>}
        {providers.length > 0 && showMethods && <>
          {passwordLogin && <Divider label={t('auth.otherMethods')} labelPosition="center" className={styles.divider} />}
          <div className={styles.providers} data-stacked={providers.length === 1 || providers.some((p) => t('auth.with', { name: p.name }).length > 32) || undefined}>
            {providers.map((p) => <Button key={p.id} type="button" onClick={() => void startOAuth(p.id)} loading={oauthBusy === p.id} disabled={busy || (registering && !legalAccepted) || (oauthBusy !== null && oauthBusy !== p.id)} variant="default" className={styles.provider} leftSection={<IconKey size={17} stroke={1.5} aria-hidden />}>{t('auth.with', { name: p.name })}</Button>)}
          </div>
        </>}
      </form>
    </AuthLayout>
  )
}
