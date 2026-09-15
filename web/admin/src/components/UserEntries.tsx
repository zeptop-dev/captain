import { Badge, Code, Group, Stack, Switch, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface EntryLink { entry_id: number; name: string; protocol: string; host: string; port: number; uri: string; blocked: boolean }

// The servers one user receives: a share link per entry with this user's
// credentials, and a switch that hides an entry from this user only.
export function UserEntries({ userID }: { userID: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const key = ['user-entries', userID]
  const q = useQuery({ queryKey: key, queryFn: () => api.get<EntryLink[]>(`/api/admin/users/${userID}/entries`) })
  const save = useMutation({
    mutationFn: (blocked: number[]) => api.put(`/api/admin/users/${userID}/entries`, { blocked }),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }), onError: toast.err,
  })
  const rows = q.data ?? []
  const toggle = (id: number, on: boolean) => {
    const blocked = rows.filter((r) => (r.entry_id === id ? on : r.blocked)).map((r) => r.entry_id)
    save.mutate(blocked)
  }
  return (
    <Stack gap="sm">
      <Title order={6}>{t('userEntries.title')}</Title>
      <Text size="xs" c="dimmed">{t('userEntries.hint')}</Text>
      {rows.length === 0 ? <Text size="sm" c="dimmed">{t('userEntries.none')}</Text> : (
        <Table fz="xs" verticalSpacing={4}><Table.Tbody>
          {rows.map((r) => (
            <Table.Tr key={r.entry_id} style={{ opacity: r.blocked ? 0.55 : 1 }}>
              <Table.Td><Text size="xs" fw={500}>{r.name}</Text><Text size="xs" c="dimmed">{r.host}:{r.port}</Text></Table.Td>
              <Table.Td><Badge size="xs" variant="light">{r.protocol}</Badge></Table.Td>
              <Table.Td>{r.uri ? <Group gap={4} wrap="nowrap"><Code style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'block' }}>{r.uri}</Code><Copy value={r.uri} /></Group> : <Text size="xs" c="dimmed">{t('userEntries.noLink')}</Text>}</Table.Td>
              <Table.Td><Switch size="xs" label={t('userEntries.allowed')} checked={!r.blocked} onChange={(e) => toggle(r.entry_id, !e.currentTarget.checked)} disabled={save.isPending} /></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
