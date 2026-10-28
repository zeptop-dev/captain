import { latestResourceHost } from '../lib/resource-host'
import { Accordion, Alert, Button, Card, SimpleGrid, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'
import { useEffect, useState } from 'react'
import type { NodeDetail } from '../lib/server-detail'
import { useNodeProbe } from '../lib/node-probe'
import { ResourcesCard } from './ResourcesCard'
import { ResourceHistoryCard } from './ResourceHistoryCard'
import { AvailabilityCard } from './AvailabilityCard'
import { NetworkQualityCard } from './NetworkQualityCard'
import { HostTrafficSummary } from './HostTrafficSummary'
import { ProxyTrafficSummary } from './ProxyTrafficSummary'
import { NodeProbeTrafficSummary } from './NodeInformationSummary'
import { NodeWorkflow } from './NodeWorkflow'
import { NodeServices } from './NodeServices'
import { NodeServiceControlSummary } from './NodeServiceControl'

export function NodeOverview({ detail: d }: { detail: NodeDetail }) {
  const n = d.node
  const { t } = useTranslation()
  const location = useLocation()
  const q = useNodeProbe(n.id)
  const [services, setServices] = useState<string | null>(location.hash === '#node-inbounds' ? 'services' : null)
  useEffect(() => {
    if (location.hash !== '#node-inbounds') return
    const frame = requestAnimationFrame(() => document.getElementById('node-inbounds')?.scrollIntoView({ block: 'start' }))
    return () => cancelAnimationFrame(frame)
  }, [location.key, location.hash])
  const host = latestResourceHost(q.data?.live?.host, d.status?.host)
  if (!n.paired) return <Text c="dimmed">{t('serverDetail.waiting')}</Text>
  return <>
    {!n.online && <Alert mb="md" color="orange">{t('serverDetail.offline')}</Alert>}
    {(d.status?.doctor?.summary.fail ?? 0) + (d.status?.doctor?.summary.warn ?? 0) > 0 && <Alert mb="md" color="yellow">{t('serverDetail.diagnosticWarning')} <Button component={Link} to={`/nodes/${n.id}/diagnostics`} variant="subtle" size="xs">{t('serverDetail.tabs.diagnostics')}</Button></Alert>}
    <ResourcesCard host={host} compact />
    <SimpleGrid cols={{ base: 1, md: 2 }} mb="lg">
      <Card><Text fw={600} mb="sm">{t('nodes.hostTraffic')}</Text><HostTrafficSummary traffic={n.host_traffic} /><Text size="xs" c="dimmed" mt="sm">{t('nodes.hostTrafficHint')}</Text>{q.data && <NodeProbeTrafficSummary data={q.data} />}<NodeServiceControlSummary nodeID={n.id} /></Card>
      <Card><Text fw={600} mb="sm">{t('nodes.trafficToday')}</Text><ProxyTrafficSummary traffic={n.proxy_traffic} collectedBytes={n.traffic_today_bytes} /><Text size="xs" c="dimmed" mt="sm">{t('nodes.proxyTrafficHint')}</Text></Card>
    </SimpleGrid>
    {q.isError && <Alert mb="md" color="orange">{t('workspace.loadError')} <Button variant="subtle" size="xs" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
    <ResourceHistoryCard nodes={[{ id: n.id, name: n.name }]} detail />
    <AvailabilityCard nodeID={n.id} />
    <NetworkQualityCard nodeID={n.id} compact />
    {n.proxy_allowed !== false && <>
      <NodeWorkflow node={n} inbounds={d.inbounds} cores={d.status?.cores} />
      <Accordion value={services} onChange={setServices} variant="contained" mb="lg">
        <Accordion.Item value="services"><Accordion.Control>{t('serverDetail.services')}</Accordion.Control><Accordion.Panel>{services === 'services' && <NodeServices detail={d} />}</Accordion.Panel></Accordion.Item>
      </Accordion>
    </>}
  </>
}
