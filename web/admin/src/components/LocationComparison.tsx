import { Alert, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { locationName, locationsDiffer, type GeoLocation, type LocationSettings } from '../lib/location'

export function LocationComparison({ automatic, effective, mode, stale = false, inherited = false }: { automatic?: GeoLocation | null; effective?: GeoLocation | null; mode: LocationSettings['mode']; stale?: boolean; inherited?: boolean }) {
 const { t } = useTranslation()
 return <Stack gap={4} data-testid="location-comparison">
  {automatic !== undefined && <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('locationCorrection.automatic')}: {locationName(automatic) || t('mapLocation.unknown')}{stale && automatic ? ` · ${t('locationCorrection.cached')}` : ''}</Text>}
  <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('locationCorrection.effective')}: {locationName(effective) || t('mapLocation.unknown')} · {t(mode === 'manual' ? 'locationCorrection.manual' : mode === 'off' ? 'mapLocation.off' : inherited ? 'mapLocation.inherit' : 'mapLocation.auto')}</Text>
  {mode === 'manual' && locationsDiffer(automatic, effective) && <Alert color="blue" py="xs">{t('locationCorrection.difference')}</Alert>}
 </Stack>
}
