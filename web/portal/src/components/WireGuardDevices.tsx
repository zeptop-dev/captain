import { Alert, Button, Card, Code, Divider, Group, Menu, Modal, Select, Stack, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDownload, IconPlus } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type WireGuardDevice, type WireGuardDevicesResponse } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { when } from '../lib/format'
import { toast } from '../lib/notify'

function configURL(url: string, entry: string) {
  const result = new URL(url, window.location.origin)
  result.searchParams.set('client', 'wireguard')
  result.searchParams.set('entry', entry)
  return result.toString()
}

function clientURL(url: string, client: string, entry?: string | null) {
  const result = new URL(url, window.location.origin)
  result.searchParams.set('client', client)
  if (entry) result.searchParams.set('entry', entry)
  return result.toString()
}

function importLinks(url: string) {
  return [
    { name: 'Clash / mihomo', href: `clash://install-config?url=${encodeURIComponent(clientURL(url, 'clash'))}` },
    { name: 'sing-box', href: `sing-box://import-remote-profile?url=${encodeURIComponent(clientURL(url, 'singbox'))}` },
  ]
}

// Device credentials are only shown by the server to their owner or a writer.
// Keep device membership separate from HWID subscription-fetch limits.
export function WireGuardDevices({ endpoint, canWrite = true, titleOrder = 4, onModalChange }: { endpoint: string; canWrite?: boolean; titleOrder?: 4 | 6; onModalChange?: (opened: boolean) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const queryKey = ['wireguard-devices', endpoint]
  const q = useQuery({ queryKey, queryFn: () => api.get<WireGuardDevicesResponse>(endpoint), refetchInterval: 15_000 })
  const [editor, setEditor] = useState<WireGuardDevice | 'new' | null>(null)
  const [name, setName] = useState('')
  const [revoking, setRevoking] = useState<WireGuardDevice | null>(null)
  const [sharingID, setSharingID] = useState<number | null>(null)
  const [entryID, setEntryID] = useState<string | null>(null)
  const [client, setClient] = useState('clash')
  const sharing = q.data?.devices.find(d => d.id === sharingID)
  const modalOpened = editor !== null || revoking !== null || (canWrite && !!sharing?.subscription_url)
  useEffect(() => {
    onModalChange?.(modalOpened)
    return () => onModalChange?.(false)
  }, [modalOpened, onModalChange])
  const entries = q.data?.entries ?? []
  const selectedEntry = entryID || null
  const selectedEntryAvailable = !selectedEntry || entries.some(e => String(e.id) === selectedEntry)
  const sharingURL = sharing?.subscription_url ? clientURL(sharing.subscription_url, client, selectedEntry) : ''
  const invalidate = () => qc.invalidateQueries({ queryKey })
  const save = useMutation({
    mutationFn: ({ id, name: deviceName }: { id?: number; name: string }) => id === undefined
      ? api.post<WireGuardDevice>(endpoint, { name: deviceName })
      : api.patch<WireGuardDevice>(`${endpoint}/${id}`, { name: deviceName }),
    onSuccess: async (device) => {
      const creating = editor === 'new'
      setEditor(null)
      setName('')
      qc.setQueryData<WireGuardDevicesResponse>(queryKey, current => current ? { ...current, devices: [...current.devices.filter(d => d.id !== device.id), device] } : current)
      await invalidate()
      if (creating) {
        const choices = qc.getQueryData<WireGuardDevicesResponse>(queryKey)?.entries ?? []
        setEntryID(choices.length === 1 ? String(choices[0].id) : '')
        setSharingID(device.id)
      }
      toast.ok(t(creating ? 'wgDevices.created' : 'wgDevices.saved'))
    },
    onError: toast.err,
  })
  const revoke = useMutation({
    mutationFn: (id: number) => api.del(`${endpoint}/${id}`),
    onSuccess: async (_, id) => {
      setRevoking(null)
      qc.setQueryData<WireGuardDevicesResponse>(queryKey, current => current ? { ...current, devices: current.devices.filter(d => d.id !== id) } : current)
      await invalidate()
      toast.ok(t('wgDevices.revoked'))
    },
    onError: toast.err,
  })
  const download = useMutation({
    mutationFn: async ({ url, filename }: { url: string; filename: string }) => {
      const response = await fetch(url, { credentials: 'same-origin' })
      if (!response.ok) {
        const error = await response.json().catch(() => null)
        throw new ApiError(response.status, error?.error ?? response.statusText)
      }
      const content = await response.text()
      if (!content.includes('[Interface]') || !content.includes('[Peer]')) throw new Error(t('wgDevices.configUnavailable'))
      const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
      const objectURL = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = filename.replace(/[^\p{L}\p{N}._-]/gu, '_') + '.conf'
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(objectURL), 1000)
    },
    onError: toast.err,
  })

  const devices = q.data?.devices ?? []
  const atLimit = !!q.data && (q.data.remaining !== undefined ? q.data.remaining <= 0 : q.data.limit > 0 && devices.length >= q.data.limit)
  const reason = q.data?.reason || 'unavailable'
  // Keep polling while hidden so newly available nodes can reveal the card.
  // Fetch errors and open editors must remain visible instead of looking empty.
  if (q.isPending || (q.isSuccess && !q.data.available && devices.length === 0 && !modalOpened)) return null
  return <Card id="wireguard-devices"><Stack gap="sm">
    <Group justify="space-between" align="flex-start">
      <div style={{ flex: '1 1 220px', minWidth: 0 }}>
        <Title order={titleOrder}>{t('wgDevices.title')}</Title>
        <Text size="sm" c="dimmed">{t('wgDevices.hint')}</Text>
      </div>
      {canWrite && <Button size="xs" variant="light" leftSection={<IconPlus size={14} />} disabled={!q.data?.available || atLimit} onClick={() => { setName(''); setEditor('new') }}>{t('wgDevices.create')}</Button>}
    </Group>
    {q.isError && <Alert color="red">{t('wgDevices.loadError')} <Button size="compact-xs" variant="subtle" onClick={() => q.refetch()}>{t('wgDevices.retry')}</Button></Alert>}
    {q.data && <>
      {!q.data.available && <Alert color="gray">{t(`wgDevices.reasons.${reason}`, { defaultValue: t('wgDevices.reasons.unavailable') })}</Alert>}
      <Text size="xs" c={atLimit ? 'orange' : 'dimmed'}>{t('wgDevices.count', { count: devices.length, limit: q.data.limit })}{q.data.remaining !== undefined ? ` · ${t('wgDevices.remaining', { count: q.data.remaining })}` : ''}{atLimit ? ` · ${t('wgDevices.limitReached')}` : ''}</Text>
      {q.data.shared_limit !== undefined && <Text size="xs" c="dimmed">{t('wgDevices.sharedHint')} {t('wgDevices.sharedUsage', { used: q.data.shared_used ?? 0, limit: q.data.shared_limit > 0 ? q.data.shared_limit : t('wgDevices.unlimited') })}</Text>}
      {q.data.legacy_enabled && <Text size="xs" c="dimmed">{t('wgDevices.legacyHint')}</Text>}
      {devices.length === 0 && <Text size="sm" c="dimmed">{t('wgDevices.empty')}</Text>}
      {devices.map(device => <Stack key={device.id} gap={5} p="sm" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 'var(--mantine-radius-md)' }}>
        <Group justify="space-between" align="flex-start">
          <Text fw={600} size="sm" style={{ overflowWrap: 'anywhere', flex: '1 1 140px', minWidth: 0 }}>{device.name}</Text>
          {canWrite && <Group gap={4}>
            {device.subscription_url && <Button size="compact-xs" variant="light" onClick={() => { setEntryID(entries.length === 1 ? String(entries[0].id) : ''); setSharingID(device.id) }}>{t('wgDevices.subscription')}</Button>}
            <Button size="compact-xs" variant="subtle" onClick={() => { setName(device.name); setEditor(device) }}>{t('wgDevices.rename')}</Button>
            <Button size="compact-xs" variant="subtle" color="red" onClick={() => setRevoking(device)}>{t('wgDevices.revoke')}</Button>
          </Group>}
        </Group>
        <Text size="xs" c="dimmed">{t('wgDevices.createdAt', { date: when(device.created_at) })}</Text>
        <Text size="xs" c="dimmed">{t('wgDevices.lastUsedAt', { date: device.last_used_at ? when(device.last_used_at) : t('wgDevices.neverUsed') })}</Text>
      </Stack>)}
      {canWrite ? <Text size="xs" c="dimmed">{t('wgDevices.syncHint')}</Text> : <Text size="xs" c="dimmed">{t('wgDevices.readOnly')}</Text>}
    </>}

    <Modal opened={editor !== null} onClose={() => { if (!save.isPending) setEditor(null) }} title={t(editor === 'new' ? 'wgDevices.create' : 'wgDevices.rename')}>
      <form onSubmit={e => { e.preventDefault(); if (name.trim() && editor) save.mutate({ ...(editor === 'new' ? {} : { id: editor.id }), name: name.trim() }) }}>
        <Stack>
          <TextInput autoFocus label={t('wgDevices.name')} placeholder={t('wgDevices.namePlaceholder')} value={name} onChange={e => setName(e.currentTarget.value)} maxLength={64} required disabled={save.isPending} />
          <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={() => setEditor(null)}>{t('wgDevices.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={!name.trim()}>{t(editor === 'new' ? 'wgDevices.create' : 'wgDevices.save')}</Button></Group>
        </Stack>
      </form>
    </Modal>
    <Modal opened={revoking !== null} onClose={() => { if (!revoke.isPending) setRevoking(null) }} title={t('wgDevices.revoke')}>
      <Stack><Text size="sm">{t('wgDevices.revokeHint', { name: revoking?.name })}</Text><Group justify="flex-end"><Button variant="default" disabled={revoke.isPending} onClick={() => setRevoking(null)}>{t('wgDevices.cancel')}</Button><Button color="red" loading={revoke.isPending} onClick={() => revoking && revoke.mutate(revoking.id)}>{t('wgDevices.revoke')}</Button></Group></Stack>
    </Modal>
    <Modal opened={canWrite && !!sharing?.subscription_url} onClose={() => setSharingID(null)} title={sharing?.name} size="lg">
      {sharing?.subscription_url && <Stack>
        <Text size="sm">{t('wgDevices.linkHint')}</Text>
        <Select label={t('wgDevices.node')} placeholder={t('wgDevices.allNodes')} description={t('wgDevices.selectionHint')} searchable clearable data={[...entries.map(entry => ({ value: String(entry.id), label: entry.name })), ...(!selectedEntryAvailable && selectedEntry ? [{ value: selectedEntry, label: t('wgDevices.selectedNodeUnavailable'), disabled: true }] : [])]} value={selectedEntry} onChange={value => setEntryID(value ?? '')} />
        <Select label={t('wgDevices.clientFormat')} data={[{ value: 'clash', label: 'Clash / mihomo' }, { value: 'stash', label: 'Stash' }, { value: 'singbox', label: 'sing-box' }, { value: 'egern', label: 'Egern' }]} value={client} onChange={value => value && setClient(value)} allowDeselect={false} />
        <Code block style={{ overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' }}>{sharingURL}</Code>
        <Group>
          <Button disabled={!selectedEntryAvailable} onClick={async () => { if (await copyText(sharingURL)) toast.ok(t('wgDevices.copied')) }}>{t('wgDevices.copy')}</Button>
          <Menu shadow="md"><Menu.Target><Button disabled={!selectedEntryAvailable} variant="light" leftSection={<IconDownload size={16} />}>{t('wgDevices.import')}</Button></Menu.Target><Menu.Dropdown>{importLinks(sharingURL).map(link => <Menu.Item key={link.name} component="a" href={link.href}>{link.name}</Menu.Item>)}</Menu.Dropdown></Menu>
        </Group>
        <Divider />
        <Text fw={600} size="sm">{t('wgDevices.nativeTitle')}</Text>
        <Text size="xs" c="dimmed">{t('wgDevices.nativeHint')}</Text>
        <Group justify="flex-end"><Button variant="light" loading={download.isPending} disabled={!selectedEntry || !selectedEntryAvailable} onClick={() => { if (selectedEntry) download.mutate({ url: configURL(sharing.subscription_url!, selectedEntry), filename: sharing.name }) }}>{t('wgDevices.download')}</Button></Group>
      </Stack>}
    </Modal>
  </Stack></Card>
}
