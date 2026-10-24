import { Alert, Anchor, Badge, Button, Code, Group, Loader, Modal, Paper, ScrollArea, SimpleGrid, Stack, Text } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconRefresh } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { adoptClaudeRules, claudeRulesEndpoint, claudeRulesQueryKey, diffClaudeRules, draftClaudeRules } from '../lib/sub-design'
import type { ClaudeRule, ClaudeRuleLibrary, ClaudeRulesData, ClaudeRuleSnapshot, ClaudeRulesSyncResult, SubDesign } from '../lib/sub-design'

const errorText = (error: unknown) => error instanceof Error ? error.message : String(error)
const timeText = (at?: number) => at ? new Date(at * 1000).toLocaleString() : ''
type Review = {
  kind: 'library' | 'draft'
  library: ClaudeRuleLibrary
  added: ClaudeRule[]
  removed: ClaudeRule[]
  previousRevision: string
  expectedDraft?: string
}

function RuleVersion({ title, snapshot, at, status }: { title: string; snapshot?: ClaudeRuleSnapshot; at?: number; status?: string }) {
  const { t } = useTranslation()
  return <Paper withBorder p="sm" radius="md" style={{ minWidth: 0 }}>
    <Stack gap={5}>
      <Text size="xs" c="dimmed">{title}</Text>
      {snapshot && <>
        <Text size="sm" fw={500}>{t(snapshot.source === 'builtin' ? 'subDesign.claudeRulesBuiltin' : 'subDesign.claudeRulesV2fly')}</Text>
        <Group gap="xs" wrap="wrap"><Code title={snapshot.revision}>{snapshot.revision.slice(0, 10)}</Code><Text size="xs">{t('subDesign.claudeRulesCount', { count: snapshot.rules.length })}</Text></Group>
      </>}
      {status && <Text size="xs" c="dimmed">{status}</Text>}
      {!!at && <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{timeText(at)}</Text>}
    </Stack>
  </Paper>
}

function RuleChanges({ added, removed }: { added: ClaudeRule[]; removed: ClaudeRule[] }) {
  const { t } = useTranslation()
  return <>
    <Group gap="xs"><Badge color="teal" variant="light">{t('subDesign.claudeRulesAdded', { count: added.length })}</Badge><Badge color="red" variant="light">{t('subDesign.claudeRulesRemoved', { count: removed.length })}</Badge></Group>
    {!added.length && !removed.length ? <Text size="sm" c="dimmed">{t('subDesign.claudeRulesNoChanges')}</Text> : <ScrollArea.Autosize mah={280} type="auto" offsetScrollbars>
      <Stack gap="sm">
        {[{ rules: added, key: 'claudeRulesAdded', color: 'teal' }, { rules: removed, key: 'claudeRulesRemoved', color: 'red' }].filter(item => item.rules.length).map(item => <Stack gap={5} key={item.key}>
          <Text size="sm" fw={500} c={item.color}>{t(`subDesign.${item.key}`, { count: item.rules.length })}</Text>
          {item.rules.map(rule => <Text size="xs" key={`${rule.type}:${rule.value}`} style={{ overflowWrap: 'anywhere' }}><Text component="span" c="dimmed">{t(rule.type === 'domain' ? 'subDesign.claudeRulesSuffix' : 'subDesign.claudeRulesExact')} · </Text>{rule.value}</Text>)}
        </Stack>)}
      </Stack>
    </ScrollArea.Autosize>}
  </>
}

