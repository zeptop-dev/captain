import { Badge, Button, Group, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'

interface Hit { id: number; node_name?: string; inbound_tag?: string; rule_name?: string; at: string; client_ip: string; host: string; port: number; action: string }

// The user's audit-rule hits, loaded on demand.
export function UserAudit({ userID }: { userID: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const q = useQuery({ queryKey: ['user-audit', userID], queryFn: () => api.get<Hit[]>(`/api/admin/audit-log?user_id=${userID}&limit=200`), enabled: open })
  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={6}>{t('audit.userTitle')}</Title>
        <Button size="compact-xs" variant="subtle" onClick={() => setOpen(!open)}>{open ? t('hwid.hideRequests') : t('audit.show')}</Button>
      </Group>
      {open && (
        <Table fz="xs"><Table.Tbody>
          {(q.data ?? []).length === 0 && <Table.Tr><Table.Td><Text size="xs" c="dimmed">{q.isLoading ? '…' : t('audit.noHits')}</Text></Table.Td></Table.Tr>}
          {(q.data ?? []).map((h) => (
            <Table.Tr key={h.id}>
              <Table.Td><Text size="xs" c="dimmed">{when(h.at)}</Text></Table.Td>
              <Table.Td><Badge size="xs" variant="light" color={h.action === 'block' ? 'red' : 'yellow'}>{h.rule_name || h.action}</Badge></Table.Td>
              <Table.Td>{h.host}:{h.port}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{h.node_name}{h.inbound_tag ? ' · ' + h.inbound_tag : ''} · {h.client_ip}</Text></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
