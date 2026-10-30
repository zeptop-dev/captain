import { ActionIcon, Badge, Button, Card, Checkbox, Code, Group, Select, Stack, Table, TagsInput, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface Token { id: number; name: string; scope: string; scopes: string[] | null; created_at: string; last_used_at: string | null; expires_at: string | null }

// Personal API tokens for scripts and AI agents (MCP). The plaintext is
// shown once; the token carries the owner's role.
export function TokensCard() {
  const { t } = useTranslation()
  // One clock read per mount keeps render pure; data refetches refresh it.
  const [renderedAt] = useState(() => Date.now())
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['api-tokens'], queryFn: () => api.get<Token[]>('/api/admin/tokens') })
  const [name, setName] = useState('')
  const [scope, setScope] = useState('full')
  const [restricted, setRestricted] = useState(false)
  const [grants, setGrants] = useState<string[]>(['users:read'])
  const resources = useQuery({ queryKey: ['token-resources'], queryFn: () => api.get<string[]>('/api/admin/tokens/scopes') })
  const [days, setDays] = useState('0')
  const [fresh, setFresh] = useState<string | null>(null)
  const create = useMutation({ mutationFn: () => api.post<{ token: string }>('/api/admin/tokens', { Name: name, Scope: scope, Scopes: restricted ? grants : null, Days: Number(days) }), onSuccess: (r) => { setFresh(r.token); setName(''); qc.invalidateQueries({ queryKey: ['api-tokens'] }) }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/tokens/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['api-tokens'] }), onError: toast.err })
  const origin = window.location.origin
  const snippet = `{\n  "mcpServers": {\n    "captain": {\n      "type": "http",\n      "url": "${origin}/mcp",\n      "headers": { "Authorization": "Bearer ${fresh ?? '<token>'}" }\n    }\n  }\n}`
  return (
    <Card>
      <Title order={5} mb="xs">{t('tokens.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('tokens.hint')}</Text>
      <Group align="flex-end" mb="sm">
        <TextInput label={t('tokens.name')} placeholder="claude-code" value={name} onChange={(e) => setName(e.currentTarget.value)} style={{ flex: 1 }} />
        <Select label={t('tokens.scope')} w={150} allowDeselect={false} data={[{ value: 'full', label: t('tokens.scopeFull') }, { value: 'read', label: t('tokens.scopeRead') }]} value={scope} onChange={(v) => setScope(v ?? 'full')} />
        <Select label={t('tokens.expiry')} w={140} allowDeselect={false} data={[{ value: '0', label: t('tokens.noExpiry') }, { value: '30', label: t('tokens.days', { count: 30 }) }, { value: '90', label: t('tokens.days', { count: 90 }) }, { value: '365', label: t('tokens.days', { count: 365 }) }]} value={days} onChange={(v) => setDays(v ?? '0')} />
        <Button size="xs" mb={2} disabled={!name.trim() || (restricted && !grants.length)} loading={create.isPending} onClick={() => create.mutate()}>{t('tokens.create')}</Button>
      </Group>
      <Checkbox mb="xs" checked={restricted} onChange={(e) => setRestricted(e.currentTarget.checked)} label={t('tokens.restrict')} />
      {restricted && <><TagsInput mb="xs" label={t('tokens.permissions')} description={t('tokens.permissionsHint')} value={grants} onChange={setGrants} data={(resources.data ?? []).flatMap((r) => r === 'line-changes' ? [`${r}:read`] : [`${r}:read`, `${r}:write`, `${r}:*`])} /><Text size="xs" c="dimmed" mb="sm">{t('lineChanges.tokenHint')}</Text></>}
      {fresh && (
        <Stack gap={4} mb="sm">
          <Text size="xs" c="orange">{t('tokens.showOnce')}</Text>
          <Group gap={4} wrap="nowrap"><Code style={{ flex: 1, wordBreak: 'break-all' }}>{fresh}</Code><Copy value={fresh} /></Group>
        </Stack>
      )}
      {(q.data ?? []).length > 0 && (
        <Table.ScrollContainer minWidth={600}><Table fz="sm"><Table.Tbody>
          {q.data!.map((tk) => <Table.Tr key={tk.id}><Table.Td><Group gap={6}><Text fw={600}>{tk.name}</Text>{tk.scopes !== null && <Badge size="xs" title={(tk.scopes ?? []).join(', ')}>{t('tokens.restricted')}</Badge>}{tk.scope === 'read' && <Badge size="xs" variant="light">{t('tokens.scopeRead')}</Badge>}{tk.expires_at && <Badge size="xs" variant="light" color={new Date(tk.expires_at).getTime() < renderedAt ? 'red' : 'gray'}>{new Date(tk.expires_at).getTime() < renderedAt ? t('tokens.expired') : `${t('tokens.expires')} ${when(tk.expires_at)}`}</Badge>}</Group></Table.Td><Table.Td><Text size="xs" c="dimmed">{t('tokens.created')} {when(tk.created_at)}</Text></Table.Td><Table.Td><Text size="xs" c="dimmed">{tk.last_used_at ? `${t('tokens.lastUsed')} ${when(tk.last_used_at)}` : t('tokens.neverUsed')}</Text></Table.Td><Table.Td><ActionIcon variant="subtle" color="red" onClick={() => del.mutate(tk.id)}><IconTrash size={14} /></ActionIcon></Table.Td></Table.Tr>)}
        </Table.Tbody></Table></Table.ScrollContainer>
      )}
      <Text size="xs" fw={600} mt="sm">{t('tokens.mcp')}</Text>
      <Text size="xs" c="dimmed">{t('tokens.mcpHint', { url: origin + '/mcp' })}</Text>
      <Group gap={4} align="flex-start" wrap="nowrap" mt={4}><Code block style={{ flex: 1 }}>{snippet}</Code><Copy value={snippet} /></Group>
      <Text size="xs" c="dimmed" mt={4}>{t('tokens.mcpCli')} <Code>claude mcp add --transport http captain {origin}/mcp --header "Authorization: Bearer {fresh ?? '<token>'}"</Code></Text>
    </Card>
  )
}
