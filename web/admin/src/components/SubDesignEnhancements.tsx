import { Accordion, Alert, Badge, Checkbox, Group, Select, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { ClaudeRulesData, SubDesign } from '../lib/sub-design'
import { claudeRulesEndpoint, claudeRulesQueryKey, designPolicyCanDirect, setClaudeEnabled } from '../lib/sub-design'
import { SubDesignClaudeRules } from './SubDesignClaudeRules'

export function SubDesignEnhancements({ design, onChange }: { design: SubDesign; onChange: (next: SubDesign) => void }) {
  const { t } = useTranslation()
  const claudeRules = useQuery({ queryKey: claudeRulesQueryKey, queryFn: ({ signal }) => api.get<ClaudeRulesData>(claudeRulesEndpoint, signal), retry: false })
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
        <Checkbox label={t('subDesign.claudeOption')} checked={!!options.claude} onChange={event => onChange(setClaudeEnabled(design, event.currentTarget.checked))} />
        <Text size="xs" c="dimmed">{t('subDesign.claudeHint')}</Text>
        {options.claude && <Select size="xs" label={t('subDesign.claudePolicy')} placeholder={t('subDesign.autoClaudePolicy')} data={policies} value={options.claude_policy || null} onChange={value => set('claude_policy', value || undefined)} clearable searchable />}
      </Stack>
      <Stack gap={4}>
        <Checkbox label={t('subDesign.webrtcOption')} checked={!!options.webrtc} onChange={event => set('webrtc', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.webrtcHint')}</Text>
      </Stack>
      <Stack gap={4}>
        <Checkbox label={t('subDesign.nodeDualStackOption')} checked={!!options.node_dual_stack} onChange={event => set('node_dual_stack', event.currentTarget.checked)} />
        <Text size="xs" c="dimmed">{t('subDesign.nodeDualStackHint')}</Text>
        {options.node_dual_stack && <Text size="xs" c="dimmed">{t('subDesign.nodeDualStackRequirements')}</Text>}
      </Stack>
    </SimpleGrid>
    {options.claude && <SubDesignClaudeRules design={design} onChange={onChange} data={claudeRules.data} loading={claudeRules.isPending} error={claudeRules.error} onRetry={() => { void claudeRules.refetch() }} />}
    <Accordion variant="contained">
      {options.node_dual_stack && <Accordion.Item value="dual-stack-protocols">
        <Accordion.Control>{t('subDesign.nodeDualStackProtocols')}</Accordion.Control>
        <Accordion.Panel><Stack gap="xs">
          <Text size="xs">{t('subDesign.nodeDualStackTCP')}</Text>
          <Text size="xs">{t('subDesign.nodeDualStackQUIC')}</Text>
          <Text size="xs">{t('subDesign.nodeDualStackOther')}</Text>
        </Stack></Accordion.Panel>
      </Accordion.Item>}
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
