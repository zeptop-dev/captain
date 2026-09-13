import { Badge, Button, Card, Group, Modal, Paper, Select, Stack, Text, Textarea, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'

interface Msg { id: number; from_admin: boolean; body: string; created_at: string }
interface Ticket { id: number; subject: string; status: string; priority: string; messages: number | Msg[]; created_at: string; updated_at: string }

const colors: Record<string, string> = { open: 'orange', replied: 'teal', closed: 'gray' }

export default function TicketsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ['tickets'], queryFn: () => api.get<Ticket[]>('/api/portal/tickets'), refetchInterval: 30_000 })
  const [openId, setOpenId] = useState<number | null>(null)
  const [creating, setCreating] = useState(false)
  const detail = useQuery({ queryKey: ['ticket', openId], queryFn: () => api.get<Ticket>(`/api/portal/tickets/${openId}`), enabled: openId !== null, refetchInterval: 15_000 })
  const form = useForm({ initialValues: { Subject: '', Priority: 'normal', Body: '' } })
  const create = useMutation({ mutationFn: (v: typeof form.values) => api.post<Ticket>('/api/portal/tickets', v), onSuccess: (tk) => { toast.ok(t('tickets.created')); form.reset(); setCreating(false); qc.invalidateQueries({ queryKey: ['tickets'] }); setOpenId(tk.id) }, onError: toast.err })
  const [reply, setReply] = useState('')
  const send = useMutation({ mutationFn: () => api.post(`/api/portal/tickets/${openId}/reply`, { Body: reply }), onSuccess: () => { setReply(''); qc.invalidateQueries({ queryKey: ['ticket', openId] }); qc.invalidateQueries({ queryKey: ['tickets'] }) }, onError: toast.err })
  const close = useMutation({ mutationFn: () => api.post(`/api/portal/tickets/${openId}/close`), onSuccess: () => { qc.invalidateQueries({ queryKey: ['ticket', openId] }); qc.invalidateQueries({ queryKey: ['tickets'] }) }, onError: toast.err })
  const d = detail.data
  const thread = (d && Array.isArray(d.messages) ? d.messages : []) as Msg[]
  return (
    <Stack gap="lg">
      <Group justify="space-between"><Title order={2}>{t('tickets.title')}</Title><Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>{t('tickets.create')}</Button></Group>
      {list.data?.length === 0 && <Text c="dimmed">{t('tickets.empty')}</Text>}
      {(list.data ?? []).map((tk) => (
        <Card key={tk.id} padding="md" style={{ cursor: 'pointer' }} onClick={() => setOpenId(tk.id)}>
          <Group justify="space-between" wrap="nowrap">
            <div style={{ minWidth: 0 }}><Text fw={700} truncate>#{tk.id} {tk.subject}</Text><Text size="xs" c="dimmed">{when(tk.updated_at)} · {t('tickets.count', { count: typeof tk.messages === 'number' ? tk.messages : tk.messages.length })}</Text></div>
            <Badge color={colors[tk.status] ?? 'gray'}>{t(`tickets.status.${tk.status}`)}</Badge>
          </Group>
        </Card>
      ))}

      <Modal opened={creating} onClose={() => setCreating(false)} title={t('tickets.create')}>
        <form onSubmit={form.onSubmit((v) => create.mutate(v))}><Stack>
          <TextInput label={t('tickets.subject')} required maxLength={200} {...form.getInputProps('Subject')} />
          <Select label={t('tickets.priority')} data={[{ value: 'low', label: t('tickets.prio.low') }, { value: 'normal', label: t('tickets.prio.normal') }, { value: 'high', label: t('tickets.prio.high') }]} allowDeselect={false} {...form.getInputProps('Priority')} />
          <Textarea label={t('tickets.message')} required autosize minRows={4} {...form.getInputProps('Body')} />
          <Group justify="flex-end"><Button type="submit" loading={create.isPending}>{t('tickets.submit')}</Button></Group>
        </Stack></form>
      </Modal>

      <Modal opened={openId !== null} onClose={() => setOpenId(null)} title={d ? `#${d.id} ${d.subject}` : ''} size="lg">
        {d && (
          <Stack>
            <Group gap="xs"><Badge color={colors[d.status] ?? 'gray'}>{t(`tickets.status.${d.status}`)}</Badge><Badge variant="outline" color="gray">{t(`tickets.prio.${d.priority}`)}</Badge></Group>
            <Stack gap="xs">
              {thread.map((m) => (
                <Paper key={m.id} p="sm" radius="md" bg={m.from_admin ? 'var(--mantine-color-cyan-0)' : 'var(--mantine-color-gray-0)'} style={{ alignSelf: m.from_admin ? 'flex-start' : 'flex-end', maxWidth: '90%' }}>
                  <Text size="xs" c="dimmed" mb={4}>{m.from_admin ? t('tickets.support') : t('tickets.you')} · {when(m.created_at)}</Text>
                  <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>{m.body}</Text>
                </Paper>
              ))}
            </Stack>
            {d.status !== 'closed' ? (
              <>
                <Textarea placeholder={t('tickets.replyHint')} autosize minRows={2} value={reply} onChange={(e) => setReply(e.currentTarget.value)} />
                <Group justify="space-between"><Button variant="subtle" color="gray" onClick={() => close.mutate()}>{t('tickets.close')}</Button><Button disabled={!reply.trim()} loading={send.isPending} onClick={() => send.mutate()}>{t('tickets.reply')}</Button></Group>
              </>
            ) : <Text size="sm" c="dimmed">{t('tickets.closedHint')}</Text>}
          </Stack>
        )}
      </Modal>
    </Stack>
  )
}
