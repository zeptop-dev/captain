import { Badge, Button, Card, Group, Modal, Paper, SegmentedControl, Stack, Table, Text, Textarea } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { ago, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Msg { id: number; from_admin: boolean; body: string; created_at: string }
interface Ticket { id: number; user_id: number; email: string; subject: string; status: string; priority: string; messages: number; created_at: string; updated_at: string; thread?: Msg[] }

const colors: Record<string, string> = { open: 'orange', replied: 'teal', closed: 'gray' }
const prio: Record<string, string> = { low: 'gray', normal: 'blue', high: 'red' }

export default function TicketsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [status, setStatus] = useState('open')
  const q = useQuery({ queryKey: ['tickets', status], queryFn: () => api.get<{ items: Ticket[]; total: number }>(`/api/admin/tickets?status=${status === 'all' ? '' : status}&limit=100`), refetchInterval: 30_000 })
  const [openId, setOpenId] = useState<number | null>(null)
  const detail = useQuery({ queryKey: ['ticket', openId], queryFn: () => api.get<Ticket>(`/api/admin/tickets/${openId}`), enabled: openId !== null })
  const [reply, setReply] = useState('')
  const done = () => { qc.invalidateQueries({ queryKey: ['tickets'] }); qc.invalidateQueries({ queryKey: ['ticket', openId] }); qc.invalidateQueries({ queryKey: ['dashboard'] }) }
  const send = useMutation({ mutationFn: () => api.post(`/api/admin/tickets/${openId}/reply`, { Body: reply }), onSuccess: () => { setReply(''); toast.ok(t('tickets.sent')); done() }, onError: toast.err })
  const setSt = useMutation({ mutationFn: (s: string) => api.post(`/api/admin/tickets/${openId}/status`, { Status: s }), onSuccess: done, onError: toast.err })
  const d = detail.data
  return (
    <>
      <PageHeader title={t('tickets.title')} subtitle={t('tickets.subtitle')} actions={<SegmentedControl value={status} onChange={setStatus} data={[{ value: 'open', label: t('tickets.status.open') }, { value: 'replied', label: t('tickets.status.replied') }, { value: 'closed', label: t('tickets.status.closed') }, { value: 'all', label: t('common.all') }]} />} />
      <Card p={0}><Table.ScrollContainer minWidth={640}><Table highlightOnHover>
        <Table.Thead><Table.Tr><Table.Th>#</Table.Th><Table.Th>{t('tickets.subject')}</Table.Th><Table.Th>{t('tickets.user')}</Table.Th><Table.Th>{t('tickets.priority')}</Table.Th><Table.Th>{t('tickets.statusCol')}</Table.Th><Table.Th>{t('tickets.updated')}</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data?.items ?? []).map((tk) => (
            <Table.Tr key={tk.id} style={{ cursor: 'pointer' }} onClick={() => setOpenId(tk.id)}>
              <Table.Td><Text size="sm" c="dimmed">{tk.id}</Text></Table.Td>
              <Table.Td><Text size="sm" fw={600}>{tk.subject}</Text><Text size="xs" c="dimmed">{t('tickets.count', { count: tk.messages })}</Text></Table.Td>
              <Table.Td><Text size="sm">{tk.email}</Text></Table.Td>
              <Table.Td><Badge size="sm" variant="light" color={prio[tk.priority]}>{t(`tickets.prio.${tk.priority}`)}</Badge></Table.Td>
              <Table.Td><Badge color={colors[tk.status]}>{t(`tickets.status.${tk.status}`)}</Badge></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{ago(tk.updated_at)}</Text></Table.Td>
            </Table.Tr>
          ))}
          {(q.data?.items ?? []).length === 0 && <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Table.ScrollContainer></Card>

      <Modal opened={openId !== null} onClose={() => setOpenId(null)} title={d ? `#${d.id} ${d.subject}` : ''} size="lg">
        {d && (
          <Stack>
            <Group gap="xs"><Text size="sm">{d.email}</Text><Badge color={colors[d.status]}>{t(`tickets.status.${d.status}`)}</Badge><Badge variant="light" color={prio[d.priority]}>{t(`tickets.prio.${d.priority}`)}</Badge></Group>
            <Stack gap="xs" mah={360} style={{ overflowY: 'auto' }}>
              {(d.thread ?? []).map((m) => (
                <Paper key={m.id} p="sm" radius="md" bg={m.from_admin ? 'var(--mantine-color-cyan-0)' : 'var(--mantine-color-gray-0)'} style={{ alignSelf: m.from_admin ? 'flex-end' : 'flex-start', maxWidth: '90%' }}>
                  <Text size="xs" c="dimmed" mb={4}>{m.from_admin ? t('tickets.you') : d.email} · {when(m.created_at)}</Text>
                  <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>{m.body}</Text>
                </Paper>
              ))}
            </Stack>
            <Textarea placeholder={t('tickets.replyHint')} autosize minRows={3} value={reply} onChange={(e) => setReply(e.currentTarget.value)} />
            <Group justify="space-between">
              {d.status === 'closed'
                ? <Button variant="subtle" onClick={() => setSt.mutate('open')}>{t('tickets.reopen')}</Button>
                : <Button variant="subtle" color="gray" onClick={() => setSt.mutate('closed')}>{t('tickets.close')}</Button>}
              <Button disabled={!reply.trim()} loading={send.isPending} onClick={() => send.mutate()}>{t('tickets.reply')}</Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </>
  )
}
