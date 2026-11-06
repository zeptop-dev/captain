import { Alert, Button, Code, Grid, Group, Select, Stack, Switch, Text, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { IconArrowsShuffle } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { composeNodeDomain, incompleteNodeDomain, splitNodeDomain, type DomainZone } from '../lib/node-domain'
import type { NodeDomainCheck } from '../lib/use-node-domain-check'

interface Props {
  domain: string; shared: boolean; active: boolean
  onDomainChange: (value: string) => void
  onSharedChange: (value: boolean) => void
  onIncompleteChange: (value: boolean) => void
  check: NodeDomainCheck
}
interface Catalog { domains: DomainZone[]; global_token: boolean }
interface Suggestion { prefix: string; domain: string; dns_check: 'checked' | 'unverified' }

export function NodeDomainFields(props: Props) {
  const { t } = useTranslation()
  const zones = useQuery({ queryKey: ['domains'], queryFn: ({ signal }) => api.get<Catalog>('/api/admin/domains', signal), enabled: props.active, staleTime: 60_000, refetchOnWindowFocus: false, retry: false })
  if (zones.isPending) return <TextInput label={t('nodes.domain')} value={props.domain} readOnly description={t('nodeDomain.loading')} />
  return <Stack gap="xs">
    {zones.isError && <Alert color="yellow"><Group justify="space-between"><Text size="sm">{t('nodeDomain.loadFailed')}</Text><Button variant="subtle" size="xs" onClick={() => zones.refetch()}>{t('workspace.retry')}</Button></Group></Alert>}
    <DomainEditor {...props} catalog={zones.data ?? { domains: [], global_token: false }} />
  </Stack>
}

function DomainEditor({ domain, shared, active, onDomainChange, onSharedChange, onIncompleteChange, check, catalog }: Props & { catalog: Catalog }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => splitNodeDomain(domain, catalog.domains))
  const [suggestion, setSuggestion] = useState<Suggestion | null>(null)
  const [randomError, setRandomError] = useState(false)
  const [requestState, setRequestState] = useState<{ domain: string; shared: boolean; controller: AbortController } | null>(null)
  const pending = !!requestState && active && requestState.domain === domain && requestState.shared === shared && !requestState.controller.signal.aborted
  const request = useRef<AbortController | null>(null)
  // External resets may replace the value; catalog polling must retain incomplete drafts.
  if (draft.value !== domain) setDraft(splitNodeDomain(domain, catalog.domains))
  const manual = shared || draft.manual || catalog.domains.length === 0 && !draft.prefix.trim() && !draft.zone
  const missingZone = !!draft.zone && !catalog.domains.some(z => z.name === draft.zone)
  const incomplete = !manual && (incompleteNodeDomain(draft) || missingZone)
  useEffect(() => { onIncompleteChange(incomplete || pending) }, [incomplete, pending, onIncompleteChange])
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    request.current?.abort(); request.current = null
  }, [domain, shared, active])

  const cancelSuggestion = () => {
    request.current?.abort(); request.current = null
    setRequestState(null); setSuggestion(null); setRandomError(false)
  }
  const changeParts = (prefix: string, zone: string | null) => {
    cancelSuggestion()
    const value = composeNodeDomain(prefix, zone)
    setDraft({ value, prefix, zone, manual: false }); onDomainChange(value)
  }
  const randomize = async () => {
    if (!draft.zone || !active) return
    cancelSuggestion()
    const controller = new AbortController()
    request.current = controller; setRequestState({ domain, shared, controller })
    try {
      const result = await api.post<Suggestion>('/api/admin/nodes/domain-suggestion', { zone: draft.zone }, controller.signal)
      if (controller.signal.aborted || request.current !== controller) return
      setDraft({ value: result.domain, prefix: result.prefix, zone: draft.zone, manual: false })
      setSuggestion(result); onDomainChange(result.domain)
    } catch {
      if (!controller.signal.aborted && request.current === controller) setRandomError(true)
    } finally {
      if (request.current === controller) { request.current = null; setRequestState(null) }
    }
  }
  const conflicts = check.data?.conflicts ?? []
  const names = conflicts.map(n => `${n.name} (#${n.id})`).join(', ')
  const selected = catalog.domains.find(z => z.name === draft.zone)
  const invalid = check.data?.valid === false ? t('nodes.domainInvalid') : undefined
  const zoneState = (zone: DomainZone) => {
    if (zone.provider !== 'cloudflare') return t('nodeDomain.manualDNS')
    if (/[^\x00-\x7F]/.test(zone.name)) return t('nodeDomain.idnHint')
    if (!zone.auto_dns) return t('nodeDomain.autoOff')
    if (!zone.has_token && !catalog.global_token) return t('nodeDomain.noToken')
    return t('nodeDomain.autoDNS')
  }
  return <Stack gap="sm">
    <Group justify="space-between" align="center" gap="xs">
      <Text size="sm" fw={500}>{t('nodes.domain')}</Text>
      {!shared && (!manual || catalog.domains.length > 0) && <Button type="button" variant="subtle" size="compact-xs" onClick={() => {
        cancelSuggestion()
        if (manual) {
          const parsed = splitNodeDomain(domain, catalog.domains)
          if (parsed.manual) return
          setDraft(parsed)
        } else {
          const value = domain || draft.prefix
          setDraft({ ...draft, value, manual: true }); onDomainChange(value)
        }
      }} disabled={manual && !!domain && splitNodeDomain(domain, catalog.domains).manual}>
        {manual ? t('nodeDomain.chooseZone') : t('nodeDomain.enterFull')}
      </Button>}
    </Group>
    {manual ? <TextInput label={t('nodeDomain.fullName')} placeholder="jp1.example.com" value={domain} onChange={e => {
      cancelSuggestion()
      const value = e.currentTarget.value
      const parsed = splitNodeDomain(value, catalog.domains)
      setDraft({ ...parsed, manual: draft.manual || parsed.manual }); onDomainChange(value)
    }} error={invalid} /> : <Grid gutter="sm" align="flex-start">
      <Grid.Col span={{ base: 12, xs: 6 }}>
        <TextInput label={t('nodeDomain.prefix')} placeholder="cedar-k7m2" value={draft.prefix} onChange={e => changeParts(e.currentTarget.value, draft.zone)} error={invalid || (draft.zone && !draft.prefix.trim() ? t('nodeDomain.prefixRequired') : undefined)}
          rightSectionWidth={110} rightSection={<Button type="button" variant="subtle" size="compact-xs" leftSection={<IconArrowsShuffle size={14} />} disabled={!draft.zone || missingZone} loading={pending} onClick={randomize}>{t('nodeDomain.random')}</Button>} />
      </Grid.Col>
      <Grid.Col span={{ base: 12, xs: 6 }}>
        <Select label={t('nodeDomain.zone')} placeholder={t('nodeDomain.selectZone')} searchable clearable
          data={catalog.domains.map(z => ({ value: z.name, label: z.name }))} value={draft.zone} onChange={value => changeParts(draft.prefix, value)}
          error={missingZone ? t('nodeDomain.zoneRemoved') : draft.prefix.trim() && !draft.zone ? t('nodeDomain.zoneRequired') : undefined}
          nothingFoundMessage={t('common.empty')} renderOption={({ option }) => <Stack gap={0}><Text size="sm">{option.label}</Text><Text size="xs" c="dimmed">{zoneState(catalog.domains.find(z => z.name === option.value)!)}</Text></Stack>} />
      </Grid.Col>
    </Grid>}
    {domain && <Text size="sm" c="dimmed">{t('nodeDomain.preview')} <Code style={{ overflowWrap: 'anywhere', whiteSpace: 'normal' }}>{domain}</Code></Text>}
    <Text size="xs" c="dimmed">{t('nodes.domainHint')}</Text>
    {!manual && <Text size="xs" c="dimmed">{selected ? zoneState(selected) : t('nodeDomain.randomHint')}</Text>}
    {!shared && manual && <Text size="xs" c="dimmed">{t('nodeDomain.manualHint')}</Text>}
    {suggestion?.domain === domain && <Text size="xs" c={suggestion.dns_check === 'checked' ? 'dimmed' : 'orange'} role="status">{suggestion.dns_check === 'checked' ? t('nodeDomain.checked') : t('nodeDomain.unverified')}</Text>}
    {randomError && <Text size="xs" c="red" role="alert">{t('nodeDomain.randomFailed')}</Text>}
    <Switch label={t('nodes.domainShared')} description={t('nodes.domainSharedHint')} checked={shared} onChange={e => {
      cancelSuggestion()
      const value = domain || draft.prefix
      setDraft(splitNodeDomain(value, catalog.domains)); onDomainChange(value); onSharedChange(e.currentTarget.checked)
    }} />
    {conflicts.length > 0 && <Alert color={shared ? 'blue' : 'orange'} title={t('nodes.domainUsed', { names })}>
      {t(shared ? 'nodes.domainSharedConflictHint' : check.data?.unchanged ? 'nodes.domainLegacyHint' : 'nodes.domainConflictHint')}
    </Alert>}
    {check.unavailable && <Text size="xs" c="dimmed">{t('nodes.domainCheckUnavailable')}</Text>}
  </Stack>
}
