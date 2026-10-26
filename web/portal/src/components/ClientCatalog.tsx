import { Alert, Anchor, Badge, Button, Card, Group, NativeSelect, SimpleGrid, Stack, Text } from '@mantine/core'
import { IconArrowUpRight, IconDownload } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { clientDownloadURL, clientFileKey, clientFileLabel, clientSubscriptionFormat, clientsForPlatform, selectedClientFile, type LegacyClientDownload, type PublishedClient } from '../../../shared/client-downloads'
import classes from './ClientCatalog.module.css'

function CatalogCard({ client }: { client: PublishedClient }) {
  const { t } = useTranslation()
  const [selection, setSelection] = useState('')
  const file = selectedClientFile(client.downloads, selection)
  const url = file && clientDownloadURL(file.url)
  const homepage = clientDownloadURL(client.homepage)
  const format = clientSubscriptionFormat(client.format)
  return <Card className={classes.card}>
    <Group justify="space-between" gap="xs"><Text fw={600}>{client.name}</Text>{client.recommended && <Badge variant="light">{t('clientCatalog.recommended')}</Badge>}</Group>
    <Text size="sm" c="dimmed" mt="xs">{t(`clientCatalog.format.${format}`)}</Text>
    <Group gap="sm" mt="sm"><Text size="xs" c="dimmed">{t(`clientCatalog.source.${client.source}`)}</Text>{(file?.version || client.version) && <Badge color="gray" variant="light">{file?.version || client.version}</Badge>}</Group>
    {client.downloads.length > 1 ? <NativeSelect mt="md" label={t('clientCatalog.package')} value={file ? clientFileKey(file) : ''} onChange={event => setSelection(event.target.value)} data={[{ value: '', label: t('clientCatalog.choosePackage') }, ...client.downloads.map(item => ({ value: clientFileKey(item), label: clientFileLabel(item) }))]} /> : file && <Text size="xs" c="dimmed" mt="md" className={classes.file}>{clientFileLabel(file)}</Text>}
    <Group mt="md" gap="sm">{url ? <Button size="sm" variant="light" component="a" href={url} target="_blank" rel="noopener noreferrer" leftSection={<IconDownload size={16} />}>{client.source === 'store' ? t('clientCatalog.openStore') : t('clientCatalog.download')}</Button> : <Button size="sm" disabled>{client.downloads.length > 1 ? t('clientCatalog.choosePackage') : t('clientCatalog.unavailable')}</Button>}{homepage && <Anchor href={homepage} target="_blank" rel="noopener noreferrer" size="xs">{t('clientCatalog.project')} ↗</Anchor>}</Group>
    {client.source === 'store' && <Text mt="sm" size="xs" c="dimmed">{t('clientCatalog.storeNote')}</Text>}
  </Card>
}

export function ClientCatalog({ clients, platform, compact = false }: { clients: PublishedClient[]; platform: string; compact?: boolean }) {
  const { t } = useTranslation()
  const visible = clientsForPlatform(clients, platform)
  const recommended = visible.filter(client => client.recommended)
  const alternatives = visible.filter(client => !client.recommended)
  return <Stack gap="md">
    <SimpleGrid cols={{ base: 1, lg: compact ? 1 : 2 }}>{(recommended.length ? recommended : alternatives).map(client => <CatalogCard key={`${client.id}:${platform}`} client={client} />)}</SimpleGrid>
    {recommended.length > 0 && alternatives.length > 0 && <details key={platform} className={classes.alternatives}><summary>{t('clientCatalog.otherChoices', { count: alternatives.length })}</summary><SimpleGrid cols={{ base: 1, lg: compact ? 1 : 2 }} mt="sm">{alternatives.map(client => <CatalogCard key={`${client.id}:${platform}`} client={client} />)}</SimpleGrid></details>}
  </Stack>
}

export function LegacyDownloads({ items }: { items: LegacyClientDownload[] }) {
  const { t } = useTranslation()
  if (!items.length) return null
  return <Stack gap="sm">
    <Text size="xs" c="dimmed">{t('clientCatalog.customLinks')}</Text>
    {items.map((item, index) => {
      const url = clientDownloadURL(item.url)
      return <Group key={`${item.name}:${index}`} className={classes.legacy} align="flex-start" justify="space-between" wrap="wrap"><div><Text fw={500} size="sm">{item.name}</Text>{item.note && <Text size="xs" c="dimmed" mt={4}>{item.note}</Text>}</div>{url ? <Button component="a" href={url} target="_blank" rel="noopener noreferrer" size="compact-sm" variant="light" rightSection={<IconArrowUpRight size={14} />}>{t('clientCatalog.download')}</Button> : <Alert color="gray" p="xs">{t('clientCatalog.unavailable')}</Alert>}</Group>
    })}
  </Stack>
}
