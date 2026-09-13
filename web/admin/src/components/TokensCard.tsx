import { ActionIcon, Button, Card, Code, Group, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface Token { id: number; name: string; created_at: string; last_used_at: string | null }

// Personal API tokens for scripts and AI agents (MCP). The plaintext is
// shown once; the token carries the owner's role.
export function TokensCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['api-tokens'], queryFn: () => api.get<Token[]>('/api/admin/tokens') })
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState<string | null>(null)
  const create = useMutation({ mutationFn: () => api.post<{ token: string }>('/api/admin/tokens', { Name: name }), onSuccess: (r) => { setFresh(r.token); setName(''); qc.invalidateQueries({ queryKey: ['api-tokens'] }) }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/tokens/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['api-tokens'] }), onError: toast.err })
  const origin = window.location.origin
  const snippet = `{\n  "mcpServers": {\n    "captain": {\n      "type": "http",\n      "url": "${origin}/mcp",\n      "headers": { "Authorization": "Bearer ${fresh ?? '<token>'}" }\n    }\n  }\n}`
  return (
    <Card>
      <Title order={5} mb="xs">{t('tokens.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('tokens.hint')}</Text>
      <Group align="flex-end" mb="sm"><TextInput label={t('tokens.name')} placeholder="claude-code" value={name} onChange={(e) => setName(e.currentTarget.value)} style={{ flex: 1 }} /><Button size="xs" mb={2} disabled={!name.trim()} loading={create.isPending} onClick={() => create.mutate()}>{t('tokens.create')}</Button></Group>
      {fresh && (
        <Stack gap={4} mb="sm">
          <Text size="xs" c="orange">{t('tokens.showOnce')}</Text>
          <Group gap={4} wrap="nowrap"><Code style={{ flex: 1, wordBreak: 'break-all' }}>{fresh}</Code><Copy value={fresh} /></Group>
        </Stack>
      )}
      {(q.data ?? []).length > 0 && (
        <Table fz="sm"><Table.Tbody>
          {q.data!.map((tk) => <Table.Tr key={tk.id}><Table.Td><Text fw={600}>{tk.name}</Text></Table.Td><Table.Td><Text size="xs" c="dimmed">{t('tokens.created')} {when(tk.created_at)}</Text></Table.Td><Table.Td><Text size="xs" c="dimmed">{tk.last_used_at ? `${t('tokens.lastUsed')} ${when(tk.last_used_at)}` : t('tokens.neverUsed')}</Text></Table.Td><Table.Td><ActionIcon variant="subtle" color="red" onClick={() => del.mutate(tk.id)}><IconTrash size={14} /></ActionIcon></Table.Td></Table.Tr>)}
        </Table.Tbody></Table>
      )}
      <Text size="xs" fw={600} mt="sm">{t('tokens.mcp')}</Text>
      <Text size="xs" c="dimmed">{t('tokens.mcpHint', { url: origin + '/mcp' })}</Text>
      <Group gap={4} align="flex-start" wrap="nowrap" mt={4}><Code block style={{ flex: 1 }}>{snippet}</Code><Copy value={snippet} /></Group>
      <Text size="xs" c="dimmed" mt={4}>{t('tokens.mcpCli')} <Code>claude mcp add --transport http captain {origin}/mcp --header "Authorization: Bearer {fresh ?? '<token>'}"</Code></Text>
    </Card>
  )
}
