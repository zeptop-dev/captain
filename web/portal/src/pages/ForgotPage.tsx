import { Alert, Anchor, Button, PasswordInput, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { IconArrowLeft, IconArrowRight } from '@tabler/icons-react'
import { useState, type KeyboardEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { authDestination, referralCode } from '../lib/referral'
import { loginReturn } from '../lib/login-return'
import { api } from '../lib/api'
import { AuthLayout } from '../components/AuthLayout'
import styles from '../components/AuthLayout.module.css'

// Password reset by emailed code: request, then code + new password.
export default function ForgotPage() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const location = useLocation()
  const login = authDestination('/login', loginReturn(location.search), referralCode(location.search))
  const [sent, setSent] = useState(false)
  const [busy, setBusy] = useState(false)
  const [passwordVisible, setPasswordVisible] = useState(false)
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { Email: '', Code: '', Password: '' } })
  const send = async () => {
    setBusy(true); setError('')
    try { await api.post('/api/portal/verify/send', { Email: form.values.Email, Purpose: 'reset' }); setSent(true) } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  }
  const reset = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post('/api/portal/password/reset', v); nav(login) } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  })
  return (
    <AuthLayout title={t('auth.forgotTitle')} description={t('auth.forgotHint')} footer={<Anchor component={Link} to={login}><IconArrowLeft size={14} style={{ verticalAlign: 'middle', marginRight: 6 }} aria-hidden />{t('auth.backToLogin')}</Anchor>}>
      <form onSubmit={sent ? reset : (e) => { e.preventDefault(); void send() }} className={styles.form}>
        <TextInput className={styles.field} label={t('auth.email')} placeholder={t('auth.emailPlaceholder')} type="email" autoComplete="username" size="md" required {...form.getInputProps('Email')} />
        {sent && <>
          <div className={styles.codeRow}>
            <TextInput className={`${styles.field} ${styles.codeField}`} label={t('auth.code')} size="md" required placeholder="000000" inputMode="numeric" autoComplete="one-time-code" {...form.getInputProps('Code')} />
            <Button type="button" variant="default" size="md" className={styles.codeButton} loading={busy} disabled={!form.values.Email.includes('@')} onClick={send}>{t('auth.resend')}</Button>
          </div>
          <PasswordInput inputWrapperOrder={['label', 'input', 'description', 'error']} className={styles.field} label={t('auth.newPassword')} placeholder={t('auth.passwordCreatePlaceholder')} size="md" required minLength={8} autoComplete="new-password" visible={passwordVisible} onVisibilityChange={setPasswordVisible} visibilityToggleButtonProps={{ tabIndex: 0, 'aria-hidden': false, 'aria-label': t(passwordVisible ? 'auth.hidePassword' : 'auth.showPassword'), onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => { if (event.key === 'Enter') { event.preventDefault(); setPasswordVisible((visible) => !visible) } } }} description={t('auth.passwordHint')} {...form.getInputProps('Password')} />
        </>}
        {error && <Alert color="red" className={styles.feedback}>{error}</Alert>}
        <Button type="submit" size="md" autoContrast className={styles.submit} loading={busy} rightSection={<IconArrowRight size={18} aria-hidden />}>{t(sent ? 'auth.resetSubmit' : 'auth.sendCode')}</Button>
      </form>
    </AuthLayout>
  )
}
