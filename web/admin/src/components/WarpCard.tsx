import { Badge, Button, Code, Group, Stack, Text, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../lib/notify'

// A registered WARP identity as the node reports it (no secrets).
export interface WarpAccount { id: string; license?: string; peer_public_key: string; endpoint: string; addresses: string[]; registered_at: string }

// Sites that commonly refuse VPS addresses; the template routes them
// through WARP and leaves everything else alone.
export const warpTemplate: { key: string; domains: string[] }[] = [
  { key: 'ai', domains: ['openai.com', 'chatgpt.com', 'oaistatic.com', 'oaiusercontent.com', 'anthropic.com', 'claude.ai', 'gemini.google.com', 'aistudio.google.com', 'x.ai', 'grok.com'] },
  { key: 'streaming', domains: ['netflix.com', 'nflxvideo.net', 'nflximg.net', 'nflxext.com', 'disneyplus.com', 'disney-plus.net', 'hulu.com', 'max.com', 'hbomax.com', 'spotify.com', 'primevideo.com'] },
]

// WarpCard drives registration and adds the outbound + template rules to a
// routing draft. The transport differs per host (standalone API vs a node
// job through Captain), so the callbacks are injected.
export function WarpCard({ queryKey, load, register, setLicense, remove, hasOutbound, onAddOutbound, onAddTemplate, readOnly }: {
  queryKey: unknown[]
  load: () => Promise<WarpAccount | null>
  register: (license: string) => Promise<WarpAccount>
  setLicense?: (license: string) => Promise<WarpAccount>
  remove?: () => Promise<unknown>
  hasOutbound: boolean
  onAddOutbound: () => void
  onAddTemplate: (keys: string[]) => void
  readOnly?: boolean
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey, queryFn: load })
  const [license, setLic] = useState('')
  const reg = useMutation({ mutationFn: () => register(license), onSuccess: () => { toast.ok(t('warp.registered')); setLic(''); qc.invalidateQueries({ queryKey }) }, onError: toast.err })
  const lic = useMutation({ mutationFn: () => setLicense!(license), onSuccess: () => { toast.ok(t('warp.licenseApplied')); setLic(''); qc.invalidateQueries({ queryKey }) }, onError: toast.err })
  const del = useMutation({ mutationFn: () => remove!(), onSuccess: () => qc.invalidateQueries({ queryKey }), onError: toast.err })
  const acct = q.data
  return (
    <Stack gap="xs" p="sm" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 12 }}>
      <Group justify="space-between">
        <div><Text size="sm" fw={600}>{t('warp.title')}</Text><Text size="xs" c="dimmed">{t('warp.hint')}</Text></div>
        {acct ? <Badge color={acct.license ? 'grape' : 'teal'}>{acct.license ? 'WARP+' : t('warp.free')}</Badge> : <Badge color="gray">{t('warp.notRegistered')}</Badge>}
      </Group>
      {acct && (
        <Group gap="lg">
          <div><Text size="xs" c="dimmed">{t('warp.addresses')}</Text><Text size="xs" ff="monospace">{acct.addresses.join(' · ')}</Text></div>
          <div><Text size="xs" c="dimmed">{t('warp.endpoint')}</Text><Text size="xs" ff="monospace">{acct.endpoint}</Text></div>
        </Group>
      )}
      {!readOnly && (
        <Group align="flex-end" wrap="nowrap">
          <TextInput size="xs" label={t('warp.license')} description={t('warp.licenseHint')} placeholder="xxxxxxxx-xxxxxxxx-xxxxxxxx" style={{ flex: 1 }} value={license} onChange={(e) => setLic(e.currentTarget.value)} />
          {!acct && <Button size="xs" mb={2} loading={reg.isPending} onClick={() => reg.mutate()}>{t('warp.register')}</Button>}
          {acct && setLicense && <Button size="xs" mb={2} variant="light" loading={lic.isPending} disabled={!license.trim()} onClick={() => lic.mutate()}>{t('warp.applyLicense')}</Button>}
          {acct && <Button size="xs" mb={2} variant="default" loading={reg.isPending} onClick={() => reg.mutate()}>{t('warp.reregister')}</Button>}
          {acct && remove && <Button size="xs" mb={2} variant="subtle" color="red" loading={del.isPending} onClick={() => del.mutate()}>{t('common.delete')}</Button>}
        </Group>
      )}
      {acct && !readOnly && (
        <Group gap="xs">
          <Button size="xs" variant="light" disabled={hasOutbound} onClick={onAddOutbound}>{hasOutbound ? t('warp.outboundAdded') : t('warp.addOutbound')}</Button>
          <Button size="xs" variant="default" disabled={!hasOutbound} onClick={() => onAddTemplate(warpTemplate.map((g) => g.key))}>{t('warp.addTemplate')}</Button>
          <Text size="xs" c="dimmed">{t('warp.templateHint')} <Code>{warpTemplate.flatMap((g) => g.domains).slice(0, 4).join(', ')}…</Code></Text>
        </Group>
      )}
    </Stack>
  )
}
