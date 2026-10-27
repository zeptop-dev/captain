import { Accordion, Alert, Card, Stack, Text } from '@mantine/core'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Inbound, type Ingress, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { isServerSettingsSection } from '../lib/navigation'
import { EgressUpstreams } from './EgressUpstreams'
import { IngressesCard } from './IngressesCard'
import { OverridesCard } from './OverridesCard'
import { RoutingCard } from './RoutingCard'

// The caller owns the shared server-page draft boundary and keys this section.
// Polling keeps drafts intact; confirmed navigation resets them.
export function ServerProxySettings({ node, inbounds, ingresses, section }: {
  node: Node; inbounds: Inbound[]; ingresses: Ingress[]; section: string | null
}) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const root = useRef<HTMLDivElement>(null)
  const editable = (me?.role === 'admin' || me?.role === 'operator') && node.paired && node.proxy_allowed !== false
  useEffect(() => {
    if (!isServerSettingsSection(section)) return
    const frame = requestAnimationFrame(() => root.current?.querySelector(`[data-section="${section}"]`)?.scrollIntoView({ block: 'start' }))
    return () => cancelAnimationFrame(frame)
  }, [section])
  return <Card mt="lg" p={0} ref={root}>
    <Stack p="md" gap={4}><Text fw={600}>{t('nodes.proxySettings')}</Text><Text size="xs" c="dimmed">{t('nodes.proxySettingsHint')}</Text></Stack>
      {!node.paired && <Alert color="orange" mx="md">{t('nodes.unpaired')}</Alert>}
      <Accordion multiple defaultValue={isServerSettingsSection(section) ? [section] : []} chevronPosition="right" variant="default">
        <Accordion.Item value="ingresses" data-section="ingresses" style={{ scrollMarginTop: 80 }}>
          <Accordion.Control><Text size="sm" fw={600}>{t('lines.sections.ingresses')}</Text><Text size="xs" c="dimmed">{t('nodes.advIngressHint')}</Text></Accordion.Control>
          <Accordion.Panel>{editable ? <IngressesCard embedded nodeID={node.id} ingresses={ingresses} inbounds={inbounds} /> : <Stack gap="xs"><Text size="sm">{t('nodes.advIngressCount', { count: ingresses.length })}</Text>{ingresses.map(g => <Text key={g.id} size="sm">{g.name} · {g.entry_domain || g.entry_host}</Text>)}</Stack>}</Accordion.Panel>
        </Accordion.Item>
        <Accordion.Item value="routing" data-section="routing" style={{ scrollMarginTop: 80 }}>
          <Accordion.Control><Text size="sm" fw={600}>{t('lines.sections.routing')}</Text><Text size="xs" c="dimmed">{t('nodes.advRoutingHint')}</Text></Accordion.Control>
          <Accordion.Panel><RoutingCard embedded nodeID={node.id} inboundTags={inbounds.map(ib => ib.Tag)} readOnly={!editable} />{editable && me?.role === 'admin' && <EgressUpstreams endpoint={`/api/admin/settings/nodes/${node.id}/egress`} />}</Accordion.Panel>
        </Accordion.Item>
        <Accordion.Item value="overrides" data-section="overrides" style={{ scrollMarginTop: 80 }}>
          <Accordion.Control><Text size="sm" fw={600}>{t('lines.sections.overrides')}</Text><Text size="xs" c="dimmed">{t('nodes.advOverridesHint')}</Text></Accordion.Control>
          <Accordion.Panel><OverridesCard embedded readOnly={!editable} queryKey={['overrides', node.id]} load={() => api.get<Record<string, string>>(`/api/admin/nodes/${node.id}/overrides`)} save={value => api.put(`/api/admin/nodes/${node.id}/overrides`, value)} /></Accordion.Panel>
        </Accordion.Item>
      </Accordion>
  </Card>
}
