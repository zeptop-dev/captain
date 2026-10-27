import { Anchor, Divider, Group, Progress, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { bytes } from '../lib/format'
import { locationName, nodeLocationDraft } from '../lib/location'
import { supplierURL } from '../lib/node-infrastructure'
import type { NodeProbe } from '../lib/node-probe'
import { renewalAnchorDay } from '../lib/node-renewal'
import { recommendedStorage } from '../lib/probe-storage'
import type { ResourceHost } from '../lib/resources'

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return <Stack gap={3} style={{ minWidth: 0 }}><Text size="xs" c="dimmed">{label}</Text><Text component="div" size="sm" style={{ overflowWrap: 'anywhere' }}>{children}</Text></Stack>
}

// The overview consumes the same response and period as the information editor.
export function NodeProbeTrafficSummary({ data }: { data: NodeProbe }) {
  const { t, i18n } = useTranslation()
  const p = data.probe
  const pct = p.limit_bytes > 0 ? Math.min(100, Math.round(data.billed / p.limit_bytes * 100)) : 0
  return <Stack gap={5}>
    <Group justify="space-between"><Text size="sm">{t('nodeProbe.used')}: <b>{bytes(data.billed)}</b>{p.limit_bytes > 0 && <> / {bytes(p.limit_bytes)}</>}</Text><Text size="xs" c="dimmed">{t('nodeProbe.prev')}: {bytes(p.prev_used)}</Text></Group>
    {p.limit_bytes > 0 && <Progress value={pct} color={pct > 90 ? 'red' : pct > 70 ? 'orange' : 'teal'} />}
    <Text size="xs" c="dimmed">{t('nodeProbe.outbound')} {bytes(p.used_up)} · {t('nodeProbe.inbound')} {bytes(p.used_down)} · {t('nodeProbe.since')} {p.period_start ? new Date(p.period_start).toLocaleDateString(i18n.language) : '—'}</Text>
  </Stack>
}

export function NodeProbeCostSummary({ data }: { data: NodeProbe }) {
  const { t } = useTranslation()
  const info = data.probe.info
  const billing = info.billing
  const url = supplierURL(info.provider_url ?? '')
  const cycle = billing ? `${t(`nodeCost.${billing.cycle}`)}${billing.cycle === 'custom' ? ` · ${t('nodeCost.years')}: ${billing.years}` : ''}` : t('nodeInformation.notSet')
  return <Stack gap="sm">
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
      <Fact label={t('nodeProbe.provider')}>{info.provider || t('nodeInformation.notSet')}</Fact>
      <Fact label={t('nodeProbe.providerURL')}>{url ? <Anchor href={url} target="_blank" rel="noopener noreferrer" size="sm">{url}</Anchor> : info.provider_url || t('nodeInformation.notSet')}</Fact>
      <Fact label={t(billing ? 'nodeCost.amount' : 'nodeProbe.price')}>{billing ? billing.amount !== '' ? `${billing.currency} ${billing.amount}` : t('nodeInformation.notSet') : info.price || t('nodeInformation.notSet')}</Fact>
      <Fact label={t('nodeCost.cycle')}>{cycle}</Fact>
      <Fact label={t('nodeProbe.expires')}>{info.expires_at || t('nodeInformation.notSet')}</Fact>
      <Fact label={t('nodeCost.autoRenewal')}>{t(info.auto_renewal?.enabled ? 'common.enabled' : 'common.disabled')}</Fact>
    </SimpleGrid>
    {info.auto_renewal?.enabled && <Text size="xs" c="dimmed">{t('nodeCost.autoRenewalHint')}</Text>}
  </Stack>
}

