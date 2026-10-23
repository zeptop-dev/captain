import { ActionIcon, Badge, Button, Group, Paper, Select, Stack, Text, TextInput } from '@mantine/core'
import { IconArrowDown, IconArrowUp, IconTrash } from '@tabler/icons-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { bytes } from '../lib/format'
import { moveStorage, recommendedStorage, type ProbeStorage, type ProbeStorageCandidate, type ProbeStorageItem } from '../lib/probe-storage'

export function NodeStorageFields({ value, candidates, onChange, disabled = false }: {
  value: ProbeStorage
  candidates: ProbeStorageCandidate[]
  onChange: (value: ProbeStorage) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const previousManual = useRef<ProbeStorageItem[] | null>(null)
  const [addMount, setAddMount] = useState<string | null>(null)
  const byMount = new Map(candidates.map(candidate => [candidate.mount, candidate]))
  const suggested = recommendedStorage(candidates)
  const selected = new Set(value.items.map(item => item.mount))
  const available = candidates.filter(candidate => !selected.has(candidate.mount))
  const defaultName = (mount: string) => t(`nodeStorage.kind_${byMount.get(mount)?.kind ?? (mount === '/' ? 'system' : 'volume')}`)
  const update = (items: ProbeStorageItem[]) => onChange({ mode: 'manual', items })
  const capacity = (candidate: ProbeStorageCandidate) => `${candidate.used == null ? '—' : bytes(candidate.used)} / ${candidate.total == null ? '—' : bytes(candidate.total)}`
  return <Stack gap="xs" data-testid="node-storage-fields">
    <Text size="sm" fw={500}>{t('nodeStorage.title')}</Text>
    <Text size="xs" c="dimmed">{t('nodeStorage.hint')}</Text>
    <Select label={t('nodeStorage.mode')} value={value.mode} allowDeselect={false} disabled={disabled}
      data={[{ value: 'auto', label: t('nodeStorage.auto') }, { value: 'manual', label: t('nodeStorage.manual') }]}
      onChange={mode => {
        if (mode === 'auto' && value.mode !== 'auto') {
          previousManual.current = value.items.map(item => ({ ...item }))
          onChange({ mode: 'auto', items: [] })
        } else if (mode === 'manual' && value.mode !== 'manual') {
          update(previousManual.current ?? suggested)
        }
        setAddMount(null)
      }} />
    {value.mode === 'auto' ? <>
      <Text size="xs" c="dimmed">{t('nodeStorage.autoHint')}</Text>
      {suggested.length > 0 ? <Stack gap={4}>{suggested.map(item => {
        const candidate = byMount.get(item.mount)!
        return <Group key={item.mount} justify="space-between" gap="xs">
          <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{defaultName(item.mount)} · {item.mount}</Text>
          <Text size="xs" c="dimmed">{capacity(candidate)}</Text>
        </Group>
      })}</Stack> : <Text size="xs" c="dimmed">{t('nodeStorage.noCandidates')}</Text>}
    </> : <>
      <Text size="xs" c="dimmed">{t('nodeStorage.manualHint')}</Text>
      {value.items.length === 0 && <Text size="sm" c="dimmed">{t('nodeStorage.empty')}</Text>}
      {value.items.map((item, index) => {
        const candidate = byMount.get(item.mount)
        return <Paper key={item.mount} withBorder p="sm" radius="sm" data-storage-mount={item.mount}>
          <Stack gap="xs">
            <Group justify="space-between" align="flex-start" gap="xs" wrap="nowrap">
              <Stack gap={2} style={{ minWidth: 0 }}>
                <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{item.mount}</Text>
                <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{candidate ? [candidate.type, candidate.device, capacity(candidate)].filter(Boolean).join(' · ') : t('nodeStorage.unavailable')}</Text>
              </Stack>
              <Group gap={2} wrap="nowrap">
                <ActionIcon type="button" variant="subtle" aria-label={t('nodeStorage.up')} disabled={disabled || index === 0} onClick={() => update(moveStorage(value.items, index, index - 1))}><IconArrowUp size={17} /></ActionIcon>
                <ActionIcon type="button" variant="subtle" aria-label={t('nodeStorage.down')} disabled={disabled || index === value.items.length - 1} onClick={() => update(moveStorage(value.items, index, index + 1))}><IconArrowDown size={17} /></ActionIcon>
                <ActionIcon type="button" variant="subtle" color="red" aria-label={t('nodeStorage.remove')} disabled={disabled} onClick={() => update(value.items.filter((_, i) => i !== index))}><IconTrash size={17} /></ActionIcon>
              </Group>
            </Group>
            <Group gap="xs"><Badge size="xs" variant="light" color={index < 2 ? 'blue' : 'gray'}>{t(index < 2 ? 'nodeStorage.home' : 'nodeStorage.expanded')}</Badge>{!candidate && <Badge size="xs" variant="light" color="orange">{t('nodeStorage.unavailable')}</Badge>}</Group>
            <TextInput label={t('nodeStorage.name')} description={t('nodeStorage.nameHint')} placeholder={defaultName(item.mount)} value={item.name} disabled={disabled} maxLength={32}
              onChange={event => update(value.items.map((entry, i) => i === index ? { ...entry, name: event.currentTarget.value } : entry))} />
          </Stack>
        </Paper>
      })}
      <Group align="flex-end" gap="xs">
        <Select label={t('nodeStorage.addMount')} placeholder={t('nodeStorage.selectMount')} searchable clearable value={addMount} disabled={disabled || available.length === 0 || value.items.length >= 32} style={{ flex: '1 1 220px', minWidth: 0 }}
          data={available.map(candidate => ({ value: candidate.mount, label: `${candidate.mount} · ${t(`nodeStorage.kind_${candidate.kind}`)}` }))}
          onChange={setAddMount} nothingFoundMessage={t('nodeStorage.noCandidates')} />
        <Button type="button" variant="light" disabled={disabled || !addMount || selected.has(addMount) || !byMount.has(addMount) || value.items.length >= 32} onClick={() => {
          if (addMount && byMount.has(addMount) && !selected.has(addMount)) update([...value.items, { mount: addMount, name: '' }])
          setAddMount(null)
        }}>{t('nodeStorage.add')}</Button>
      </Group>
      {available.length === 0 && <Text size="xs" c="dimmed">{t('nodeStorage.noMore')}</Text>}
    </>}
  </Stack>
}
