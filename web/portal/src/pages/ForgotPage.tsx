import { Anchor, Button, Card, Center, Group, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'

// Password reset by emailed code: request, then code + new password.
export default function ForgotPage() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [sent, setSent] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { Email: '', Code: '', Password: '' } })
  const send = async () => {
    setBusy(true); setError('')
    try { await api.post('/api/portal/verify/send', { Email: form.values.Email, Purpose: 'reset' }); setSent(true) } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  }
  const reset = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post('/api/portal/password/reset', v); nav('/login') } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  })
  return (
    <Center mih="100vh" p="md" bg="var(--mantine-color-gray-0)">
      <Card w={400}>
        <form onSubmit={reset}><Stack>
          <Title order={2}>{t('auth.forgotTitle')}</Title>
          <Text size="sm" c="dimmed">{t('auth.forgotHint')}</Text>
          <Group align="flex-end" wrap="nowrap">
            <TextInput flex={1} label={t('auth.email')} type="email" size="md" required {...form.getInputProps('Email')} />
            <Button variant="default" size="md" loading={busy && !sent} disabled={!form.values.Email.includes('@')} onClick={send}>{sent ? t('auth.resend') : t('auth.sendCode')}</Button>
          </Group>
          {sent && <>
            <TextInput label={t('auth.code')} size="md" required placeholder="000000" {...form.getInputProps('Code')} />
            <PasswordInput label={t('auth.newPassword')} size="md" required minLength={8} {...form.getInputProps('Password')} />
            <Button type="submit" size="md" loading={busy}>{t('auth.resetSubmit')}</Button>
          </>}
          {error && <Text c="red" size="sm">{error}</Text>}
          <Text size="sm" ta="center"><Anchor component={Link} to="/login">{t('auth.toLogin')}</Anchor></Text>
        </Stack></form>
      </Card>
    </Center>
  )
}
