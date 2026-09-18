import { Badge, Card, Group, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'

interface Entry {
  id: number; at: string; user_id: number; email: string; role: string
  method: string; path: string; target?: string; status: number; via: string; ip: string
}

// Who changed what in the console. Staff accounts exist, so this has to be
// answerable; reads are not recorded (the console polls constantly).
export function AdminLogCard() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['admin-log'], queryFn: () => api.get<Entry[]>('/api/admin/admin-log?limit=100'), refetchInterval: 30000 })
  const rows = q.data ?? []
  return (
    <Card>
      <Title order={5} mb="xs">{t('adminlog.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('adminlog.hint')}</Text>
      {rows.length === 0 ? <Text size="sm" c="dimmed">{t('adminlog.empty')}</Text> : (
        <Table.ScrollContainer minWidth={720}>
          <Table fz="xs" verticalSpacing={4}>
            <Table.Thead><Table.Tr>
              <Table.Th>{t('adminlog.when')}</Table.Th><Table.Th>{t('adminlog.who')}</Table.Th>
              <Table.Th>{t('adminlog.what')}</Table.Th><Table.Th>{t('adminlog.result')}</Table.Th>
            </Table.Tr></Table.Thead>
            <Table.Tbody>
              {rows.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td><Text size="xs" c="dimmed">{when(e.at)}</Text></Table.Td>
                  <Table.Td>
                    <Group gap={4} wrap="nowrap">
                      <Text size="xs" fw={600}>{e.email || '—'}</Text>
                      <Badge size="xs" variant="light">{e.role}</Badge>
                      {e.via === 'token' && <Badge size="xs" variant="light" color="grape">token</Badge>}
                    </Group>
                    <Text size="xs" c="dimmed">{e.ip}</Text>
                  </Table.Td>
                  <Table.Td><Text size="xs" ff="monospace">{e.method} {e.path}</Text></Table.Td>
                  <Table.Td><Badge size="xs" color={e.status >= 400 ? 'red' : 'green'}>{e.status}</Badge></Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Card>
  )
}
