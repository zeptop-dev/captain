import { Select, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { ForwardAddressMode, Node } from '../lib/api'

export function ForwardAddressModeSelect({ node, value, onChange, disabled, internalDisabled = false, internalDisabledHint, label }: {
  node?: Pick<Node, 'internal_addr'>; value?: ForwardAddressMode; onChange: (value: ForwardAddressMode) => void; disabled?: boolean; internalDisabled?: boolean; internalDisabledHint?: string; label?: string
}) {
  const { t } = useTranslation()
  const address = node?.internal_addr?.trim()
  return <Stack gap={4}>
    <Select label={label || t('forwardAddress.mode')} value={value || 'default'} allowDeselect={false} disabled={disabled || !node}
      data={[
        { value: 'default', label: t('forwardAddress.default') },
        { value: 'internal', label: address ? t('forwardAddress.internalValue', { address }) : t('forwardAddress.internalMissing'), disabled: !address || internalDisabled },
      ]} onChange={mode => onChange(mode === 'internal' ? 'internal' : 'default')} />
    {value === 'internal' && <Text size="xs" c={address ? 'dimmed' : 'red'}>{t(address ? 'forwardAddress.internalHint' : 'forwardAddress.missingHint')}</Text>}
    {internalDisabled && <Text size="xs" c="dimmed">{internalDisabledHint || t('forwardAddress.candidatesHint')}</Text>}
  </Stack>
}
