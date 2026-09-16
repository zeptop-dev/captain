import { Badge, Button, Group, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'

interface Row { id: number; node_name?: string; inbound_tag?: string; at: string; client_ip: string; host: string; port: number; network?: string }

// The user's recent connections (destination log), loaded on demand.
export function UserConnections({ userID }: { userID: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const q = useQuery({ queryKey: ['user-connections', userID], queryFn: () => api.get<Row[]>(`/api/admin/users/${userID}/connections?limit=300`), enabled: open })
  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Title order={6}>{t('connlog.userTitle')}</Title>
        <Button size="compact-xs" variant="subtle" onClick={() => setOpen(!open)}>{open ? t('hwid.hideRequests') : t('connlog.show')}</Button>
      </Group>
      {open && (
        <Table fz="xs"><Table.Tbody>
          {(q.data ?? []).length === 0 && <Table.Tr><Table.Td><Text size="xs" c="dimmed">{q.isLoading ? '…' : t('connlog.none')}</Text></Table.Td></Table.Tr>}
          {(q.data ?? []).map((r) => (
            <Table.Tr key={r.id}>
              <Table.Td><Text size="xs" c="dimmed">{when(r.at)}</Text></Table.Td>
              <Table.Td>{r.node_name}{r.inbound_tag ? ' · ' + r.inbound_tag : ''}</Table.Td>
              <Table.Td>{r.client_ip}</Table.Td>
              <Table.Td>{r.host}:{r.port}</Table.Td>
              <Table.Td>{r.network ? <Badge size="xs" variant="light" color={r.network === 'udp' ? 'grape' : 'blue'}>{r.network}</Badge> : null}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
