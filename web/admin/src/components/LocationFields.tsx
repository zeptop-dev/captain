import { Autocomplete, Alert, Button, Collapse, Group, NumberInput, Select, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type GeoLocation, type LocationDraft, locationDraftError, cityLocationDraft } from '../lib/location'
import { LocationComparison } from './LocationComparison'
import { locationCities } from '../lib/location-cities'
import { REGIONS } from '../lib/regions'

export function LocationFields({ value, onChange, effective, countries, inherited = false, disabled = false, automatic, automaticStale, effectiveMode, onCity }: {
  value: LocationDraft; onChange: (value: LocationDraft) => void; effective?: GeoLocation | null
  countries?: string[]; inherited?: boolean; disabled?: boolean
  automatic?: GeoLocation | null; automaticStale?: boolean; effectiveMode?: LocationDraft['mode']; onCity?: (city: LocationDraft) => void
}) {
  const { t, i18n } = useTranslation()
  const [coordinatesOpen, setCoordinatesOpen] = useState<boolean | null>(null)
  const [cityQuery, setCityQuery] = useState('')
  const cityOptions = locationCities.map(city => ({ value: city.id, label: `${city.city} · ${city.country} · ${city.zh}` }))
  const error = locationDraftError(value)
  const needsCoordinates = !!value.city.trim() || value.lat !== '' || value.lng !== ''
  const expanded = coordinatesOpen ?? needsCoordinates
  const names = new Intl.DisplayNames([i18n.resolvedLanguage ?? 'en'], { type: 'region' })
  const codes = [...new Set([...(countries ?? REGIONS.map(region => region.code)), ...(value.country ? [value.country] : [])])]
  const change = <K extends keyof LocationDraft>(key: K, next: LocationDraft[K]) => onChange({ ...value, [key]: next })
  return <Stack gap="sm">
    <Title order={6}>{t('mapLocation.title')}</Title>
    <Text size="xs" c="dimmed">{inherited ? t('mapLocation.entryHint') : t('mapLocation.serverHint')}</Text>
    <Select label={t('mapLocation.mode')} allowDeselect={false} value={value.mode} disabled={disabled} onChange={mode => { if (mode) change('mode', mode as LocationDraft['mode']) }} data={[
      { value: 'auto', label: inherited ? t('mapLocation.inherit') : t('mapLocation.auto') },
      { value: 'manual', label: t('mapLocation.manual') }, { value: 'off', label: t('mapLocation.off') },
    ]} />
    <LocationComparison automatic={automatic} effective={effective} mode={effectiveMode ?? value.mode} stale={automaticStale} inherited={inherited} />
    <Select label={t('locationCorrection.city')} description={t(inherited ? 'locationCorrection.entryHint' : 'locationCorrection.cityHint')} searchable clearable searchValue={cityQuery} onSearchChange={setCityQuery} value={null} data={cityOptions} disabled={disabled} placeholder={t('locationCorrection.search')} onChange={id => {
      const city = locationCities.find(item => item.id === id)
      if (city) { const draft = cityLocationDraft(city); if (onCity) onCity(draft); else onChange(draft); setCoordinatesOpen(true); setCityQuery('') }
    }} />
    <Text size="xs" c="dimmed">{t('locationCorrection.cityReference')}</Text>
    {value.mode === 'manual' && <>
      <SimpleGrid cols={{ base: 1, sm: 3 }}>
        {countries ? <Select label={t('mapLocation.country')} required searchable allowDeselect={false} value={value.country || null} disabled={disabled} onChange={country => change('country', country || '')} data={codes.map(code => ({ value: code, label: `${code} · ${names.of(code) ?? code}` }))} /> : <Autocomplete label={t('mapLocation.country')} required maxLength={2} value={value.country} disabled={disabled} onChange={country => change('country', country.toUpperCase())} data={codes} description={/^[A-Z]{2}$/.test(value.country) ? names.of(value.country) : undefined} />}
        <TextInput label={t('mapLocation.subdivision')} maxLength={100} value={value.subdivision} disabled={disabled} onChange={event => change('subdivision', event.currentTarget.value)} />
        <TextInput label={t('mapLocation.city')} maxLength={100} value={value.city} disabled={disabled} onChange={event => change('city', event.currentTarget.value)} />
      </SimpleGrid>
      <Group><Button variant="subtle" size="compact-sm" onClick={() => setCoordinatesOpen(!expanded)} aria-expanded={expanded}>{t('mapLocation.coordinates')}</Button></Group>
      <Collapse in={expanded}><Stack gap="xs">
        <Text size="xs" c="dimmed">{t('mapLocation.coordinatesHint')}</Text>
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <NumberInput label={t('mapLocation.latitude')} min={-90} max={90} decimalScale={6} value={value.lat} disabled={disabled} onChange={lat => change('lat', lat)} />
          <NumberInput label={t('mapLocation.longitude')} min={-180} max={180} decimalScale={6} value={value.lng} disabled={disabled} onChange={lng => change('lng', lng)} />
        </SimpleGrid>
      </Stack></Collapse>
      {error && <Alert color="yellow">{t(`mapLocation.validation.${error}`)}</Alert>}
    </>}
  </Stack>
}
