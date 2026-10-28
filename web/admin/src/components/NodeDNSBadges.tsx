import { Button, Group, Text, Tooltip } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import type { Node } from '../lib/api'
import { dnsFamily, dnsFamilyLabel, dnsFamilyStatus, dnsRecordTypes, dnsSnapshotStale, dnsStatusColor, nodeDNSLink, nodeDNSTarget } from '../lib/dns-health'
import { useDNSClock, useDNSHealth } from '../lib/use-dns-health'

export function NodeDNSBadges({ node }: { node: Node }) {
  const { t } = useTranslation()
  const q = useDNSHealth(node.id, !!node.domain)
  const clock = useDNSClock()
  const now = Math.max(clock, q.dataUpdatedAt / 1000)
  const health = q.data?.[0]
  const target = nodeDNSTarget(health, node)
  if (!node.domain) return null
  const stale = !health || dnsSnapshotStale(health, now)
  return <>
    <Group gap={5} mt="xs">{dnsRecordTypes.map(type => {
      const status = q.isError ? 'unknown' : target ? dnsFamilyStatus(dnsFamily(target, type), stale, target.mode) : 'unknown'
      return <Tooltip key={type} label={t('dnsHealth.badgeHint', { at: health?.checked_at ? new Date(health.checked_at * 1000).toLocaleString() : '—' })} multiline w={280}>
        <Button component={Link} to={nodeDNSLink(node.id, type)} size="compact-xs" variant="light" color={dnsStatusColor(status)}>{dnsFamilyLabel(type)} · {t(`dnsHealth.statuses.${status}`)}</Button>
      </Tooltip>
    })}</Group>
    {q.isError && <Text size="xs" c="dimmed" mt={4}>{t('dnsHealth.loadError')}</Text>}
  </>
}
