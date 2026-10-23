import { Accordion, Alert, Badge, Checkbox, Group, Select, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { SubDesign } from '../lib/sub-design'
import { designPolicyCanDirect } from '../lib/sub-design'

export function SubDesignEnhancements({ design, onChange }: { design: SubDesign; onChange: (next: SubDesign) => void }) {
  const { t } = useTranslation()
  const options = design.enhancements ?? {}
  const set = (key: string, value: boolean | string | undefined) => onChange({ ...design, enhancements: { ...options, [key]: value } })
  const policies = [...new Set(design.groups.map(group => group.name).filter(Boolean))].map(name => {
    const direct = designPolicyCanDirect(design, name)
    return { value: name, label: direct ? `${name} · ${t('subDesign.directPolicy')}` : name, disabled: direct }
  })
  const hasRegions = design.groups.some(group => !!group.region)
  return <Stack gap="sm">
    <Title order={6}>{t('subDesign.enhancements')}</Title>
    <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
      <Stack gap={4}>
        <Checkbox label={t('subDesign.regionsOption')} checked={!!options.regions} onChange={event => set('regions', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.regionsHint')}</Text>
        {hasRegions && <Badge variant="light" size="sm" w="fit-content">{t('subDesign.regionsIncluded')}</Badge>}
      </Stack>
      <Stack gap={4}>
        <Checkbox label={t('subDesign.dnsOption')} checked={!!options.dns_protection} onChange={event => set('dns_protection', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.dnsHint')}</Text>
        {options.dns_protection && <Select size="xs" label={t('subDesign.dnsPolicy')} placeholder={t('subDesign.autoDNSPolicy')} data={policies} value={options.dns_policy || null} onChange={value => set('dns_policy', value || undefined)} clearable searchable />}
      </Stack>
      <Stack gap={4}>
        <Checkbox label={t('subDesign.claudeOption')} checked={!!options.claude} onChange={event => set('claude', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.claudeHint')}</Text>
        {options.claude && <Select size="xs" label={t('subDesign.claudePolicy')} placeholder={t('subDesign.autoClaudePolicy')} data={policies} value={options.claude_policy || null} onChange={value => set('claude_policy', value || undefined)} clearable searchable />}
      </Stack>
      <Stack gap={4}>
        <Checkbox label={t('subDesign.webrtcOption')} checked={!!options.webrtc} onChange={event => set('webrtc', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.webrtcHint')}</Text>
      </Stack>
    </SimpleGrid>
    <Accordion variant="contained">
      <Accordion.Item value="advanced">
        <Accordion.Control>{t('subDesign.advancedOptions')}</Accordion.Control>
        <Accordion.Panel>
          <Checkbox label={t('subDesign.quicOption')} description={t('subDesign.quicHint')} checked={!!options.quic} onChange={event => set('quic', event.currentTarget.checked)} />
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
    {(options.dns_protection || options.webrtc) && <Alert color="blue" variant="light"><Text size="xs">{t('subDesign.protectionLimit')}</Text></Alert>}
    <Group><Text size="xs" c="dimmed">{t('subDesign.enhancementsHint')}</Text></Group>
  </Stack>
}
