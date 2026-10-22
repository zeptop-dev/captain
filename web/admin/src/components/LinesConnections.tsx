import { Alert, Button, Loader, Select, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Inbound, type Ingress, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { SettingsDraftBoundary } from '../lib/settings-draft'
import { ForwardsCard } from './ForwardsCard'
import { ReverseConnections } from './ReverseConnections'
import { WireGuardConnections } from './WireGuardConnections'

export type ConnectionSection = 'wireguard' | 'reverse' | 'legacy'
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
  const title = { wireguard: 'wireguard.title', reverse: 'reverse.title', legacy: 'lines.sections.legacy' }[section]
  return <Stack>
    <Title order={4}>{t(title)}</Title>
    <ServerSelector serverID={serverID} onServerChange={onServerChange} />
    {!!serverID && <Button component={Link} to={`/nodes/${serverID}`} variant="subtle" size="xs" style={{ alignSelf: 'flex-start' }}>{t('entryFlow.serverDetails')}</Button>}
    {!serverID && <Text size="sm" c="dimmed">{t(section === 'wireguard' ? 'wireguard.hint' : section === 'reverse' ? 'reverse.hint' : 'nodes.subtitle')}</Text>}
    {!!serverID && q.isPending && <Loader size="sm" />}
    {q.isError && <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>}
    {data && <>
      {!data.node.paired && <Alert color="orange">{t('nodes.unpaired')}</Alert>}
      {data.node.proxy_allowed === false && <Alert color="orange">{t('nodeMode.monitorHint')}</Alert>}
      {section === 'reverse' && <ReverseConnections key={data.node.id} node={data.node} readOnly={!editable} editRequest={editRequest} onEditRequestHandled={onEditRequestHandled} />}
      {section === 'wireguard' && <WireGuardConnections key={data.node.id} node={data.node} readOnly={!editable} editRequest={editRequest} onEditRequestHandled={onEditRequestHandled} />}
      {section === 'legacy' && <SettingsDraftBoundary key={`forwards-${data.node.id}`}>
        <ForwardsCard embedded node={data.node} ingresses={data.ingresses ?? []} readOnly={!editable} />
      </SettingsDraftBoundary>}
    </>}
  </Stack>
}