export function NodeInformationSummary({ data, host }: { data: NodeProbe; host?: ResourceHost | null }) {
  const { t, i18n } = useTranslation()
  const p = data.probe
  const info = p.info
  const resources = p.resources
  const resetPolicy = info.traffic_reset?.mode ?? 'manual'
  const resetDay = renewalAnchorDay(info.auto_renewal, info.expires_at)
  const resetText = resetPolicy === 'manual' ? t('nodeInformation.manualReset', { day: p.reset_day }) : resetDay === null ? t('nodeCost.dateRequired') : t(resetPolicy === 'expiry_next' ? 'nodeCost.resetNextHint' : 'nodeCost.resetHint', { day: resetDay })
  const bandwidth = info.bandwidth
  const storage = p.storage?.mode === 'manual' ? p.storage.items : recommendedStorage(data.storage_candidates ?? [])
  const mode = nodeLocationDraft(info.location_settings, p.region_mode).mode
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? 'en'], { type: 'region' })
  const code = info.region || p.region_detection?.country || ''
  const region = /^[A-Z]{2}$/.test(code) ? `${names.of(code) ?? code} (${code})` : code || t('nodeRegion.unknown')
  return <Stack gap="lg">
    <Title order={5}>{t('nodeInformation.traffic')}</Title>
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
      <Fact label={t('resources.includeInterfaces')}>{resources?.include_interfaces?.join(', ') || t('nodeInformation.automaticInterfaces')}</Fact>
      <Fact label={t('resources.excludeInterfaces')}>{resources?.exclude_interfaces?.join(', ') || t('common.none')}</Fact>
      <Fact label={t('nodeProbe.mode')}>{t(`nodeProbe.modes.${p.mode}`)}</Fact>
      <Fact label={t('nodeProbe.limit')}>{p.limit_bytes > 0 ? bytes(p.limit_bytes) : t('nodeInformation.unlimited')}</Fact>
      <Fact label={t('nodeProbe.resetDay')}>{resetText}</Fact>
      <Fact label={t('gpu.enabled')}>{t(resources?.gpu ? 'common.enabled' : 'common.disabled')}</Fact>
    </SimpleGrid>
    <Text size="xs" c="dimmed">{t('resources.selectionHint')}</Text>
    <Divider />
    <Title order={5}>{t('nodeInformation.specifications')}</Title>
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
      <Fact label={t('resources.cpuCores')}>{host?.resources?.cpus?.length || t('nodeInformation.notReported')}</Fact>
      <Fact label={t('resources.memory')}>{host?.mem_total ? bytes(host.mem_total) : t('nodeInformation.notReported')}</Fact>
      <Fact label={t('nodeStorage.mode')}>{t(p.storage?.mode === 'manual' ? 'nodeStorage.manual' : 'nodeStorage.auto')}</Fact>
      <Fact label={t('nodeBandwidth.inbound')}>{bandwidth?.inbound_mbps ? `${bandwidth.inbound_mbps.toLocaleString(i18n.language)} Mbps` : t('nodeBandwidth.unknown')}</Fact>
      <Fact label={t('nodeBandwidth.outbound')}>{bandwidth?.outbound_mbps ? `${bandwidth.outbound_mbps.toLocaleString(i18n.language)} Mbps` : t('nodeBandwidth.unknown')}</Fact>
      <Fact label={t('nodeBandwidth.public')}>{t(bandwidth?.public ? 'common.enabled' : 'common.disabled')}</Fact>
    </SimpleGrid>
    <Fact label={t('nodeStorage.title')}>{storage.length ? <Stack gap={4}>{storage.map(item => {
      const candidate = data.storage_candidates?.find(value => value.mount === item.mount)
      return <Text key={item.mount} size="sm">{item.name ? `${item.name} · ` : ''}{item.mount} · {candidate?.total != null ? bytes(candidate.total) : t('nodeStorage.unavailable')}</Text>
    })}</Stack> : t(p.storage?.mode === 'manual' ? 'nodeStorage.empty' : 'nodeStorage.noCandidates')}</Fact>
    <Divider />
    <Title order={5}>{t('nodeInformation.location')}</Title>
    <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
      <Fact label={t('nodeProbe.region')}>{p.region_mode === 'off' ? t('nodeRegion.off') : p.region_mode === 'auto' ? t('nodeRegion.auto', { country: region }) : region}</Fact>
      <Fact label={t('mapLocation.mode')}>{t(`mapLocation.${mode}`)}</Fact>
      <Fact label={t('mapLocation.savedPosition')}>{locationName(info.location) || t('mapLocation.unknown')}</Fact>
      {info.location?.lat != null && info.location.lng != null && <Fact label={t('mapLocation.coordinates')}>{info.location.lat}, {info.location.lng}</Fact>}
    </SimpleGrid>
  </Stack>
}
