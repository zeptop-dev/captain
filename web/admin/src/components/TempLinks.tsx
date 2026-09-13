import { ActionIcon, Badge, Button, Code, Group, NumberInput, Stack, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface Link { id: number; code: string; kind: string; max_uses: number; uses: number; expires_at: string | null; enabled: boolean; url: string }

// Temporary subscription links for one user: limited by uses and/or hours,
// revocable, handed to someone without exposing the permanent link.
export function TempLinks({ userID }: { userID: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sub-links', userID], queryFn: () => api.get<Link[]>(`/api/admin/users/${userID}/links`) })
  const [uses, setUses] = useState<number | string>(3)
  const [hours, setHours] = useState<number | string>(24)
  const create = useMutation({ mutationFn: () => api.post(`/api/admin/users/${userID}/links`, { MaxUses: Number(uses) || 0, Hours: Number(hours) || 0 }), onSuccess: () => qc.invalidateQueries({ queryKey: ['sub-links', userID] }), onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/users/${userID}/links/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['sub-links', userID] }), onError: toast.err })
  const temps = (q.data ?? []).filter((l) => l.kind === 'temp')
  return (
    <Stack gap="sm">
      <Title order={6}>{t('tempLinks.title')}</Title>
      <Text size="xs" c="dimmed">{t('tempLinks.hint')}</Text>
      <Group align="flex-end" gap="xs"><NumberInput label={t('tempLinks.uses')} w={110} min={0} value={uses} onChange={setUses} /><NumberInput label={t('tempLinks.hours')} w={110} min={0} value={hours} onChange={setHours} /><Button size="xs" mb={2} variant="light" loading={create.isPending} onClick={() => create.mutate()}>{t('tempLinks.create')}</Button></Group>
      {temps.length > 0 && (
        <Table fz="xs"><Table.Tbody>
          {temps.map((l) => <Table.Tr key={l.id}><Table.Td><Group gap={4} wrap="nowrap"><Code>{l.url}</Code><Copy value={l.url} /></Group></Table.Td><Table.Td><Badge size="xs" variant="light">{l.uses}{l.max_uses ? ` / ${l.max_uses}` : ''}</Badge></Table.Td><Table.Td><Text size="xs" c="dimmed">{l.expires_at ? when(l.expires_at) : '∞'}</Text></Table.Td><Table.Td><ActionIcon variant="subtle" color="red" size="sm" onClick={() => del.mutate(l.id)}><IconTrash size={12} /></ActionIcon></Table.Td></Table.Tr>)}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
