import { useState } from 'react'
import { Box, Group, Modal, Progress, SimpleGrid, Text, UnstyledButton } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { Node } from '../lib/api'
import { storageCapacity, storageFullCapacity, storageItems, storagePercent, type StorageItem } from '../lib/storage'

function StorageMeter({ item, full = false }: { item: StorageItem; full?: boolean }) {
  return <Box style={{ minWidth: 0 }} data-storage-volume={item.id}>
    <Group justify="space-between" gap={4} wrap="nowrap">
      <Text size="xs" c="dimmed" truncate={!full} style={{ minWidth: 0, overflowWrap: 'anywhere' }}>{full ? item.detailLabel : item.label}</Text>
      <Text size="xs" fw={600} style={{ flexShrink: 0 }}>{storagePercent(item.percent)}</Text>
    </Group>
    <Progress value={item.percent ?? 0} size="xs" color={item.percent !== null && item.percent > 90 ? 'red' : item.percent !== null && item.percent > 70 ? 'orange' : undefined} />
    <Text size="xs" c="dimmed" mt={2} truncate={!full} data-storage-capacity>{full ? storageFullCapacity(item) : storageCapacity(item)}</Text>
  </Box>
}

export function StorageVolumes({ node }: { node: Node }) {
  const { t } = useTranslation()
  const items = storageItems(node, key => t(key))
  return items.length ? <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md" data-storage-volumes>
    {items.map(item => <StorageMeter key={item.id} item={item} full />)}
  </SimpleGrid> : <Text size="xs" c="dimmed" data-storage-empty>{t('storage.empty')}</Text>
}

export function StorageSummary({ node }: { node: Node }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const items = storageItems(node, key => t(key))
  const more = items.length - 2
  return <Box mt="sm" style={{ minWidth: 0 }} data-storage-summary onClick={event => event.stopPropagation()} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') event.stopPropagation() }}>
    <UnstyledButton w="100%" aria-label={t('storage.viewAll')} data-storage-open onClick={() => setOpen(true)}>
      <Group justify="space-between" mb={2}><Text size="xs" c="dimmed">{t('storage.title')}</Text><Text size="xs" c="dimmed">{more > 0 ? t('storage.more').replace('{count}', String(more)) : '\u00a0'}</Text></Group>
      <Box h={44}>{items.length ? <SimpleGrid cols={Math.min(items.length, 2)} spacing="sm">{items.slice(0, 2).map(item => <StorageMeter key={item.id} item={item} />)}</SimpleGrid> : <Text size="xs" c="dimmed">—</Text>}</Box>
    </UnstyledButton>
    <Modal opened={open} onClose={() => setOpen(false)} title={t('storage.title')} centered>
      <Text size="xs" c="dimmed" mb="md">{t('storage.hint')}</Text>
      <StorageVolumes node={node} />
    </Modal>
  </Box>
}
