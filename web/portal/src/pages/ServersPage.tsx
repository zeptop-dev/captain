import { ActionIcon, Badge, Card, CopyButton, Group, Stack, Text, Title, Tooltip } from '@mantine/core'
import { IconCheck, IconCopy } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Server } from '../lib/api'

export default function ServersPage() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['servers'], queryFn: () => api.get<Server[]>('/api/portal/servers') })
  return (
    <Stack gap="lg">
      <Title order={2}>{t('servers.title')}</Title>
      {q.data?.length === 0 && <Text c="dimmed">{t('servers.empty')}</Text>}
      {(q.data ?? []).map((s) => (
        <Card key={s.name} padding="md">
          <Group justify="space-between">
            <div><Text fw={700}>{s.name}</Text><Text size="xs" c="dimmed" ff="monospace">{s.host}:{s.port}</Text></div>
            <Group gap="sm">
              <Badge>{s.protocol}</Badge>
              {s.uri && <CopyButton value={s.uri} timeout={1500}>{({ copied, copy }) => <Tooltip label={t('servers.copyLink')}><ActionIcon variant="light" color={copied ? 'teal' : undefined} onClick={copy}>{copied ? <IconCheck size={16} /> : <IconCopy size={16} />}</ActionIcon></Tooltip>}</CopyButton>}
            </Group>
          </Group>
        </Card>
      ))}
    </Stack>
  )
}
