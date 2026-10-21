import { Alert, Button, Group, Loader, Select, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Inbound, type Ingress, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { EgressUpstreams } from './EgressUpstreams'
import { ForwardsCard } from './ForwardsCard'
import { IngressesCard } from './IngressesCard'
import { OverridesCard } from './OverridesCard'
import { ReverseConnections } from './ReverseConnections'
import { RoutingCard } from './RoutingCard'
import { WireGuardConnections } from './WireGuardConnections'

export type ConnectionSection = 'wireguard' | 'reverse' | 'routing' | 'legacy' | 'ingresses' | 'overrides'
export interface ServerProxyDetail { node: Node; inbounds: Inbound[]; ingresses?: Ingress[] }
export interface ServerSelectionProps {
  serverID?: number
  onServerChange: (id: number | null) => void
  canWrite?: boolean
}

export function ServerSelector({ serverID, onServerChange }: ServerSelectionProps) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes'), enabled: me?.role === 'admin' || me?.role === 'operator' })
  return <Stack gap="xs">
    <Select searchable clearable label={t('nodes.title')} value={serverID ? String(serverID) : null}
      data={(nodes.data ?? []).map(n => ({ value: String(n.id), label: n.name }))}
      onChange={value => onServerChange(value ? Number(value) : null)} disabled={nodes.isPending || nodes.isError} />
    {nodes.isError && <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => nodes.refetch()}>{t('workspace.retry')}</Button></Alert>}
  </Stack>
}

export function LinesConnections({ section, serverID, onServerChange, canWrite = true, editRequest = 0, onEditRequestHandled }: ServerSelectionProps & {
  section: ConnectionSection
  editRequest?: number
  onEditRequestHandled?: () => void
}) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const permitted = me?.role === 'admin' || (me?.role === 'operator' && !['wireguard', 'reverse'].includes(section))
  const q = useQuery({ queryKey: ['node', String(serverID)], queryFn: () => api.get<ServerProxyDetail>(`/api/admin/nodes/${serverID}`), enabled: !!serverID && permitted, refetchInterval: 10_000 })
  if (!permitted) return <Alert color="orange">{t('workspace.forbidden')}</Alert>
  const data = q.data
  const editable = canWrite && !!data?.node.paired && data.node.proxy_allowed !== false
  const title = { wireguard: 'wireguard.title', reverse: 'reverse.title', routing: 'routing.title', legacy: 'forwards.title', ingresses: 'ingress.title', overrides: 'overrides.title' }[section]
  return <Stack>
    <Title order={4}>{t(title)}</Title>
    <ServerSelector serverID={serverID} onServerChange={onServerChange} />
    {!serverID && <Text size="sm" c="dimmed">{t(section === 'wireguard' ? 'wireguard.hint' : section === 'reverse' ? 'reverse.hint' : section === 'routing' ? 'routing.hint' : 'nodes.subtitle')}</Text>}
    {!!serverID && q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
    {data && <>
      {!data.node.paired && <Alert color="orange">{t('nodes.unpaired')}</Alert>}
      {data.node.proxy_allowed === false && <Alert color="orange">{t('nodeMode.monitorHint')}</Alert>}
      {section === 'reverse' && <ReverseConnections key={data.node.id} node={data.node} readOnly={!editable} editRequest={editRequest} onEditRequestHandled={onEditRequestHandled} />}
      {section === 'wireguard' && <WireGuardConnections key={data.node.id} node={data.node} readOnly={!editable} editRequest={editRequest} onEditRequestHandled={onEditRequestHandled} />}
      {!['reverse', 'wireguard'].includes(section) && <SettingsDraftBoundary key={`${data.node.id}:${section}`}>
        {section === 'routing' && <Stack><RoutingCard embedded nodeID={data.node.id} inboundTags={data.inbounds.map(ib => ib.Tag)} readOnly={!editable} />{editable && me?.role === 'admin' && <EgressUpstreams endpoint={`/api/admin/settings/nodes/${data.node.id}/egress`} />}</Stack>}
        {section === 'legacy' && <ForwardsCard embedded node={data.node} ingresses={data.ingresses ?? []} readOnly={!editable} />}
        {section === 'ingresses' && editable && <IngressesCard embedded nodeID={data.node.id} ingresses={data.ingresses ?? []} inbounds={data.inbounds} />}
        {section === 'overrides' && editable && <OverridesCard queryKey={['overrides', data.node.id]} load={() => api.get<Record<string, string>>(`/api/admin/nodes/${data.node.id}/overrides`)} save={v => api.put(`/api/admin/nodes/${data.node.id}/overrides`, v)} />}
        {!editable && ['ingresses', 'overrides'].includes(section) && <Group><Text c="dimmed" size="sm">{t('workspace.forbidden')}</Text></Group>}
      </SettingsDraftBoundary>}
    </>}
  </Stack>
}
