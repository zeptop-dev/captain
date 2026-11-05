import { Button, Group, Select, Stack, Text, Textarea } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { carrierProvinceOptions } from '../../../shared/carrier-provinces'
import { carrierTargetPlaceholder, type CarrierProbe, type CarrierTargetTexts, type CarrierFamily } from '../lib/carrier-probe'
import { SettingsFields } from './SettingsFields'

export function CarrierProbeFields({ probe, texts, onProbeChange, onTextsChange }: { probe: CarrierProbe; texts: CarrierTargetTexts; onProbeChange: (value: CarrierProbe) => void; onTextsChange: (value: CarrierTargetTexts) => void }) {
  const { t, i18n } = useTranslation()
  return <Stack gap="xs">
    <SettingsFields>
      <Select label={t('probe.carrierProtocol')} data={['tcp', 'icmp'].map(value => ({ value, label: value.toUpperCase() }))} allowDeselect={false} value={probe.protocol} onChange={value => value && onProbeChange({ ...probe, protocol: value as CarrierProbe['protocol'] })} />
      <Select label={t('probe.carrierFamily')} data={[{ value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }, { value: 'dual', label: 'IPv4 + IPv6' }]} allowDeselect={false} value={probe.family} onChange={value => value && onProbeChange({ ...probe, family: value as CarrierProbe['family'] })} />
      {probe.protocol === 'tcp' && <Select label={t('probe.carrierProvince')} data={carrierProvinceOptions(i18n.language)} searchable allowDeselect={false} value={probe.tcp_province || 'bj'} onChange={value => value && onProbeChange({ ...probe, tcp_province: value })} />}
    </SettingsFields>
    <Text size="xs" c="dimmed">{t('probe.carrierModesHint')}</Text>
    <SettingsFields>
      {(['ipv4', 'ipv6'] as CarrierFamily[]).filter(family => probe.family === 'dual' || probe.family === family).map(family => {
        const key = `${probe.protocol}_${family}` as const
        const custom = !!texts[key].trim()
        return <Stack key={key} gap={4} style={{ minWidth: 0 }}>
          <Textarea label={`${t('probe.carriers')} · ${family === 'ipv4' ? 'IPv4' : 'IPv6'}`} description={probe.protocol === 'icmp' ? t('probe.carriersICMPHint') : t('probe.carriersHint')} autosize minRows={3} placeholder={carrierTargetPlaceholder(probe.protocol, family, probe.tcp_province)} styles={{ input: { fontFamily: 'monospace', fontSize: 12 } }} value={texts[key]} onChange={event => onTextsChange({ ...texts, [key]: event.currentTarget.value })} />
          <Group justify="space-between" gap="xs">
            <Text size="xs" c={custom ? 'orange' : 'dimmed'}>{t(custom ? 'probe.carrierCustomActive' : 'probe.carrierPresetActive')}</Text>
            {custom && <Button size="compact-xs" variant="subtle" onClick={() => onTextsChange({ ...texts, [key]: '' })}>{t('probe.carrierRestorePreset')}</Button>}
          </Group>
        </Stack>
      })}
    </SettingsFields>
  </Stack>
}
