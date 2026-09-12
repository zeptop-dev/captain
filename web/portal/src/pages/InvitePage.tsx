import { Button, Card, Code, Group, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Invite } from '../lib/api'
import { money } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from '../components/Copy'

export default function InvitePage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['invite'], queryFn: () => api.get<Invite>('/api/portal/invite') })
  const [code, setCode] = useState('')
  const bind = useMutation({ mutationFn: () => api.post('/api/portal/invite/bind', { Code: code }), onSuccess: () => { toast.ok(t('invite.bound')); qc.invalidateQueries({ queryKey: ['invite'] }) }, onError: toast.err })
  const d = q.data
  if (!d) return null
  return (
    <Stack gap="lg">
      <Title order={2}>{t('invite.title')}</Title>
      <Card>
        <Text size="sm" c="dimmed">{d.enabled ? t('invite.hint', { percent: d.percent }) : t('invite.disabledHint')}</Text>
        <Group gap={4} mt="sm" wrap="nowrap"><Code style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>{d.url}</Code><Copy value={d.url} /></Group>
        <Text size="xs" c="dimmed" mt={4}>{t('invite.code')}: <b>{d.code}</b></Text>
      </Card>
      <SimpleGrid cols={2}>
        <Card><Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('invite.invited')}</Text><Text fz="xl" fw={700}>{d.invited}</Text></Card>
        <Card><Text size="xs" tt="uppercase" c="dimmed" fw={700}>{t('invite.earned')}</Text><Text fz="xl" fw={700}>{money(d.earned_cents)}</Text></Card>
      </SimpleGrid>
      {!d.invited_by && (
        <Card>
          <Text size="sm" fw={600}>{t('invite.bindTitle')}</Text>
          <Text size="xs" c="dimmed">{t('invite.bindHint')}</Text>
          <Group mt="sm" align="flex-end"><TextInput flex={1} placeholder="ABCD2345" value={code} onChange={(e) => setCode(e.currentTarget.value.toUpperCase())} /><Button disabled={!code} loading={bind.isPending} onClick={() => bind.mutate()}>{t('invite.bind')}</Button></Group>
        </Card>
      )}
    </Stack>
  )
}
