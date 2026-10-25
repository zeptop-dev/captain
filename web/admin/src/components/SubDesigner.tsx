import { Accordion, ActionIcon, Alert, Anchor, Badge, Button, Card, Checkbox, Code, Divider, Group, List, Loader, Modal, MultiSelect, NumberInput, Paper, Select, SimpleGrid, Stack, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconDeviceFloppy, IconPlus, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'
import { claudeRulesQueryKey, designCatalogue, designFormatLabels, designSupportReasons, designSupportWarnings, moveDesignItem, renameDesignGroup, selectedDesignFormats } from '../lib/sub-design'
import type { DesignInspection, DesignPreset, SubDesign, SubDesignData } from '../lib/sub-design'
import { SubDesignEnhancements } from './SubDesignEnhancements'

const endpoint = '/api/admin/settings/sub-design'
const emptyDesign: SubDesign = { groups: [], rules: [], final: '' }
const groupTypes = ['select', 'url-test', 'fallback', 'load-balance']
const errorText = (error: unknown) => error instanceof Error ? error.message : String(error)
type Snapshot = { design: SubDesign; epoch: number }
type Preview = { format: string; text: string }

function OfficialPresetNotes({ preset, draftRevision }: { preset?: DesignPreset; draftRevision?: string }) {
  const { t } = useTranslation()
  if (preset?.source !== 'acl4ssr') return null
  return <Paper withBorder p="sm" radius="md">
    <Stack gap="xs">
      <Group gap="xs" wrap="wrap"><Text size="sm" fw={600}>{t('subDesign.officialNotes')}</Text>{preset.revision && <Code>{preset.revision.slice(0, 10)}</Code>}</Group>
      <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{preset.name}</Text>
      {draftRevision && draftRevision !== preset.revision && <Text size="xs" c="orange">{t('subDesign.officialNotesDifferentRevision')}</Text>}
      <Text size="sm" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{preset.official_notes || t('subDesign.officialNotesEmpty')}</Text>
      <Text size="xs" c="dimmed">{t('subDesign.officialNotesHint')}</Text>
      {preset.url?.startsWith('https://') && <Anchor size="xs" href={preset.url} target="_blank" rel="noopener noreferrer">{t('subDesign.officialNotesSource')}</Anchor>}
    </Stack>
  </Paper>
}

// The editable document is the base plus options. Inspection produces a separate
// effective design; checking an option never appends generated groups to the draft.
export function SubDesigner() {
  return <SettingsDraftBoundary><Designer /></SettingsDraftBoundary>
}

function Designer() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sub-design'], queryFn: ({ signal }) => api.get<SubDesignData>(endpoint, signal) })
  const [local, setLocal] = useState<SubDesign | null>(null)
  const d = local ?? q.data?.design ?? emptyDesign
  const epoch = useRef(0)
  const loadAbort = useRef<AbortController | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewFormat, setPreviewFormat] = useState('clash')
  const [formatSelection, setFormatSelection] = useState<string[] | null>(null)
  const [saveAsOpen, setSaveAsOpen] = useState(false)
  const [presetName, setPresetName] = useState('')
  useSettingsDirty(local !== null)
  useEffect(() => () => { epoch.current++; loadAbort.current?.abort() }, [])

  const edit = (change: SubDesign | ((current: SubDesign) => SubDesign)) => {
    epoch.current++
    loadAbort.current?.abort()
    setPreview(null)
    setLocal(typeof change === 'function' ? change(structuredClone(d)) : change)
  }
  const snapshot = (): Snapshot => ({ design: structuredClone(d), epoch: epoch.current })
  const saved = (submitted: Snapshot) => {
    if (submitted.epoch === epoch.current) {
      qc.setQueryData<SubDesignData>(['sub-design'], old => old ? { ...old, design: submitted.design } : old)
      setLocal(null)
    }
    void qc.invalidateQueries({ queryKey: ['sub-design'], exact: true })
  }

  const [debounced] = useDebouncedValue(d, 300)
  const inspection = useQuery({
    queryKey: ['sub-design-inspect', debounced],
    queryFn: ({ signal }) => api.post<DesignInspection>(`${endpoint}/inspect`, { design: debounced }, signal),
    enabled: !!q.data && debounced.groups.length > 0,
    retry: false,
    staleTime: 30_000,
  })
  const inspectionCurrent = JSON.stringify(debounced) === JSON.stringify(d) && inspection.isSuccess && !inspection.isFetching
  const support = inspectionCurrent ? inspection.data.formats : []
  const selectedFormats = selectedDesignFormats(support, formatSelection)
  const unsupported = support.filter(item => !item.supported)
  const supportReasons = designSupportReasons(support, {
    imported_rules_mihomo: t('subDesign.supportImported'),
    dns_mihomo: t('subDesign.supportDNS'),
    claude_mihomo: t('subDesign.supportClaude'),
    webrtc_mihomo: t('subDesign.supportWebRTC'),
    quic_mihomo: t('subDesign.supportQUIC'),
    timeout_mihomo: t('subDesign.supportTimeout'),
    'dns-direct-only': t('subDesign.supportDNSDirectOnly'),
    'dns-loon-policy-unverified': t('subDesign.supportDNSLoonPolicy'),
    'dns-qx-policy-unavailable': t('subDesign.supportDNSQXPolicy'),
    invalid_design: detail => t('subDesign.supportInvalidDesign', { detail }),
    inline_rule_unsupported: detail => t('subDesign.supportInlineRule', { detail }),
    remote_rules_unsupported: detail => t('subDesign.supportRemoteRules', { detail }),
    udp_rule_order_unsupported: t('subDesign.supportUDPRuleOrder'),
    group_timeout_unsupported: detail => t('subDesign.supportGroupTimeout', { detail }),
    group_test_url_unsupported: detail => t('subDesign.supportGroupTestURL', { detail }),
    group_test_url_http: t('subDesign.supportHTTPTestURL'),
  })
  const supportWarnings = designSupportWarnings(support, {
    'dns-stash-version': t('subDesign.warningDNSStashVersion'),
    'dns-surge-ip-proxies': t('subDesign.warningDNSSurgeIPProxies'),
    'dns-domestic-suffix-only': t('subDesign.warningDNSDomesticSuffix'),
    'dns-egern-proxy-direct': t('subDesign.warningDNSEgernProxyDirect'),
    native_rule_priority: t('subDesign.warningNativeRulePriority'),
    loon_rule_version: t('subDesign.warningLoonRuleVersion'),
    qx_udp_tunnel: t('subDesign.warningQXUDPTunnel'),
  })

  const loadPreset = useMutation({
    mutationFn: async ({ key }: { key: string; epoch: number }) => {
      loadAbort.current?.abort()
      const controller = new AbortController()
      loadAbort.current = controller
      return api.get<SubDesign>(`${endpoint}/presets/${encodeURIComponent(key)}`, controller.signal)
    },
    onSuccess: (design, request) => {
      if (request.epoch !== epoch.current) return
      setLocal(design)
      setFormatSelection(null)
      setPreview(null)
    },
    onError: error => { if (!(error instanceof Error && error.name === 'AbortError')) toast.err(error) },
  })
  const sync = useMutation({
    mutationFn: () => api.post(`${endpoint}/presets/sync`),
    onSuccess: () => { toast.ok(t('subDesign.synced')); void qc.invalidateQueries({ queryKey: ['sub-design'], exact: true }) },
    onError: toast.err,
  })
  const save = useMutation({
    mutationFn: (submitted: Snapshot) => api.put<SubDesign>(endpoint, { design: submitted.design }),
    onSuccess: (design, submitted) => { toast.ok(t('common.saved')); saved({ ...submitted, design }) },
    onError: toast.err,
  })
  const apply = useMutation({
    mutationFn: (submitted: Snapshot & { formats: string[] }) => api.post(`${endpoint}/apply`, { design: submitted.design, formats: submitted.formats }),
    onSuccess: (_, submitted) => { toast.ok(t('subDesign.applied')); saved(submitted); void qc.invalidateQueries({ queryKey: ['sub-templates'] }); void qc.invalidateQueries({ queryKey: claudeRulesQueryKey, exact: true }) },
    onError: toast.err,
  })
  const doPreview = useMutation({
    mutationFn: (submitted: Snapshot & { format: string }) => api.post<Preview>(`${endpoint}/preview`, { design: submitted.design, format: submitted.format }),
    onSuccess: (result, submitted) => { if (submitted.epoch === epoch.current) setPreview(result) },
    onError: toast.err,
  })
  const saveAs = useMutation({
    mutationFn: (submitted: Snapshot & { name: string }) => api.post<DesignPreset>(`${endpoint}/presets/custom`, { name: submitted.name, design: submitted.design }),
    onSuccess: () => { setSaveAsOpen(false); setPresetName(''); toast.ok(t('subDesign.presetSaved')); void qc.invalidateQueries({ queryKey: ['sub-design'], exact: true }) },
    onError: toast.err,
  })
  const removePreset = useMutation({
    mutationFn: (key: string) => api.del(`${endpoint}/presets/${encodeURIComponent(key)}`),
    onSuccess: () => { toast.ok(t('subDesign.presetDeleted')); void qc.invalidateQueries({ queryKey: ['sub-design'], exact: true }) },
    onError: toast.err,
  })

  const names = [...new Set(d.groups.map(group => group.name).filter(Boolean))]
  const policies = [...new Set([...names, 'DIRECT', 'REJECT'])]
  const catalogue = designCatalogue(q.data?.catalogue ?? [], d)
  const presets = q.data?.presets ?? []
  const presetGroups = [{ source: 'builtin', label: t('subDesign.builtinPresets') }, { source: 'acl4ssr', label: t('subDesign.officialPresets') }, { source: 'custom', label: t('subDesign.customPresets') }].map(({ source, label }) => ({
    group: label,
    items: presets.filter(preset => (preset.source ?? 'builtin') === source).map(preset => ({
      value: preset.key,
      label: preset.key === 'basic' ? t('subDesign.basicPreset') : preset.name,
      disabled: preset.supported === false,
    })),
  })).filter(group => group.items.length > 0)
  const activePreset = presets.find(preset => preset.key === d.source?.key)
  const memberOptions = (index: number) => {
    const group = d.groups[index]
    const servers = [
      { value: '@all', label: t('subDesign.allServers') },
      ...(q.data?.tags ?? []).map(tag => ({ value: `@tag:${tag}`, label: t('subDesign.tag', { tag }) })),
      ...(q.data?.regions ?? []).map(region => ({ value: `@match:${region.match}`, label: region.name })),
    ]
    const serverKeys = new Set(servers.map(item => item.value))
    const groups = [...new Set([...names.filter(name => name !== group.name), 'DIRECT', 'REJECT'])].filter(value => !serverKeys.has(value)).map(value => ({ value, label: value }))
    const known = new Set([...servers, ...groups].map(item => item.value))
    const retained = group.members.filter(value => !known.has(value)).map(value => ({ value, label: value }))
    return [{ group: t('subDesign.memberServers'), items: servers }, { group: t('subDesign.memberGroups'), items: groups }, ...(retained.length ? [{ group: t('subDesign.customMembers'), items: retained }] : [])]
  }
  const moveButtons = (kind: 'groups' | 'rules', index: number, length: number) => <Group gap={2} wrap="nowrap">
    <ActionIcon aria-label={t('subDesign.moveUp')} size="sm" variant="subtle" disabled={index === 0} onClick={() => edit(current => kind === 'groups' ? { ...current, groups: moveDesignItem(current.groups, index, -1) } : { ...current, rules: moveDesignItem(current.rules, index, -1) })}><IconArrowUp size={14} /></ActionIcon>
    <ActionIcon aria-label={t('subDesign.moveDown')} size="sm" variant="subtle" disabled={index === length - 1} onClick={() => edit(current => kind === 'groups' ? { ...current, groups: moveDesignItem(current.groups, index, 1) } : { ...current, rules: moveDesignItem(current.rules, index, 1) })}><IconArrowDown size={14} /></ActionIcon>
    <ActionIcon aria-label={t('common.delete')} size="sm" variant="subtle" color="red" onClick={() => edit(current => kind === 'groups' ? { ...current, groups: current.groups.filter((_, at) => at !== index) } : { ...current, rules: current.rules.filter((_, at) => at !== index) })}><IconTrash size={14} /></ActionIcon>
  </Group>
  const syncedAt = q.data?.library?.synced_at
  const syncTime = syncedAt ? new Date(typeof syncedAt === 'number' ? syncedAt * 1000 : syncedAt).toLocaleString() : ''

  if (q.isPending) return <Group><Loader size="sm" /><Text size="sm">{t('subDesign.loading')}</Text></Group>
  if (!q.data) return <Alert color="red"><Stack gap="xs"><Text size="sm">{errorText(q.error)}</Text><Button size="xs" variant="light" onClick={() => q.refetch()}>{t('subDesign.retry')}</Button></Stack></Alert>

  return <Stack gap="md">
    {q.isError && <Alert color="red"><Group justify="space-between"><Text size="sm">{errorText(q.error)}</Text><Button size="xs" variant="light" onClick={() => q.refetch()}>{t('subDesign.retry')}</Button></Group></Alert>}
    <Card>
      <Stack gap="md">
        <div><Title order={5}>{t('subDesign.title')}</Title><Text size="sm" c="dimmed" mt={4}>{t('subDesign.hint')}</Text></div>
        <Group align="flex-end" wrap="wrap">
          <Select label={t('subDesign.presetLabel')} placeholder={t('subDesign.loadPreset')} data={presetGroups} searchable clearable={false} value={activePreset?.key ?? null} style={{ flex: '1 1 300px', minWidth: 0 }} maxDropdownHeight={360}
            nothingFoundMessage={t('subDesign.noPresetMatch')}
            renderOption={({ option }) => {
              const preset = presets.find(item => item.key === option.value)
              return <Stack gap={2} style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
                <Text size="sm">{option.label}</Text>
                {preset?.source === 'acl4ssr' && <Text size="xs" c="dimmed" lineClamp={2}>{preset.official_notes?.replace(/\n/g, ' · ') || t('subDesign.officialNotesEmpty')}</Text>}
                {preset?.reason && <Text size="xs" c="dimmed">{preset.reason}</Text>}
              </Stack>
            }}
            onChange={key => key && modals.openConfirmModal({ title: t('subDesign.loadPreset'), children: <Stack gap="sm"><OfficialPresetNotes preset={presets.find(item => item.key === key)} /><Text size="sm">{t('subDesign.presetConfirm')}</Text></Stack>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => { epoch.current++; loadPreset.mutate({ key, epoch: epoch.current }) } })} />
          <Button variant="light" leftSection={<IconRefresh size={16} />} loading={sync.isPending} onClick={() => sync.mutate()}>{t('subDesign.syncPresets')}</Button>
          <Button variant="default" leftSection={<IconDeviceFloppy size={16} />} disabled={!d.groups.length} onClick={() => setSaveAsOpen(true)}>{t('subDesign.saveAs')}</Button>
          {activePreset?.source === 'custom' && <ActionIcon size="lg" variant="light" color="red" aria-label={t('subDesign.deletePreset')} onClick={() => modals.openConfirmModal({ title: t('subDesign.deletePreset'), children: <Text size="sm">{t('subDesign.deletePresetConfirm', { name: activePreset.name })}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => removePreset.mutate(activePreset.key) })}><IconTrash size={18} /></ActionIcon>}
        </Group>
        <Text size="xs" c="dimmed">{t('subDesign.syncHint')}</Text>
        {syncTime ? <Text size="xs" c="dimmed">{t('subDesign.lastSync', { time: syncTime, count: q.data.library?.count ?? 0 })} · <Code>{q.data.library?.revision.slice(0, 10)}</Code></Text> : <Text size="xs" c="dimmed">{t('subDesign.notSynced')}</Text>}
        {d.source && <Group gap="xs" wrap="wrap"><Text size="xs" style={{ overflowWrap: 'anywhere' }}>{d.source.url?.startsWith('https://') ? <Anchor size="xs" href={d.source.url} target="_blank" rel="noopener noreferrer">{t('subDesign.loadedSource', { name: d.source.name })}</Anchor> : t('subDesign.loadedSource', { name: d.source.name })}</Text>{d.source.revision && <Code>{d.source.revision.slice(0, 10)}</Code>}{local !== null && <Badge variant="light" color="yellow">{t('subDesign.draft')}</Badge>}</Group>}
        <OfficialPresetNotes preset={activePreset} draftRevision={d.source?.revision} />
        <Text size="xs" c="dimmed">{t('subDesign.sourceCredit')} <Anchor size="xs" href="https://github.com/ACL4SSR/ACL4SSR" target="_blank" rel="noopener noreferrer">ACL4SSR</Anchor> · <Anchor size="xs" href="https://github.com/ACL4SSR/ACL4SSR/blob/master/LICENCE" target="_blank" rel="noopener noreferrer">CC BY-SA 4.0</Anchor></Text>
        <Divider />
        <SubDesignEnhancements design={d} onChange={edit} />
      </Stack>
    </Card>

    <Card>
      <Group justify="space-between" mb="sm"><Title order={6}>{t('subDesign.groups')}</Title><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => edit(current => ({ ...current, groups: [...current.groups, { name: '', type: 'select', members: ['@all'] }] }))}>{t('subDesign.addGroup')}</Button></Group>
      <Text size="xs" c="dimmed" mb="sm">{t('subDesign.groupsHint')}</Text>
      <Stack gap="sm">
        {d.groups.map((group, index) => <Card key={index} withBorder p="sm">
          <Stack gap="xs">
            <Group justify="space-between" wrap="nowrap"><Text size="xs" c="dimmed">{index + 1}{group.region ? ` · ${group.region}` : ''}</Text>{moveButtons('groups', index, d.groups.length)}</Group>
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="xs">
              <TextInput size="xs" label={t('subDesign.groupName')} value={group.name} onChange={event => edit(renameDesignGroup(d, index, event.currentTarget.value))} />
              <Select size="xs" label={t('subDesign.groupType')} data={groupTypes} value={group.type} onChange={value => value && edit(current => { current.groups[index].type = value; return current })} allowDeselect={false} />
            </SimpleGrid>
            <MultiSelect size="xs" label={t('subDesign.members')} data={memberOptions(index)} value={group.members} onChange={value => edit(current => { current.groups[index].members = value; return current })} searchable />
            {group.type !== 'select' && <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="xs">
              <TextInput size="xs" label={t('subDesign.testURL')} value={group.url ?? ''} onChange={event => { const value = event.currentTarget.value; edit(current => { current.groups[index].url = value; return current }) }} />
              <NumberInput size="xs" label={t('subDesign.interval')} min={1} value={group.interval ?? 300} onChange={value => edit(current => { current.groups[index].interval = typeof value === 'number' ? value : undefined; return current })} suffix=" s" />
              <NumberInput size="xs" label={t('subDesign.tolerance')} min={0} value={group.tolerance ?? ''} onChange={value => edit(current => { current.groups[index].tolerance = typeof value === 'number' ? value : undefined; return current })} suffix=" ms" />
            </SimpleGrid>}
          </Stack>
        </Card>)}
        {!d.groups.length && <Text size="sm" c="dimmed">{t('subDesign.noGroups')}</Text>}
      </Stack>
    </Card>

    <Card>
      <Group justify="space-between" mb="sm"><Title order={6}>{t('subDesign.rules')}</Title><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => edit(current => ({ ...current, rules: [...current.rules, { set: catalogue[0]?.key ?? '', policy: policies[0] ?? 'DIRECT' }] }))}>{t('subDesign.addRule')}</Button></Group>
      <Text size="xs" c="dimmed" mb="sm">{t('subDesign.rulesHint')}</Text>
      <Stack gap="xs">
        {d.rules.map((rule, index) => <Card key={index} withBorder p="xs">
          <Group justify="space-between" mb={4}><Text size="xs" c="dimmed">{index + 1}</Text>{moveButtons('rules', index, d.rules.length)}</Group>
          <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="xs">
            <Select size="xs" aria-label={t('subDesign.ruleSet')} data={catalogue.map(item => ({ value: item.key, label: item.name }))} value={rule.set} onChange={value => value && edit(current => { current.rules[index].set = value; return current })} allowDeselect={false} searchable />
            <Select size="xs" aria-label={t('subDesign.rulePolicy')} data={policies} value={rule.policy} onChange={value => value && edit(current => { current.rules[index].policy = value; return current })} allowDeselect={false} searchable />
          </SimpleGrid>
        </Card>)}
        <Select size="xs" label={t('subDesign.final')} description={t('subDesign.finalHint')} data={policies} value={d.final || null} onChange={value => value && edit(current => ({ ...current, final: value }))} allowDeselect={false} searchable />
        {!!d.rule_sets?.length && <Accordion variant="contained">
          <Accordion.Item value="sources"><Accordion.Control>{t('subDesign.importedRules')}</Accordion.Control><Accordion.Panel><Stack gap="sm">
            {d.rule_sets.map((set, index) => <Card key={set.key} withBorder p="xs"><Stack gap="xs">
              <TextInput size="xs" label={t('subDesign.ruleSet')} value={set.name} onChange={event => { const value = event.currentTarget.value; edit(current => { current.rule_sets![index].name = value; return current }) }} />
              {set.inline !== undefined ? <TextInput size="xs" label={t('subDesign.inlineRule')} value={set.inline} onChange={event => { const value = event.currentTarget.value; edit(current => { current.rule_sets![index].inline = value; return current }) }} /> : <TextInput size="xs" label={t('subDesign.ruleURL')} value={set.url ?? ''} onChange={event => { const value = event.currentTarget.value; edit(current => { current.rule_sets![index].url = value; return current }) }} />}
            </Stack></Card>)}
          </Stack></Accordion.Panel></Accordion.Item>
        </Accordion>}
      </Stack>
    </Card>

    <Card>
      <Stack gap="sm">
        <Title order={6}>{t('subDesign.formatSupport')}</Title>
        {d.groups.length > 0 && !inspectionCurrent && !inspection.isError && <Group gap="xs"><Loader size="xs" /><Text size="xs" c="dimmed">{t('subDesign.checking')}</Text></Group>}
        {inspection.isError && <Alert color="red"><Text size="sm">{errorText(inspection.error)}</Text></Alert>}
        {support.length > 0 && <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {support.map(item => <Stack key={item.format} gap={4}>
            <Checkbox label={designFormatLabels[item.format] ?? item.format} checked={selectedFormats.includes(item.format)} disabled={!item.supported} onChange={event => setFormatSelection(event.currentTarget.checked ? [...selectedFormats, item.format] : selectedFormats.filter(format => format !== item.format))} />
            <Text size="xs" c={item.supported ? ((item.warnings?.length || item.warning_codes?.length) ? 'blue' : 'teal') : 'orange'}>{item.supported ? t((item.warnings?.length || item.warning_codes?.length) ? 'subDesign.supportedWithNotes' : 'subDesign.supported') : t('subDesign.unsupported')}</Text>
          </Stack>)}
        </SimpleGrid>}
        {!!unsupported.length && <Alert color="yellow"><Text size="xs">{t('subDesign.unchangedFormats', { formats: unsupported.map(item => designFormatLabels[item.format] ?? item.format).join(', ') })}</Text><List size="xs" spacing={4} mt="xs" style={{ overflowWrap: 'anywhere' }}>{supportReasons.map(reason => <List.Item key={reason}>{reason}</List.Item>)}</List></Alert>}
        {!!supportWarnings.length && <Alert color="blue" title={t('subDesign.clientDifferences')}><Stack gap="xs">{supportWarnings.map(item => <div key={item.format}><Text size="xs" fw={600}>{designFormatLabels[item.format] ?? item.format}</Text><List size="xs" style={{ overflowWrap: 'anywhere' }}>{item.messages.map(message => <List.Item key={message}>{message}</List.Item>)}</List></div>)}</Stack></Alert>}
        {inspectionCurrent && <Text size="xs" c="dimmed">{t('subDesign.effectiveCount', { groups: inspection.data.effective_design.groups.length, rules: inspection.data.effective_design.rules.length })}</Text>}
        <Group justify="space-between" wrap="wrap" align="flex-end">
          <Group gap="xs" align="flex-end">
            <Select size="xs" label={t('subDesign.previewFormat')} data={q.data.formats.map(format => ({ value: format, label: designFormatLabels[format] ?? format }))} value={previewFormat} onChange={value => value && setPreviewFormat(value)} allowDeselect={false} style={{ flex: '1 1 180px' }} />
            <Button size="xs" variant="default" loading={doPreview.isPending} disabled={!inspectionCurrent || !support.some(item => item.format === previewFormat && item.supported)} onClick={() => doPreview.mutate({ ...snapshot(), format: previewFormat })}>{t('subDesign.preview')}</Button>
          </Group>
          <Group gap="xs">
            <Tooltip label={t('subDesign.saveHint')}><Button size="xs" variant="default" loading={save.isPending} disabled={!d.groups.length} onClick={() => save.mutate(snapshot())}>{t('subDesign.save')}</Button></Tooltip>
            <Button size="xs" loading={apply.isPending} disabled={!inspectionCurrent || !selectedFormats.length} onClick={() => {
              const submitted = { ...snapshot(), formats: selectedFormats }
              modals.openConfirmModal({ title: t('subDesign.apply'), children: <Stack gap="xs"><Text size="sm">{t('subDesign.applyConfirm', { formats: submitted.formats.map(format => designFormatLabels[format] ?? format).join(', ') })}</Text><Text size="xs" c="dimmed">{t('subDesign.applyScopeHint')}</Text></Stack>, labels: { confirm: t('subDesign.apply'), cancel: t('common.cancel') }, onConfirm: () => { if (submitted.epoch === epoch.current) apply.mutate(submitted) } })
            }}>{t('subDesign.apply')}</Button>
          </Group>
        </Group>
        <Text size="xs" c="dimmed">{t('subDesign.applyHint')} <Code>{'{{proxy_names:tag=hk}}'}</Code> <Code>{'{{proxy_names:match=HK|香港}}'}</Code></Text>
      </Stack>
    </Card>

    <Modal opened={preview !== null} onClose={() => setPreview(null)} title={preview ? designFormatLabels[preview.format] ?? preview.format : ''} size="xl">
      <Text size="xs" c="dimmed" mb="sm">{t('subDesign.previewHint')}</Text>
      <Code block style={{ maxHeight: '70vh', overflow: 'auto', fontSize: 12, whiteSpace: 'pre' }}>{preview?.text}</Code>
    </Modal>
    <Modal opened={saveAsOpen} onClose={() => setSaveAsOpen(false)} title={t('subDesign.saveAs')} centered>
      <Stack><TextInput label={t('subDesign.presetName')} value={presetName} onChange={event => setPresetName(event.currentTarget.value)} maxLength={120} data-autofocus /><Text size="xs" c="dimmed">{t('subDesign.saveAsHint')}</Text><Group justify="flex-end"><Button variant="default" onClick={() => setSaveAsOpen(false)}>{t('common.cancel')}</Button><Button disabled={!presetName.trim()} loading={saveAs.isPending} onClick={() => saveAs.mutate({ ...snapshot(), name: presetName.trim() })}>{t('common.save')}</Button></Group></Stack>
    </Modal>
  </Stack>
}
