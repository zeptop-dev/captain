import { Select, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { locationIPError, locationIPSourceKey, type LocationIPDraft, type LocationIPMode, type LocationIPSettings } from '../lib/location-ip'
import type { RegionDetection } from './NodeRegionSelect'

export function NodeLocationIPEvidence({ detection }: { detection?: RegionDetection }) {
  const { t, i18n } = useTranslation()
  return <Stack gap={3} data-testid="location-ip-evidence">
    {detection?.ip ? <>
      <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t(detection.fresh ? 'locationIP.current' : 'locationIP.previous', { ip: detection.ip })}</Text>
      <Text size="xs" c="dimmed">{t('locationIP.source', { source: t(locationIPSourceKey(detection.ip_source)) })}</Text>
      <Text size="xs" c="dimmed">{t('locationIP.checked', { time: detection.checked_at ? new Date(detection.checked_at * 1000).toLocaleString(i18n.language) : t('nodeRegion.pending') })}</Text>
      {!detection.fresh && <Text size="xs" c="orange">{t('nodeRegion.stale')}</Text>}
    </> : <Text size="xs" c="dimmed">{t(detection?.selection_reason === 'entry_unavailable' ? 'locationIP.entryUnavailable' : 'nodeRegion.noIP')}</Text>}
  </Stack>
}

export function NodeLocationIPFields({ value, onChange, detection, disabled, unsaved }: { value: LocationIPSettings; onChange: (value: LocationIPDraft) => void; detection?: RegionDetection; disabled: boolean; unsaved: boolean }) {
  const { t } = useTranslation()
  const update = (patch: Partial<LocationIPSettings>) => onChange({ mode: value.mode, manual_ip: value.manual_ip ?? '', allow_entry_fallback: value.allow_entry_fallback ?? false, ...patch })
  return <Stack gap="xs" data-testid="location-ip-fields">
    <Title order={6}>{t('locationIP.title')}</Title>
    <Select label={t('locationIP.mode')} description={t('locationIP.hint')} inputWrapperOrder={['label', 'input', 'description', 'error']} allowDeselect={false} value={value.mode} data={(['auto', 'entry', 'manual'] as const).map(mode => ({ value: mode, label: t(`locationIP.modes.${mode}`) }))} disabled={disabled} onChange={mode => { if (mode) update({ mode: mode as LocationIPMode }) }} />
    {value.mode === 'manual' && <TextInput label={t('locationIP.manual')} description={t('locationIP.manualHint')} placeholder="198.51.100.20 / 2001:db8::20" value={value.manual_ip ?? ''} error={locationIPError(value) ? t('locationIP.invalid') : undefined} disabled={disabled} onChange={event => update({ manual_ip: event.currentTarget.value })} />}
    {value.mode === 'auto' && <><Text size="xs" c="dimmed">{t('locationIP.priority')}</Text><Switch label={t('locationIP.allowEntry')} description={t('locationIP.entryHint')} checked={value.allow_entry_fallback ?? false} disabled={disabled} onChange={event => update({ allow_entry_fallback: event.currentTarget.checked })} /></>}
    {value.mode === 'entry' && <Text size="xs" c="orange">{t('locationIP.entryHint')}</Text>}
    {unsaved && <Text size="xs" c="orange">{t('locationIP.saveFirst')}</Text>}
    <NodeLocationIPEvidence detection={detection} />
  </Stack>
}
