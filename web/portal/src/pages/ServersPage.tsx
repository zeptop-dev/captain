import { ActionIcon, Alert, Anchor, Badge, Button, Card, Group, Select, SimpleGrid, Skeleton, Stack, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { IconArrowLeft, IconCopy, IconSearch, IconServer } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { copyText } from '../lib/clipboard'
import { api, type Server } from '../lib/api'
import { Link } from 'react-router-dom'
import { toast } from '../lib/notify'

export default function ServersPage() {
  const { t } = useTranslation()
  return <Stack gap="lg">
    <Group justify="space-between"><Title order={1} fz={30}>{t('servers.title')}</Title><Button variant="subtle" component={Link} to="/connect" leftSection={<IconArrowLeft size={16} />}>{t('connection.back')}</Button></Group>
    <ServerList />
  </Stack>
}

export function ServerList() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['servers'], queryFn: () => api.get<Server[]>('/api/portal/servers'), meta: { handlesError: true } })
  const [search, setSearch] = useState('')
  const [protocol, setProtocol] = useState<string | null>(null)
  const [tag, setTag] = useState<string | null>(null)
  const servers = q.data ?? []
  const protocols = Array.from(new Set(servers.map(server => server.protocol))).sort()
  const tags = Array.from(new Set(servers.flatMap(server => server.tags ?? []))).sort()
  const needle = search.trim().toLocaleLowerCase()
  const visible = servers.filter(server => (!protocol || server.protocol === protocol) && (!tag || server.tags?.includes(tag)) && (!needle || [server.name, server.host, server.protocol, ...(server.tags ?? [])].join(' ').toLocaleLowerCase().includes(needle)))
  return <Stack gap="md">
    <Text size="sm" c="dimmed">{t('connection.nodesHint')}</Text>
    {q.isPending && <SimpleGrid cols={{ base: 1, sm: 2 }}><Skeleton height={100} /><Skeleton height={100} /></SimpleGrid>}
    {q.isError && <Alert color="red" title={t('connection.nodesError')}><Button size="compact-sm" variant="subtle" onClick={() => q.refetch()}>{t('connection.retry')}</Button></Alert>}
    {servers.length > 0 && <Group align="flex-end" gap="sm">
      <TextInput value={search} onChange={event => setSearch(event.currentTarget.value)} placeholder={t('connection.searchNodes')} aria-label={t('connection.searchNodes')} leftSection={<IconSearch size={17} />} style={{ flex: '1 1 220px' }} />
      {protocols.length > 1 && <Select aria-label={t('connection.protocol')} placeholder={t('connection.allProtocols')} value={protocol} onChange={setProtocol} data={protocols} clearable style={{ flex: '0 1 170px' }} />}
      {tags.length > 0 && <Select aria-label={t('connection.tag')} placeholder={t('connection.allTags')} value={tag} onChange={setTag} data={tags} searchable clearable style={{ flex: '0 1 170px' }} />}
    </Group>}
    {q.isSuccess && servers.length === 0 && <Card><Text c="dimmed" size="sm">{t('servers.empty')}</Text><Anchor component={Link} to="/plans" size="sm" mt="xs">{t('home.browsePlans')}</Anchor></Card>}
    {servers.length > 0 && visible.length === 0 && <Card><Text size="sm" c="dimmed">{t('connection.noMatches')}</Text><Button variant="subtle" size="compact-sm" mt="xs" onClick={() => { setSearch(''); setProtocol(null); setTag(null) }}>{t('connection.clearFilters')}</Button></Card>}
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
      {visible.map((server, index) => <Card key={`${server.name}-${server.host}-${server.port}-${index}`} padding="lg">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}><IconServer size={21} style={{ flexShrink: 0, color: 'var(--mantine-primary-color-filled)' }} aria-hidden /><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{server.name}</Text></Group>
          <Group gap={6} wrap="nowrap"><Badge variant="light" size="sm">{server.protocol}</Badge>{server.uri && <Tooltip label={t('servers.copyLink')}><ActionIcon variant="subtle" aria-label={t('servers.copyLink')} onClick={async () => { if (await copyText(server.uri)) toast.ok(t('home.copied')) }}><IconCopy size={17} /></ActionIcon></Tooltip>}</Group>
        </Group>
        {(server.tags ?? []).length > 0 && <Group gap={5} mt="sm">{server.tags!.map(value => <Badge key={value} size="xs" color="gray" variant="light">{value}</Badge>)}</Group>}
        <Text size="xs" c="dimmed" ff="monospace" mt="sm" style={{ overflowWrap: 'anywhere' }}>{server.host}:{server.port}</Text>
        {server.client_chain && <Text size="xs" c="dimmed" mt="xs">{t('servers.clientChain')}</Text>}
        {server.protocol === 'wireguard' && <Text size="xs" c="dimmed" mt="xs">{t('wgDevices.serverHint')} <Anchor component={Link} to="/connect#wireguard" size="xs">{t('connection.manageWG')}</Anchor></Text>}
      </Card>)}
    </SimpleGrid>
  </Stack>
}
