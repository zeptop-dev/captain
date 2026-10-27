import { Accordion, Badge, Button, Card, Code, Group, Table, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { useNodeAccess } from '../lib/node-access-query'
import { coreName } from '../lib/coreSelection'
import { bytes } from '../lib/format'
import { AccessSummary } from './NodeAccess'
import { ReverseConnections } from './ReverseConnections'
import { WireGuardConnections } from './WireGuardConnections'
import { ForwardsCard } from './ForwardsCard'
import type { NodeDetail } from '../lib/server-detail'
export function NodeServices({ detail: d }: { detail: NodeDetail }) {
 const { t } = useTranslation()
 const { me } = useAuth()
 const access = useNodeAccess()
 const n = d.node
 return (
      <><Card p={0} id="node-inbounds" style={{ scrollMarginTop: 80 }}>
        <Group justify="space-between" p="md" pb="xs">
          <div><Text fw={600}>{t('inbounds.title')}</Text><Text size="xs" c="dimmed">{t('inbounds.subtitle')}</Text></div>
          <Group gap="xs"><Button component={Link} to={`/entries?node=${n.id}`} variant="light" size="xs">{t('entries.title')}</Button>{(me?.role === 'admin' || me?.role === 'operator') && <Button component={Link} to={`/entries?section=inbounds&server=${n.id}`} variant="default" size="xs">{t('inbounds.title')}</Button>}</Group>
        </Group>
        <Table>
          <Table.Thead><Table.Tr><Table.Th>{t('inbounds.tag')}</Table.Th><Table.Th>{t('inbounds.protocol')}</Table.Th><Table.Th>{t('inbounds.port')}</Table.Th><Table.Th>{t('inbounds.core')}</Table.Th><Table.Th>{t('inbounds.traffic')}</Table.Th><Table.Th>{t('nodeAccess.title')}</Table.Th><Table.Th>{t('inbounds.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {d.inbounds.map((ib) => (
              <Table.Tr key={ib.ID}>
                <Table.Td><Text fw={600}>{ib.Tag}</Text></Table.Td>
                <Table.Td><Badge>{ib.Protocol}</Badge></Table.Td>
                <Table.Td><Code>{ib.Listen || '::'}:{ib.Port}</Code></Table.Td>
                <Table.Td><Text size="sm">{ib.Core ? coreName(ib.Core) : t('inbounds.coreAuto')}</Text><Text size="xs" c="dimmed">{(() => { const actual = Object.entries(d.node.online ? d.status?.cores ?? {} : {}).find(([, c]) => c.running && c.inbounds?.includes(ib.Tag))?.[0]; return actual ? t('inbounds.coreSelection.running', { core: coreName(actual) }) : t('inbounds.coreSelection.notReported') })()}</Text></Table.Td>
                <Table.Td>{d.traffic?.[String(ib.ID)] ? <Text size="xs">{bytes(d.traffic[String(ib.ID)].today)} <Text span c="dimmed">/ {bytes(d.traffic[String(ib.ID)].total)}</Text></Text> : <Text size="xs" c="dimmed">—</Text>}</Table.Td>
                <Table.Td>{me?.role === 'admin' ? <AccessSummary row={access.isError ? undefined : access.data?.inbounds.find(row => row.inbound_id === ib.ID)} directory={access.data} /> : <Text size="xs" c="dimmed">{t('nodeAccess.adminOnly')}</Text>}</Table.Td>
                <Table.Td>{ib.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
                <Table.Td>{(me?.role === 'admin' || (me?.role === 'operator' && !ib.Settings.reverse && !ib.Settings.wireguard_slot)) && <Button component={Link} to={(ib.Settings.reverse || ib.Settings.wireguard_slot) ? `/entries?node=${n.id}` : `/entries?section=inbounds&server=${n.id}&inbound=${ib.ID}`} variant="subtle" size="compact-xs">{t((ib.Settings.reverse || ib.Settings.wireguard_slot) ? 'reverse.managed' : 'inbounds.title')}</Button>}</Table.Td>
              </Table.Tr>
            ))}
            {d.inbounds.length === 0 && <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
          </Table.Tbody>
        </Table>
      </Card>

      <Card mt="lg" p={0}>
        <Text fw={600} p="md">{t('lines.tab')}</Text>
        <Accordion multiple chevronPosition="right" variant="default">
          {me?.role === 'admin' && <Accordion.Item value="reverse"><Accordion.Control><Text size="sm" fw={600}>{t('reverse.title')}</Text></Accordion.Control><Accordion.Panel><ReverseConnections key={n.id} node={n} readOnly /><Button component={Link} to={`/entries?tab=lines&section=reverse&server=${n.id}`} variant="light" size="xs" mt="sm">{t('reverse.manage')}</Button></Accordion.Panel></Accordion.Item>}
          {me?.role === 'admin' && <Accordion.Item value="wireguard"><Accordion.Control><Text size="sm" fw={600}>{t('wireguard.title')}</Text></Accordion.Control><Accordion.Panel><WireGuardConnections key={n.id} node={n} readOnly /><Button component={Link} to={`/entries?tab=lines&section=wireguard&server=${n.id}`} variant="light" size="xs" mt="sm">{t('wireguard.title')}</Button></Accordion.Panel></Accordion.Item>}
          <Accordion.Item value="forwards"><Accordion.Control><Text size="sm" fw={600}>{t('forwards.title')}</Text><Text size="xs" c="dimmed">{t('nodes.advForwardsHint')}</Text></Accordion.Control><Accordion.Panel><ForwardsCard key={n.id} embedded node={n} ingresses={d.ingresses ?? []} readOnly /><Button component={Link} to={`/entries?tab=lines&section=forward&mode=single&server=${n.id}`} variant="light" size="xs" mt="sm">{t('forwards.title')}</Button></Accordion.Panel></Accordion.Item>
        </Accordion>
      </Card>
      </>

 )
}
