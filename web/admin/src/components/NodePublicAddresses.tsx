import { Badge, Group, Stack, Text, Tooltip } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { Node } from '../lib/api'
import { dnsRecordTypes, registeredAddresses } from '../lib/dns-health'
import { Copy } from './Copy'

export function NodePublicAddresses({ node }: { node: Node }) {
  const { t } = useTranslation()
  const addresses = registeredAddresses(node)
  return <Stack gap={3} w={230} maw={230}>
    {dnsRecordTypes.map(type => <Group gap={5} wrap="nowrap" key={type} align="flex-start">
      <Text size="xs" c="dimmed" w={30} style={{ flexShrink: 0 }}>{type === 'A' ? 'IPv4' : 'IPv6'}</Text>
      <Stack gap={2} style={{ minWidth: 0, flex: 1 }}>{addresses[type].length ? addresses[type].map(address => <Group key={address} gap={2} wrap="nowrap">
        <Tooltip label={address} withArrow><Text size="xs" ff="monospace" truncate style={{ minWidth: 0, flex: 1 }}>{address}</Text></Tooltip><Copy value={address} />
      </Group>) : <Text size="xs" c="dimmed">{t('nodeAddresses.unregistered')}</Text>}</Stack>
    </Group>)}
    {node.domain && <Tooltip label={node.domain}><Text size="xs" c="dimmed" truncate>{node.domain}</Text></Tooltip>}
    {(node.domain_shared || node.domain_conflict) && <Tooltip label={t(node.domain_shared ? 'nodes.domainSharedHint' : 'nodes.domainLegacyHint')}><Badge size="xs" color={node.domain_shared ? 'blue' : 'orange'}>{t(node.domain_shared ? 'nodes.domainExternal' : 'nodes.domainDuplicate')}</Badge></Tooltip>}
  </Stack>
}
