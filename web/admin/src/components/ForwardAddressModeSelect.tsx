import { Select, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { ForwardAddressMode, Ingress, Node } from '../lib/api'

export function ForwardAddressModeSelect({ node, ingress, value, onChange, disabled, internalDisabled = false, internalDisabledHint, label }: {
  node?: Pick<Node, 'internal_addr'> & Partial<Pick<Node, 'domain' | 'public_addr' | 'v6_addr'>>; ingress?: Ingress; value?: ForwardAddressMode; onChange: (value: ForwardAddressMode) => void; disabled?: boolean; internalDisabled?: boolean; internalDisabledHint?: string; label?: string
}) {
  const { t } = useTranslation()
  const address = node?.internal_addr?.trim()
  return <Stack gap={4}>
    <Select label={label || t('forwardAddress.mode')} value={value || 'default'} allowDeselect={false} disabled={disabled || !node}
      data={[
        { value: 'default', label: t('forwardAddress.default') },
        { value: 'line', label: ingress?.line_ip ? t('forwardAddress.lineValue', { address: ingress.line_ip }) : t('forwardAddress.lineMissing'), disabled: !ingress?.line_ip },
        { value: 'public', label: t('forwardAddress.publicValue', { address: ingress ? ingress.entry_domain || ingress.entry_host || '—' : node?.domain || node?.public_addr || node?.v6_addr || '—' }), disabled: !(ingress ? ingress.entry_domain || ingress.entry_host : node?.domain || node?.public_addr || node?.v6_addr) },
        { value: 'internal', label: address ? t('forwardAddress.internalValue', { address }) : t('forwardAddress.internalMissing'), disabled: !address || internalDisabled },
      ]} onChange={mode => onChange(mode === 'internal' || mode === 'line' || mode === 'public' ? mode : 'default')} />
    {(value === 'line' || value === 'public' || !value || value === 'default') && <Text size="xs" c="dimmed">{t(value === 'line' ? 'forwardAddress.lineHint' : value === 'public' ? 'forwardAddress.publicHint' : 'forwardAddress.defaultHint')}</Text>}
    {value === 'internal' && <Text size="xs" c={address ? 'dimmed' : 'red'}>{t(address ? 'forwardAddress.internalHint' : 'forwardAddress.missingHint')}</Text>}
    {internalDisabled && <Text size="xs" c="dimmed">{internalDisabledHint || t('forwardAddress.candidatesHint')}</Text>}
  </Stack>
}
