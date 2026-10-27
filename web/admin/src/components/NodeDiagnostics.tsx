import { Alert, Badge, Button, Card, Code, Group, Table, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import type { NodeDetail } from '../lib/server-detail'
import { when } from '../lib/format'
import { DoctorCard } from './DoctorCard'
import { DNSHealthCard } from './DNSHealthCard'
import { PrivateAccessReadiness } from './PrivateAccessReadiness'
import { NetworkDiagnosticsCard } from './NetworkDiagnosticsCard'
import { ReturnRoutesCard } from './ReturnRoutesCard'

export function NodeDiagnostics({ detail: d }: { detail: NodeDetail }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const [params] = useSearchParams()
  const view = params.get('view') ?? 'health'
  const n = d.node
  if (!n.paired) return <Alert color="orange">{t('nodes.unpaired')}</Alert>
  return <>
    <Group mb="lg">{['health', ...(me?.role === 'admin' ? ['network', 'routes'] : [])].map(key => <Button key={key} component={Link} variant={view === key ? 'light' : 'subtle'} aria-current={view === key ? 'page' : undefined} to={`/nodes/${n.id}/diagnostics?view=${key}`} onClick={e => { if (view === key) e.preventDefault() }}>{t(`serverDetail.diagnostics.${key}`)}</Button>)}</Group>
    {view === 'network' && me?.role === 'admin' ? <NetworkDiagnosticsCard nodeID={n.id} /> : view === 'routes' && me?.role === 'admin' ? <ReturnRoutesCard nodeID={n.id} /> : <>
      {d.status?.doctor ? <DoctorCard report={d.status.doctor} /> : <Card mb="lg"><Text c="dimmed">{t('common.empty')}</Text></Card>}
      {n.proxy_allowed !== false && <PrivateAccessReadiness nodeID={n.id} />}
      <DNSHealthCard nodeID={n.id} />
      {!!d.status?.certs?.length && <Card mb="lg"><Text fw={600} mb="sm">{t('nodes.certs')}</Text><Table.ScrollContainer minWidth={500}><Table><Table.Tbody>{d.status.certs.map(c => <Table.Tr key={c.domain}>
        <Table.Td><Code>{c.domain}</Code></Table.Td><Table.Td><Badge variant="outline" color="gray">{c.method}</Badge></Table.Td>
        <Table.Td>{c.not_after && !c.not_after.startsWith('0001') ? t('nodes.certExpires', { date: when(c.not_after).split(',')[0] }) : '—'}</Table.Td>
        <Table.Td>{c.error ? <Text size="xs" c="red">{c.error}</Text> : <Badge color="teal">{t('nodes.certOk')}</Badge>}</Table.Td>
      </Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer></Card>}
    </>}
  </>
}
