import { Button, Card, Center, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'

export default function LoginPage() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { refresh } = useAuth()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [needCode, setNeedCode] = useState(false)
  const form = useForm({ initialValues: { Email: '', Password: '', Code: '' } })
  const submit = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post('/api/admin/login', v); await refresh(); nav('/') } catch (e) { if (e instanceof ApiError && e.status === 428) setNeedCode(true); else setError(needCode ? t('login.badCode') : t('login.failed')) } finally { setBusy(false) }
  })
  return (
    <Center h="100vh" p="md">
      <Card w={380} p="xl">
        <form onSubmit={submit}>
          <Stack>
            <div>
              <Title order={3}>{t('login.title')}</Title>
              <Text c="dimmed" size="sm">{t('login.subtitle')}</Text>
            </div>
            <TextInput label={t('login.email')} type="email" required autoFocus {...form.getInputProps('Email')} />
            <PasswordInput label={t('login.password')} required {...form.getInputProps('Password')} />
            {needCode && <TextInput label={t('login.code')} placeholder="123456" autoFocus {...form.getInputProps('Code')} />}
            {error && <Text c="red" size="sm">{error}</Text>}
            <Button type="submit" loading={busy}>{t('login.submit')}</Button>
          </Stack>
        </form>
      </Card>
    </Center>
  )
}