export function SubDesignClaudeRules({ design, onChange, data, loading, error, onRetry }: {
  design: SubDesign
  onChange: (next: SubDesign) => void
  data?: ClaudeRulesData
  loading: boolean
  error: unknown
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [review, setReview] = useState<Review | null>(null)
  const active = useRef(true)
  const syncAbort = useRef<AbortController | null>(null)
  useEffect(() => {
    active.current = true
    return () => { active.current = false; syncAbort.current?.abort() }
  }, [])
  const sync = useMutation({
    mutationFn: async () => {
      await qc.cancelQueries({ queryKey: claudeRulesQueryKey, exact: true })
      if (!active.current) throw new DOMException('Aborted', 'AbortError')
      const controller = new AbortController()
      syncAbort.current = controller
      return api.post<ClaudeRulesSyncResult>(`${claudeRulesEndpoint}/sync`, undefined, controller.signal)
    },
    onSuccess: async result => {
      await qc.cancelQueries({ queryKey: claudeRulesQueryKey, exact: true })
      // Updating this cache never updates the separate editable design query.
      qc.setQueryData<ClaudeRulesData>(claudeRulesQueryKey, old => old ? { ...old, library: result.library } : old)
      if (active.current) setReview({ kind: 'library', library: structuredClone(result.library), added: result.added ?? [], removed: result.removed ?? [], previousRevision: result.previous_revision })
    },
    onSettled: () => { void qc.invalidateQueries({ queryKey: claudeRulesQueryKey, exact: true }) },
  })
  const draft = data ? draftClaudeRules(design, data.builtin) : design.enhancements?.claude_rules
  const openDraftReview = (library: ClaudeRuleLibrary) => {
    if (!draft) return
    setReview({ kind: 'draft', library: structuredClone(library), ...diffClaudeRules(draft, library.snapshot), previousRevision: draft.revision, expectedDraft: JSON.stringify(design) })
  }
  const changedDraft = review?.kind === 'draft' && review.expectedDraft !== JSON.stringify(design)
  const sameVersion = !!draft && !!data && draft.source === data.library.snapshot.source && draft.revision === data.library.snapshot.revision && !diffClaudeRules(draft, data.library.snapshot).added.length && !diffClaudeRules(draft, data.library.snapshot).removed.length

  return <Paper withBorder p="sm" radius="md">
    <Stack gap="sm">
      <Group justify="space-between" wrap="wrap">
        <Text size="sm" fw={600}>{t('subDesign.claudeRulesTitle')}</Text>
        <Group gap="xs" wrap="wrap">
          <Button size="xs" variant="light" leftSection={<IconRefresh size={15} />} loading={sync.isPending} onClick={() => sync.mutate()}>{t('subDesign.claudeRulesSync')}</Button>
          <Button size="xs" variant="default" disabled={!data || !draft || sameVersion || sync.isPending} onClick={() => data && openDraftReview(data.library)}>{t('subDesign.claudeRulesReviewDraft')}</Button>
        </Group>
      </Group>
      <Text size="xs" c="dimmed">{t('subDesign.claudeRulesSyncHint')}</Text>
      {loading && <Group gap="xs"><Loader size="xs" /><Text size="xs" c="dimmed">{t('subDesign.loading')}</Text></Group>}
      {!!error && <Alert color="red"><Stack gap="xs"><Text size="xs">{errorText(error)}</Text><Button size="xs" variant="light" onClick={onRetry}>{t('subDesign.retry')}</Button></Stack></Alert>}
      {sync.isError && <Alert color="red"><Text size="xs">{t('subDesign.claudeRulesSyncFailed')}</Text><Text size="xs" style={{ overflowWrap: 'anywhere' }}>{errorText(sync.error)}</Text></Alert>}
      {data && <>
        <SimpleGrid cols={{ base: 1, md: 3 }} spacing="xs">
          <RuleVersion title={t('subDesign.claudeRulesLibrary')} snapshot={data.library.snapshot} at={data.library.synced_at} status={!data.library.synced_at ? t('subDesign.claudeRulesNotSynced') : undefined} />
          <RuleVersion title={t('subDesign.claudeRulesDraft')} snapshot={draft} status={t(sameVersion ? 'subDesign.claudeRulesDraftLatest' : 'subDesign.claudeRulesDraftPinned')} />
          <RuleVersion title={t('subDesign.claudeRulesApplied')} snapshot={data.applied.status === 'applied' ? (data.applied.snapshot ?? data.builtin) : undefined} at={data.applied.applied_at} status={data.applied.status === 'unknown' ? t('subDesign.claudeRulesAppliedUnknown') : data.applied.status === 'disabled' ? t('subDesign.claudeRulesAppliedDisabled') : undefined} />
        </SimpleGrid>
        <Text size="xs" c="dimmed">{t('subDesign.claudeRulesAppliedHint')}</Text>
        <Group gap="xs" wrap="wrap">
          {data.library.url?.startsWith('https://') && <Anchor size="xs" href={data.library.url} target="_blank" rel="noopener noreferrer">{t('subDesign.claudeRulesSource')}</Anchor>}
          {data.library.license_url?.startsWith('https://') && <Anchor size="xs" href={data.library.license_url} target="_blank" rel="noopener noreferrer">{t('subDesign.claudeRulesLicense')}</Anchor>}
        </Group>
      </>}
    </Stack>
    <Modal opened={review !== null} onClose={() => setReview(null)} title={t(review?.kind === 'draft' ? 'subDesign.claudeRulesDraftReviewTitle' : 'subDesign.claudeRulesSyncReviewTitle')} size="lg">
      {review && <Stack gap="md">
        <Text size="sm">{t(review.kind === 'draft' ? 'subDesign.claudeRulesAdoptHint' : 'subDesign.claudeRulesSyncedHint')}</Text>
        <Group gap="xs" wrap="wrap"><Code>{review.previousRevision.slice(0, 10) || '—'}</Code><Text size="xs">→</Text><Code>{review.library.snapshot.revision.slice(0, 10)}</Code></Group>
        <RuleChanges added={review.added} removed={review.removed} />
        {changedDraft && <Alert color="yellow"><Text size="sm">{t('subDesign.claudeRulesDraftChanged')}</Text></Alert>}
        <Group justify="flex-end" wrap="wrap">
          <Button variant="default" onClick={() => setReview(null)}>{t('common.cancel')}</Button>
          {review.kind === 'library' ? <Button disabled={!draft} onClick={() => openDraftReview(review.library)}>{t('subDesign.claudeRulesReviewDraft')}</Button> : <Button disabled={changedDraft} onClick={() => {
            const next = adoptClaudeRules(design, review.expectedDraft ?? '', review.library.snapshot)
            if (next) { onChange(next); setReview(null) }
          }}>{t('subDesign.claudeRulesAdopt')}</Button>}
        </Group>
      </Stack>}
    </Modal>
  </Paper>
}
