import { ActionIcon, Badge, Button, Card, Code, Group, Stack, Table, Text, TextInput, Textarea, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconRefresh, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface Cert { id: number; domain: string; names: string[]; not_after: string; source: string; updated_at: string }

// Operator-supplied certificates: pasted PEM pairs or renewals delivered by
// a certificate manager's webhook. Nodes get the ones their inbounds use.
export function CertificatesCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['certificates'], queryFn: () => api.get<{ certificates: Cert[]; webhook_url: string }>('/api/admin/certificates') })
  const [domain, setDomain] = useState('')
  const [cert, setCert] = useState('')
  const [key, setKey] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['certificates'] })
  const upload = useMutation({ mutationFn: () => api.post('/api/admin/certificates', { Domain: domain, CertPEM: cert, KeyPEM: key }), onSuccess: () => { toast.ok(t('common.saved')); setDomain(''); setCert(''); setKey(''); refresh() }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/certificates/${id}`), onSuccess: refresh, onError: toast.err })
  const rotate = useMutation({ mutationFn: () => api.post('/api/admin/certificates/webhook-token'), onSuccess: refresh, onError: toast.err })
  const soon = (d: string) => new Date(d).getTime() - Date.now() < 14 * 86400e3
  return (
    <Card>
      <Title order={5} mb="xs">{t('certs.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('certs.hint')}</Text>
      <Stack gap="sm">
        {(q.data?.certificates.length ?? 0) > 0 && (
          <Table fz="sm"><Table.Tbody>
            {q.data!.certificates.map((c) => (
              <Table.Tr key={c.id}>
                <Table.Td><Code>{c.domain}</Code>{c.names.filter((n) => n !== c.domain).length > 0 && <Text size="xs" c="dimmed">{c.names.filter((n) => n !== c.domain).join(', ')}</Text>}</Table.Td>
                <Table.Td><Badge size="xs" variant="outline" color="gray">{c.source}</Badge></Table.Td>
                <Table.Td><Text size="xs" c={soon(c.not_after) ? 'red' : 'dimmed'}>{t('certs.expires', { date: when(c.not_after).split(',')[0] })}</Text></Table.Td>
                <Table.Td><ActionIcon variant="subtle" color="red" size="sm" onClick={() => del.mutate(c.id)}><IconTrash size={14} /></ActionIcon></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody></Table>
        )}
        <TextInput label={t('certs.domain')} description={t('certs.domainHint')} placeholder="*.example.com" value={domain} onChange={(e) => setDomain(e.currentTarget.value)} />
        <Group grow align="flex-start">
          <Textarea label={t('certs.cert')} placeholder="-----BEGIN CERTIFICATE-----" autosize minRows={3} maxRows={6} value={cert} onChange={(e) => setCert(e.currentTarget.value)} styles={{ input: { fontFamily: 'monospace', fontSize: 11 } }} />
          <Textarea label={t('certs.key')} placeholder="-----BEGIN PRIVATE KEY-----" autosize minRows={3} maxRows={6} value={key} onChange={(e) => setKey(e.currentTarget.value)} styles={{ input: { fontFamily: 'monospace', fontSize: 11 } }} />
        </Group>
        <Group justify="flex-end"><Button size="xs" loading={upload.isPending} disabled={!cert || !key} onClick={() => upload.mutate()}>{t('certs.upload')}</Button></Group>
        <div>
          <Text size="sm" fw={600}>{t('certs.webhook')}</Text>
          <Text size="xs" c="dimmed" mb={4}>{t('certs.webhookHint')}</Text>
          {q.data?.webhook_url ? (
            <Group gap={4} wrap="nowrap"><Code style={{ wordBreak: 'break-all' }}>{q.data.webhook_url}</Code><Copy value={q.data.webhook_url} /><ActionIcon variant="subtle" size="sm" onClick={() => rotate.mutate()} title={t('certs.rotate')}><IconRefresh size={14} /></ActionIcon></Group>
          ) : (
            <Button size="xs" variant="light" loading={rotate.isPending} onClick={() => rotate.mutate()}>{t('certs.enableWebhook')}</Button>
          )}
          {q.data?.webhook_url && <Code block mt={6} style={{ fontSize: 11 }}>{'{"domain": "${DOMAINS}", "certificate": "${CERTIFICATE}", "privateKey": "${PRIVATE_KEY}"}'}</Code>}
        </div>
      </Stack>
    </Card>
  )
}
