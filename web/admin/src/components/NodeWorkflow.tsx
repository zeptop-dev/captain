import { Badge, Button, Card, Group, SimpleGrid, Stack, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Entry, type Inbound, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { ago } from '../lib/format'

export function NodeWorkflow({ node, inbounds, cores }: { node: Node; inbounds: Inbound[]; cores?: Record<string, { running: boolean; inbounds?: string[] }> | null }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const entries = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries') })
  const enabled = inbounds.filter(ib => ib.Enabled)
  const reported = Object.values(cores ?? {})
  const confirmed = new Set(node.online ? reported.filter(c => c.running).flatMap(c => c.inbounds ?? []) : [])
  const known = node.online && reported.some(c => c.inbounds !== undefined)
  const published = entries.data?.filter(e => e.Enabled && enabled.some(ib => ib.ID === e.InboundID)).length
  return <Card mb="lg"><Text fw={600} mb="sm">{t('workspace.nodeProgress')}</Text><SimpleGrid cols={{ base: 1, md: 3 }}>
    <Stack gap="xs"><Group><Text size="sm" fw={600}>1 · {t('nav.nodes')}</Text><Badge color={!node.paired ? 'gray' : node.online ? 'teal' : 'orange'}>{t(`nodes.${!node.paired ? 'unpaired' : node.online ? 'online' : 'offline'}`)}</Badge></Group><Text size="xs" c="dimmed">{t('nodes.lastSeen')}: {node.last_seen_at ? ago(node.last_seen_at) : '—'}</Text><Button component={Link} to={`/monitoring?tab=alerts&node=${node.id}`} variant="subtle" size="xs" style={{ alignSelf: 'flex-start' }}>{t('alerts.title')}</Button></Stack>
    <Stack gap="xs"><Text size="sm" fw={600}>2 · {t('inbounds.title')}</Text><Text size="xs" c="dimmed">{known ? t('workspace.confirmedInbounds', { count: enabled.filter(ib => confirmed.has(ib.Tag)).length, total: enabled.length }) : t('workspace.applicationUnknown')}</Text><Button size="xs" variant="subtle" style={{ alignSelf: 'flex-start' }} onClick={() => document.getElementById('node-inbounds')?.scrollIntoView({ block: 'start', behavior: 'smooth' })}>{t('inbounds.title')}</Button></Stack>
    <Stack gap="xs"><Text size="sm" fw={600}>3 · {t('entries.title')}</Text><Text size="xs" c="dimmed">{published === undefined ? '—' : t('workspace.publishedEntries', { count: published })}</Text><Group gap="xs"><Button component={Link} to={`/entries?node=${node.id}`} variant="subtle" size="xs">{t('entries.title')}</Button>{node.paired && me?.role === 'admin' && <Button component={Link} to={`/nodes/${node.id}#diagnostics`} variant="subtle" size="xs">{t('diagnostics.title')}</Button>}</Group></Stack>
  </SimpleGrid><Text size="xs" c="dimmed" mt="sm">{t('workspace.publicationHint')}</Text></Card>
}
