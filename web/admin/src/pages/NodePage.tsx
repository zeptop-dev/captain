import { Alert, Badge, Button, Card, Group, Loader, Stack, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useLocation, useParams } from 'react-router-dom'
import { api } from '../lib/api'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { serverDetailLocation, serverTabs, type NodeDetail } from '../lib/server-detail'
import { PageHeader } from '../components/PageHeader'
import { NodeStatus, PairCodeBox } from './NodesPage'
import { NodeOverview } from '../components/NodeOverview'
import { NodeInformationCard } from '../components/NodeInformationCard'
import { NodeDetailsCard } from '../components/NodeDetailsCard'
import { NodeGroupCard } from '../components/NodeGroupCard'
import { MetadataEditor } from '../components/MetadataEditor'
import { NodeDiagnostics } from '../components/NodeDiagnostics'
import { NodeSettings } from '../components/NodeSettings'

export default function NodePage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const { t } = useTranslation()
  const route = serverDetailLocation(id, location.pathname, location.search, location.hash)
  const q = useQuery({ queryKey: ['node', id], queryFn: () => api.get<NodeDetail>(`/api/admin/nodes/${id}`), refetchInterval: 10_000 })
  if (route.redirect) return <Navigate to={route.redirect} replace />
  if (q.isPending) return <Loader size="sm" />
  if (q.isError || !q.data) return <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>
  const d = q.data, n = d.node
  const modePending = n.paired && !!n.reported_mode && (n.reported_mode !== (n.mode ?? 'full') || n.reported_mode_revision !== n.mode_revision)
  // One boundary covers the mounted destination. Router navigation prompts before
  // replacing its drafts; other destinations aren't mounted or running effects.
  return <SettingsDraftBoundary key={`${n.id}:${location.key}`}>
    <PageHeader title={n.name} subtitle={[n.hostname, n.platform, n.version].filter(Boolean).join(' · ')} actions={<>
      <NodeStatus n={n} /><Badge color={n.mode === 'monitor' ? 'cyan' : 'indigo'}>{t(n.mode === 'monitor' ? 'nodeMode.monitor' : 'nodeMode.full')}</Badge>
      <Button component={Link} to="/nodes" variant="default" size="xs">{t('serverDetail.back')}</Button>
    </>} />
    <Group component="nav" aria-label={t('serverDetail.navigation')} gap="xs" mb="lg">
      {serverTabs.map(tab => <Button key={tab} component={Link} to={`/nodes/${n.id}${tab === 'overview' ? '' : tab === 'settings' ? '/settings/proxy' : `/${tab}`}`} variant={route.tab === tab ? 'filled' : 'subtle'} aria-current={route.tab === tab ? 'page' : undefined} onClick={event => { if (route.tab === tab) event.preventDefault() }}>{t(`serverDetail.tabs.${tab}`)}</Button>)}
    </Group>
    {(n.domain_shared || n.domain_conflict) && <Alert mb="md" color={n.domain_shared ? 'blue' : 'orange'} title={t(n.domain_shared ? 'nodes.domainExternal' : 'nodes.domainDuplicate')}>{t(n.domain_shared ? 'nodes.domainSharedHint' : 'nodes.domainLegacyHint')}</Alert>}
    {modePending && <Alert color="blue" mb="md">{t('nodeMode.pending', { mode: t(n.reported_mode === 'monitor' ? 'nodeMode.monitor' : 'nodeMode.full') })}</Alert>}
    {n.mode_error && <Alert color="red" mb="md" title={t('nodeMode.failed')}>{n.mode_error}</Alert>}
    {!n.paired && <Card mb="lg"><Stack><Text fw={600}>{t('nodes.pairTitle')}</Text>{n.pair_code ? <PairCodeBox code={n.pair_code} mode={n.mode} /> : <Text c="dimmed">{t('nodes.unpaired')}</Text>}</Stack></Card>}
    {route.tab === 'overview' && (n.paired ? <NodeOverview detail={d} /> : <Text c="dimmed">{t('serverDetail.waiting')}</Text>)}
    {route.tab === 'information' && <>
      <NodeDetailsCard node={n} section="profile" />
      <NodeInformationCard nodeID={n.id} reportedHost={d.status?.host} />
      <NodeGroupCard node={n} />
      <Card mb="lg"><MetadataEditor endpoint={`/api/admin/nodes/${n.id}/metadata`} /></Card>
    </>}
    {route.tab === 'diagnostics' && <NodeDiagnostics detail={d} />}
    {route.tab === 'settings' && <NodeSettings detail={d} section={route.section} />}
  </SettingsDraftBoundary>
}
