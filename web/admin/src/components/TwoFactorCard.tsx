import { Button, Card, Code, Group, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation } from '@tanstack/react-query'
import { QRCodeSVG } from 'qrcode.react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'

// TOTP for the signed-in staff account: scan, confirm with a code, done.
export function TwoFactorCard() {
  const { t } = useTranslation()
  const { me, refresh } = useAuth()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState('')
  const start = useMutation({ mutationFn: () => api.post<{ secret: string; uri: string }>('/api/admin/2fa/setup'), onSuccess: setSetup, onError: toast.err })
  const enable = useMutation({ mutationFn: () => api.post('/api/admin/2fa/enable', { Code: code }), onSuccess: () => { toast.ok(t('twofa.enabled')); setSetup(null); setCode(''); refresh() }, onError: toast.err })
  const disable = useMutation({ mutationFn: () => api.post('/api/admin/2fa/disable', { Code: code }), onSuccess: () => { toast.ok(t('twofa.disabled')); setCode(''); refresh() }, onError: toast.err })
  const on = !!(me as { totp?: boolean } | null)?.totp
  return (
    <Card>
      <Title order={5} mb="xs">{t('twofa.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('twofa.hint')}</Text>
      {on ? (
        <Group align="flex-end"><TextInput label={t('twofa.code')} placeholder="123456" w={140} value={code} onChange={(e) => setCode(e.currentTarget.value)} /><Button size="xs" mb={2} color="red" variant="light" loading={disable.isPending} onClick={() => disable.mutate()}>{t('twofa.disable')}</Button><Text size="xs" c="teal" mb={8}>{t('twofa.on')}</Text></Group>
      ) : setup ? (
        <Stack gap="sm">
          <Group align="flex-start" gap="lg">
            <QRCodeSVG value={setup.uri} size={140} />
            <Stack gap={4}><Text size="sm">{t('twofa.scan')}</Text><Text size="xs" c="dimmed">{t('twofa.manual')}</Text><Code>{setup.secret}</Code></Stack>
          </Group>
          <Group align="flex-end"><TextInput label={t('twofa.code')} placeholder="123456" w={140} value={code} onChange={(e) => setCode(e.currentTarget.value)} /><Button size="xs" mb={2} loading={enable.isPending} disabled={code.length < 6} onClick={() => enable.mutate()}>{t('twofa.confirm')}</Button></Group>
        </Stack>
      ) : (
        <Button size="xs" variant="light" loading={start.isPending} onClick={() => start.mutate()}>{t('twofa.start')}</Button>
      )}
    </Card>
  )
}
