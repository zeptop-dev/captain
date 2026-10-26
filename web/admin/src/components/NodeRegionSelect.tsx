import { Button, Select, Stack, Text } from '@mantine/core'
import type { GeoLocation } from '../lib/location'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

export interface RegionResult { ip: string; country: string; checked_at: number; status: string }
export interface RegionDetection { location?: GeoLocation; last_success_at?: number; stale?: boolean; fresh: boolean; observed_at: number; ipv4: RegionResult; ipv6: RegionResult; country: string; ip: string; checked_at: number; conflict: boolean; source: string }

export function NodeRegionSelect({ value, onChange, countries, detection, detect, pending, disabled }: { value: string; onChange: (v: string) => void; countries: string[]; detection?: RegionDetection; detect: () => void; pending: boolean; disabled: boolean }) {
  const { t, i18n } = useTranslation()
  const countryLabel = useMemo(() => {
    const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? 'en'], { type: 'region' })
    return (code: string) => /^[A-Z]{2}$/.test(code) ? `${String.fromCodePoint(...[...code].map(c => 127397 + c.charCodeAt(0)))} ${code} · ${names.of(code) ?? code}` : code
  }, [i18n.resolvedLanguage])
  const options = useMemo(() => countries.map(code => ({ value: code, label: countryLabel(code) })), [countries, countryLabel])
  const known = detection?.country ? countryLabel(detection.country) : (detection?.checked_at ? t('nodeRegion.unknown') : t('nodeRegion.pending'))
  const data = [{ value: 'off', label: t('nodeRegion.off') }, { value: 'auto', label: t('nodeRegion.auto', { country: known }) }, ...options]
  // Preserve old region values in the editor rather than silently hiding them.
  if (value && !data.some(x => x.value === value)) data.push({ value, label: countryLabel(value) })
  return <Stack gap={5}>
    <Select label={t('nodeProbe.region')} description={t('nodeRegion.hint')} inputWrapperOrder={['label', 'input', 'description', 'error']} searchable allowDeselect={false} data={data} value={value} disabled={disabled} onChange={v => { if (v) { onChange(v); if (v === 'auto' && detection?.fresh) detect() } }} />
    {detection?.ip && <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{detection.ip} · {detection.checked_at ? new Date(detection.checked_at * 1000).toLocaleString() : t('nodeRegion.pending')}</Text>}
    {detection?.conflict && <Text size="xs" c="orange">{t('nodeRegion.conflict', { ipv4: detection.ipv4.country, ipv6: detection.ipv6.country })}</Text>}
    {detection?.ip && (detection.ipv4.status === 'failed' || detection.ipv6.status === 'failed') && <Text size="xs" c="orange">{t('nodeRegion.failed')}</Text>}
    <Button size="compact-xs" variant="subtle" style={{ alignSelf: 'flex-start' }} disabled={disabled || !detection?.fresh} loading={pending} onClick={detect}>{t('nodeRegion.detect')}</Button>
    {detection?.ip && !detection.fresh && <Text size="xs" c="dimmed">{t('nodeRegion.stale')}</Text>}
    {!detection?.ip && <Text size="xs" c="dimmed">{t('nodeRegion.noIP')}</Text>}
  </Stack>
}
