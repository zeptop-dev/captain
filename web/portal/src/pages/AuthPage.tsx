import { Anchor, Button, Card, Center, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'

export default function AuthPage({ mode }: { mode: 'login' | 'register' }) {
  const { t } = useTranslation()
  const nav = useNavigate()
  const { refresh } = useAuth()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { Email: '', Password: '' } })
  const submit = form.onSubmit(async (v) => {
    setBusy(true); setError('')
    try { await api.post(`/api/portal/${mode}`, v); refresh(); nav('/') } catch (e) {
      setError(e instanceof ApiError && e.status === 403 ? t('auth.closed') : e instanceof ApiError && e.status !== 401 ? e.message : t('auth.failed'))
    } finally { setBusy(false) }
  })
  return (
    <Center mih="100vh" p="md" bg="var(--mantine-color-gray-0)">
      <Card w={400}>
        <form onSubmit={submit}><Stack>
          <Title order={2}>{t(`auth.${mode}`)}</Title>
          <TextInput label={t('auth.email')} type="email" size="md" required autoFocus {...form.getInputProps('Email')} />
          <PasswordInput label={t('auth.password')} size="md" required minLength={8} description={mode === 'register' ? t('auth.passwordHint') : undefined} {...form.getInputProps('Password')} />
          {error && <Text c="red" size="sm">{error}</Text>}
          <Button type="submit" size="md" loading={busy}>{t(`auth.${mode}`)}</Button>
          <Text size="sm" ta="center"><Anchor component={Link} to={mode === 'login' ? '/register' : '/login'}>{t(mode === 'login' ? 'auth.toRegister' : 'auth.toLogin')}</Anchor></Text>
        </Stack></form>
      </Card>
    </Center>
  )
}
