import { Alert, Button, Card, Center, Group, Loader, Stack, Text, Title } from '@mantine/core'
import { useQuery, useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'
import { useState } from 'react'
import { api, ApiError } from '../lib/api'
import { useSite } from '../lib/theme'

export default function AuthorizePage() {
  const { t } = useTranslation()
  const site = useSite()
  const brand = site.data?.name || 'Captain'
  const { search } = useLocation()
  const [error, setError] = useState('')
  const q = useQuery({ queryKey: ['login-authorization', search], meta: { handlesError: true }, queryFn: () => api.get<{ ticket: string; account: string; name: string; redirect_uri: string; scope: string }>(`/api/login-provider/request${search}`), retry: false, staleTime: Infinity, refetchOnWindowFocus: false, gcTime: 0 })
  const decision = useMutation({ mutationFn: (allow: boolean) => api.post<{ redirect: string }>('/api/login-provider/decision', { ticket: q.data!.ticket, allow }), onSuccess: r => { window.location.assign(r.redirect) }, onError: () => setError(t('loginConsent.expired')) })
  const next = `/portal/authorize${search}`
  return <Center mih="100vh" p="md"><Card w={480} maw="100%"><Stack>
    <Title order={3}>{t('loginConsent.title', { brand })}</Title>
    {q.isPending && <Loader />}
    {q.error instanceof ApiError && q.error.status === 401 ? <><Text>{t('loginConsent.login', { brand })}</Text><Button component={Link} to={`/login?next=${encodeURIComponent(next)}`}>{t('auth.login')}</Button></> : q.isError ? <Alert color="red">{t('loginConsent.invalid')}</Alert> : null}
    {q.data && <><Text fw={600}>{q.data.name}</Text><Text size="sm">{q.data.account}</Text><Text>{t('loginConsent.profile')}</Text>{q.data.scope.split(' ').includes('email') && <Text>{t('loginConsent.email')}</Text>}<Text size="sm" c="dimmed">{t('loginConsent.callback')}</Text><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{q.data.redirect_uri}</Text><Text size="sm">{t('loginConsent.boundary')}</Text><Group><Button loading={decision.isPending} disabled={!!error} onClick={() => decision.mutate(true)}>{t('loginConsent.allow')}</Button><Button variant="default" disabled={decision.isPending || !!error} onClick={() => decision.mutate(false)}>{t('loginConsent.deny')}</Button></Group></>}
    {error && <Alert color="red">{error}</Alert>}
  </Stack></Card></Center>
}
