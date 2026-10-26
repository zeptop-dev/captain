import { Button, Card, Center, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { usePublicBrand } from '../lib/branding'
import { useAuth } from '../lib/auth'
import { Brand } from '../components/AppLayout'
import { loginWithPasskey, passkeysAvailable } from '../lib/passkeys'
import { pageBackground } from '../theme'

export default function LoginPage() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { refresh } = useAuth()
  const { data: site } = usePublicBrand()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [needCode, setNeedCode] = useState(false)
  const form = useForm({ initialValues: { Email: '', Password: '', Code: '' } })
  const submit = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post('/api/admin/login', v); await refresh(); nav('/') } catch (e) { if (e instanceof ApiError && e.status === 428) setNeedCode(true); else setError(needCode ? t('login.badCode') : t('login.failed')) } finally { setBusy(false) }
  })
  const passkeyLogin = async () => {
    setBusy(true); setError('')
    try { await loginWithPasskey(form.values.Code); await refresh(); nav('/') }
    catch (e) { if (e instanceof ApiError && e.status === 428) { setNeedCode(true); setError(t('passkeys.needCode')) } else setError(t('passkeys.failed')) }
    finally { setBusy(false) }
  }
  return (
    <Center h="100vh" p="md" style={{ background: pageBackground }}>
      <Card w={400} p="xl">
        <form onSubmit={submit}>
          <Stack>
            <Brand size="lg" />
            <div>
              <Title order={3}>{t('login.title', { name: site?.name || 'Captain' })}</Title>
              <Text c="dimmed" size="sm">{t('login.subtitle')}</Text>
            </div>
            <TextInput label={t('login.email')} type="email" required autoFocus {...form.getInputProps('Email')} />
            <PasswordInput label={t('login.password')} required {...form.getInputProps('Password')} />
            {needCode && <TextInput label={t('login.code')} placeholder="123456" autoFocus {...form.getInputProps('Code')} />}
            {error && <Text c="red" size="sm">{error}</Text>}
            <Button type="submit" loading={busy}>{t('login.submit')}</Button>
            {passkeysAvailable() && <Button type="button" variant="default" disabled={busy} onClick={passkeyLogin}>{t('passkeys.login')}</Button>}
          </Stack>
        </form>
      </Card>
    </Center>
  )
}
