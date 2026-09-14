import { ActionIcon, Badge, Card, Group, Stack, Text, Title, Tooltip } from '@mantine/core'
import { IconCheck, IconCopy } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { copyText } from '../lib/clipboard'
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
            <div><Group gap={6}><Text fw={700}>{s.name}</Text>{(s.tags ?? []).map((tg) => <Badge key={tg} size="xs" variant="light" color="grape">{tg}</Badge>)}</Group><Text size="xs" c="dimmed" ff="monospace">{s.host}:{s.port}</Text></div>
            <Group gap="sm">
              <Badge>{s.protocol}</Badge>
              {s.uri && <CopyLink value={s.uri} label={t('servers.copyLink')} />}
            </Group>
          </Group>
        </Card>
      ))}
    </Stack>
  )
}

function CopyLink({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Tooltip label={label}><ActionIcon variant="light" color={copied ? 'teal' : undefined} onClick={async () => { if (await copyText(value)) { setCopied(true); window.setTimeout(() => setCopied(false), 1500) } }}>{copied ? <IconCheck size={16} /> : <IconCopy size={16} />}</ActionIcon></Tooltip>
  )
}
