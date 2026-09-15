import { Button, Card, Group, JsonInput, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput, UnstyledButton } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Group as UGroup, Inbound, Ingress } from '../lib/api'
import { IngressFields, emptyIngress, type IngressValues } from './IngressesCard'
import { RealityScan, type RealityResult } from './RealityScan'
import { api, type NodeJob } from '../lib/api'

// REALITY helpers over the settings JSON.
type TLSSettings = { mode?: number; server_name?: string; reality?: { handshake_server?: string; handshake_port?: number; fallback_limit?: { off?: boolean; after_bytes?: number; bytes_per_sec?: number } } }
function tlsOf(settings: string): TLSSettings | undefined { try { return JSON.parse(settings || '{}').tls } catch { return undefined } }
function patchReality(settings: string, patch: { server_name?: string; handshake_server?: string; handshake_port?: number; fallback_limit?: { off?: boolean; after_bytes?: number; bytes_per_sec?: number } | null }): string {
  let s: Record<string, unknown> = {}
  try { s = JSON.parse(settings || '{}') } catch { s = {} }
  const tls = (s.tls as Record<string, unknown>) ?? {}
  const reality = (tls.reality as Record<string, unknown>) ?? {}
  if (patch.server_name !== undefined) tls.server_name = patch.server_name
  if (patch.handshake_server !== undefined) reality.handshake_server = patch.handshake_server
  if (patch.handshake_port !== undefined) reality.handshake_port = patch.handshake_port
  if (patch.fallback_limit !== undefined) { if (patch.fallback_limit === null) delete reality.fallback_limit; else reality.fallback_limit = patch.fallback_limit }
  tls.reality = reality; s.tls = tls
  return JSON.stringify(s, null, 2)
}
// scanViaNode queues a reality_scan job on the node and polls until it answers.
async function scanViaNode(nodeID: number, hosts: string[]): Promise<RealityResult[]> {
  const { id } = await api.post<{ id: string }>(`/api/admin/nodes/${nodeID}/jobs`, { kind: 'reality_scan', params: { hosts } })
  const started = Date.now()
  while (Date.now() - started < 150_000) {
    await new Promise((r) => setTimeout(r, 2000))
    const j = await api.get<NodeJob>(`/api/admin/nodes/${nodeID}/jobs/${id}`)
    if (j.done_at) { if (j.error) throw new Error(j.error); return (j.result as RealityResult[]) ?? [] }
  }
  throw new Error('node did not answer in time')
}

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'anytls', 'mieru', 'snell', 'socks', 'http', 'naive']
const cores = ['', 'singbox', 'xray', 'mita', 'hysteria', 'snell']

// Snell's PSK: 32 random bytes, base64 (any string works, this is the convention).
function randomPSK(): string { const b = new Uint8Array(32); crypto.getRandomValues(b); return btoa(String.fromCharCode(...b)) }

// Recipes fill the protocol-specific settings; values follow bosun's
// spec.Inbound JSON so bosun renders them without translation.
const recipes: { key: string; protocol: string; port: number; settings: Record<string, unknown> }[] = [
  { key: 'vlessReality', protocol: 'vless', port: 443, settings: { flow: 'xtls-rprx-vision', tls: { mode: 2, server_name: 'www.apple.com', reality: { private_key: '', public_key: '', short_ids: ['0123abcd'], handshake_server: 'www.apple.com', handshake_port: 443 } } } },
  { key: 'hysteria2', protocol: 'hysteria2', port: 8443, settings: { tls: { mode: 1, server_name: 'node.example.com', auto_cert: true, acme: 'http' }, obfs: 'salamander', obfs_password: 'change-me', up_mbps: 100, down_mbps: 500 } },
  { key: 'mieru', protocol: 'mieru', port: 24450, settings: { mieru_transport: 'TCP' } },
  { key: 'snell', protocol: 'snell', port: 6160, settings: { snell_psk: '', snell_version: 5 } },
  { key: 'ss2022', protocol: 'shadowsocks', port: 8388, settings: { cipher: '2022-blake3-aes-128-gcm', server_key: '' } },
  { key: 'trojanWs', protocol: 'trojan', port: 443, settings: { tls: { mode: 1, server_name: 'node.example.com', auto_cert: true, acme: 'http' }, transport: { type: 'ws', path: '/trojan', host: 'node.example.com' } } },
]

// mieru strategies, mirroring nobrand-oneclick's presets: all TCP, the
// difference is mita's trafficPattern (off / conservative / aggressive).
const mieruPatterns: Record<string, unknown> = {
  iplc: undefined,
  balanced: { seed: 0, unlockAll: false, nonce: { type: 'NONCE_TYPE_PRINTABLE', applyToAllUDPPacket: true, minLen: 4, maxLen: 8 }, padding: { maxMiddlePaddingLen: 0, maxEndPaddingLen: 128 } },
  stealth: { seed: 0, unlockAll: false, tcpFragment: { enable: true, maxSleepMs: 8 }, nonce: { type: 'NONCE_TYPE_PRINTABLE', applyToAllUDPPacket: true, minLen: 6, maxLen: 12 }, padding: { maxMiddlePaddingLen: 64, maxEndPaddingLen: 255 } },
}
function mieruStrategyOf(settings: string): string {
  try {
    const s = JSON.parse(settings || '{}')
    const tp = typeof s.traffic_pattern === 'string' ? JSON.parse(s.traffic_pattern) : s.traffic_pattern
    if (!tp) return 'iplc'
    for (const k of ['balanced', 'stealth']) if (JSON.stringify(tp) === JSON.stringify(mieruPatterns[k])) return k
    return 'custom'
  } catch { return 'custom' }
}
// patchSettings sets or deletes top-level keys of the settings JSON (blank/0 = delete).
function patchSettings(settings: string, patch: Record<string, unknown>): string {
  let s: Record<string, unknown> = {}
  try { s = JSON.parse(settings || '{}') } catch { s = {} }
  for (const [k, v] of Object.entries(patch)) { if (v === '' || v === 0 || v === undefined || v === null) delete s[k]; else s[k] = v }
  return JSON.stringify(s, null, 2)
}
function settingOf(settings: string, key: string): unknown { try { return JSON.parse(settings || '{}')[key] } catch { return undefined } }
function withMieru(settings: string, patch: { strategy?: string; transport?: string }): string {
  let s: Record<string, unknown> = {}
  try { s = JSON.parse(settings || '{}') } catch { s = {} }
  if (patch.transport) s.mieru_transport = patch.transport
  if (patch.strategy && patch.strategy !== 'custom') {
    const tp = mieruPatterns[patch.strategy]
    if (tp) s.traffic_pattern = JSON.stringify(tp); else delete s.traffic_pattern
  }
  return JSON.stringify(s, null, 2)
}

export type InboundValues = { Tag: string; Protocol: string; Listen: string; Port: number; Core: string; GroupID: string; Enabled: boolean; Settings: string; IngressID: string; NewIngress?: IngressValues }

export function toValues(ib?: Inbound): InboundValues {
  return ib
    ? { Tag: ib.Tag, Protocol: ib.Protocol, Listen: ib.Listen, Port: ib.Port, Core: ib.Core, GroupID: ib.GroupID ? String(ib.GroupID) : '', Enabled: ib.Enabled, Settings: JSON.stringify(stripIdentity(ib.Settings), null, 2), IngressID: ib.IngressID ? String(ib.IngressID) : '' }
    : { Tag: '', Protocol: 'vless', Listen: '', Port: 443, Core: '', GroupID: '', Enabled: true, Settings: '{}', IngressID: '' }
}

function stripIdentity(s: Record<string, unknown>) {
  const { tag: _t, protocol: _p, listen: _l, port: _po, core: _c, ...rest } = s as Record<string, unknown>
  return rest
}

export function toPayload(v: InboundValues) {
  const settings = JSON.parse(v.Settings || '{}')
  return { Tag: v.Tag, Protocol: v.Protocol, Listen: v.Listen, Port: v.Port, Core: v.Core, GroupID: v.GroupID ? Number(v.GroupID) : null, Enabled: v.Enabled, Settings: settings, IngressID: v.IngressID ? Number(v.IngressID) : null }
}

export function InboundForm({ initial, groups, onSubmit, busy, onCancel, domain, ingresses = [], usedPorts = [], lineOnly, nodeID }: { initial: InboundValues; groups: UGroup[]; onSubmit: (v: InboundValues) => void; busy: boolean; onCancel: () => void; domain?: string; ingresses?: Ingress[]; usedPorts?: number[]; lineOnly?: boolean; nodeID?: number }) {
  const { t } = useTranslation()
  const form = useForm<InboundValues>({
    initialValues: initial,
    validate: { Tag: (v) => (v ? null : 'required'), Port: (v) => (v > 0 && v < 65536 ? null : 'port'), Settings: (v) => { try { JSON.parse(v || '{}'); return null } catch { return 'invalid JSON' } } },
  })
  const [recipe, setRecipe] = useState<string | null>(null) // highlighted quick-setup card
  // Recipes name node.example.com; a node with a registered host name gets it instead.
  const firstFree = (g?: { port_from: number; port_to: number; reserved_ports?: number[] }) => { if (!g || !g.port_from) return 0; for (let p = g.port_from; p <= g.port_to; p++) if (!usedPorts.includes(p) && !(g.reserved_ports ?? []).includes(p)) return p; return 0 }
  const selectedIngress = ingresses.find((g) => String(g.id) === form.values.IngressID)
  const apply = (r: (typeof recipes)[number]) => {
    // A recipe keeps the chosen line ingress and takes a port from its range; any protocol may ride a line.
    const port = selectedIngress && selectedIngress.port_from ? (firstFree(selectedIngress) || r.port) : r.port
    const settings = r.key === 'snell' ? { ...r.settings, snell_psk: randomPSK() } : r.settings
    form.setValues({ Protocol: r.protocol, Port: port, Settings: JSON.stringify(settings, null, 2).replaceAll('node.example.com', domain || 'node.example.com'), Tag: form.values.Tag || r.protocol })
  }
  // A node reachable only through a line (no public address, no domain) defaults new inbounds to its first ingress.
  useEffect(() => { if (lineOnly && !initial.IngressID && !initial.Tag && ingresses[0]) form.setValues({ IngressID: String(ingresses[0].id), Port: firstFree(ingresses[0]) || form.values.Port }) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  const ingressForm = useForm<IngressValues>({ initialValues: form.values.NewIngress ?? emptyIngress })
  useEffect(() => { if (form.values.NewIngress) ingressForm.setValues(form.values.NewIngress) }, [form.values.NewIngress]) // eslint-disable-line react-hooks/exhaustive-deps
  const onIngress = (v: string | null) => {
    if (v === 'new') { form.setValues({ IngressID: '', NewIngress: { ...emptyIngress } }); return }
    const g = ingresses.find((x) => String(x.id) === v)
    form.setValues({ IngressID: v ?? '', NewIngress: undefined, Port: g && !g.port_from ? form.values.Port : (g ? (firstFree(g) || form.values.Port) : form.values.Port) })
  }
  const submit = (v: InboundValues) => onSubmit(v.NewIngress ? { ...v, NewIngress: ingressForm.values } : v)
  return (
    <form onSubmit={form.onSubmit(submit)}>
      <Stack>
        <div>
          <Text size="sm" fw={600}>{t('inbounds.recipe')}</Text>
          <Text size="xs" c="dimmed" mb="xs">{t('inbounds.recipeHint')}</Text>
          <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="xs">
            {recipes.map((r) => (
              <UnstyledButton key={r.key} onClick={() => { setRecipe(r.key); apply(r) }} aria-pressed={recipe === r.key}>
                <Card p="sm" withBorder style={{ height: '100%', borderColor: recipe === r.key ? 'var(--mantine-primary-color-filled)' : undefined, background: recipe === r.key ? 'var(--mantine-primary-color-light)' : undefined }}>
                  <Text size="sm" fw={600} c={recipe === r.key ? 'var(--mantine-primary-color-light-color)' : undefined}>{t(`inbounds.recipes.${r.key}`)}</Text>
                  <Text size="xs" c="dimmed">{t(`inbounds.recipes.${r.key}Desc`)}</Text>
                </Card>
              </UnstyledButton>
            ))}
          </SimpleGrid>
        </div>
        <Group grow>
          <TextInput label={t('inbounds.tag')} required {...form.getInputProps('Tag')} />
          <Select label={t('inbounds.protocol')} data={protocols} required allowDeselect={false} {...form.getInputProps('Protocol')} />
        </Group>
        <Select label={t('inbounds.ingress')} description={form.values.NewIngress ? t('inbounds.ingressNewHint') : selectedIngress ? t('inbounds.ingressHint', { host: selectedIngress.entry_host || t('ingress.noEntry'), ports: selectedIngress.port_from ? `${selectedIngress.port_from}–${selectedIngress.port_to}` : t('ingress.anyPort') }) : t('inbounds.ingressDirectHint')} allowDeselect={false}
          data={[{ value: '', label: t('inbounds.ingressDirect') }, ...ingresses.map((g) => ({ value: String(g.id), label: `${g.name} → ${g.entry_domain || g.entry_host || t('ingress.noEntry')}` })), { value: 'new', label: t('inbounds.ingressNew') }]}
          value={form.values.NewIngress ? 'new' : form.values.IngressID} onChange={onIngress} />
        {lineOnly && !form.values.IngressID && !form.values.NewIngress && <Text size="xs" c="orange">{t('inbounds.lineOnlyHint')}</Text>}
        {form.values.NewIngress && <Stack gap="xs" p="sm" style={{ border: '1px dashed var(--mantine-color-default-border)', borderRadius: 8 }}><Text size="xs" c="dimmed">{t('inbounds.ingressNewFields')}</Text><IngressFields form={ingressForm} /></Stack>}
        <Group grow>
          <TextInput label={t('inbounds.listen')} placeholder={selectedIngress?.bind_ip || '::'} description={selectedIngress?.bind_ip ? t('inbounds.listenIngressHint', { ip: selectedIngress.bind_ip }) : undefined} {...form.getInputProps('Listen')} />
          <NumberInput label={t('inbounds.port')} min={1} max={65535} required {...form.getInputProps('Port')} />
          <Select label={t('inbounds.core')} data={cores.map((c) => ({ value: c, label: c || t('inbounds.coreAuto') }))} allowDeselect={false} {...form.getInputProps('Core')} />
        </Group>
        <Group grow align="flex-end">
          <Select label={t('inbounds.group')} data={[{ value: '', label: t('inbounds.groupAll') }, ...groups.map((g) => ({ value: String(g.ID), label: g.Name }))]} allowDeselect={false} {...form.getInputProps('GroupID')} />
          {form.values.Protocol === 'mieru' && (
            <Group grow>
              <Select label={t('inbounds.mieruStrategy')} description={t('inbounds.mieruStrategyHint')} allowDeselect={false}
                data={[{ value: 'iplc', label: t('inbounds.mieru.iplc') }, { value: 'balanced', label: t('inbounds.mieru.balanced') }, { value: 'stealth', label: t('inbounds.mieru.stealth') }, { value: 'custom', label: t('inbounds.mieru.custom') }]}
                value={mieruStrategyOf(form.values.Settings)} onChange={(v) => v && form.setFieldValue('Settings', withMieru(form.values.Settings, { strategy: v }))} />
              <Select label={t('inbounds.mieruTransport')} description={t('inbounds.mieruBothHint')} data={['TCP', 'UDP', 'BOTH']} allowDeselect={false}
                value={(() => { try { return String(JSON.parse(form.values.Settings || '{}').mieru_transport || 'TCP').toUpperCase() } catch { return 'TCP' } })()} onChange={(v) => v && form.setFieldValue('Settings', withMieru(form.values.Settings, { transport: v }))} />
            </Group>
          )}
          <Switch label={t('inbounds.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
        </Group>
        {form.values.Protocol === 'mieru' && (
          <Group grow align="flex-end">
            <NumberInput label={t('inbounds.mieruMTU')} description={t('inbounds.mieruMTUHint')} min={1280} max={1500} placeholder="1400" value={(settingOf(form.values.Settings, 'mieru_mtu') as number | undefined) || ''} onChange={(v) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { mieru_mtu: Number(v) || 0 }))} />
            <Select label={t('inbounds.mieruMux')} data={[{ value: '', label: t('inbounds.clientDefault') }, { value: 'MULTIPLEXING_OFF', label: 'off' }, { value: 'MULTIPLEXING_LOW', label: 'low' }, { value: 'MULTIPLEXING_MIDDLE', label: 'middle' }, { value: 'MULTIPLEXING_HIGH', label: 'high' }]} allowDeselect={false} value={String(settingOf(form.values.Settings, 'mieru_multiplexing') ?? '')} onChange={(v) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { mieru_multiplexing: v ?? '' }))} />
            <Select label={t('inbounds.mieruHandshake')} data={[{ value: '', label: t('inbounds.clientDefault') }, { value: 'HANDSHAKE_NO_WAIT', label: 'no-wait (0-RTT)' }, { value: 'HANDSHAKE_STANDARD', label: 'standard' }]} allowDeselect={false} value={String(settingOf(form.values.Settings, 'mieru_handshake') ?? '')} onChange={(v) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { mieru_handshake: v ?? '' }))} />
          </Group>
        )}
        {form.values.Protocol === 'snell' && (
          <Stack gap="xs">
            <Group grow align="flex-end">
              <TextInput label={t('inbounds.snellPSK')} required value={String(settingOf(form.values.Settings, 'snell_psk') ?? '')} onChange={(e) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { snell_psk: e.currentTarget.value }))} rightSection={<Button size="compact-xs" variant="subtle" onClick={() => form.setFieldValue('Settings', patchSettings(form.values.Settings, { snell_psk: randomPSK() }))}>{t('inbounds.generate')}</Button>} rightSectionWidth={70} />
              <Select label={t('inbounds.snellVersion')} data={[{ value: '5', label: 'v5' }, { value: '4', label: 'v4' }]} allowDeselect={false} value={String(settingOf(form.values.Settings, 'snell_version') || 5)} onChange={(v) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { snell_version: Number(v) || 5 }))} />
              <Select label={t('inbounds.snellObfs')} data={[{ value: '', label: 'off' }, { value: 'http', label: 'http' }, { value: 'tls', label: 'tls' }]} allowDeselect={false} value={String(settingOf(form.values.Settings, 'snell_obfs') ?? '')} onChange={(v) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { snell_obfs: v ?? '', ...(v ? {} : { snell_obfs_host: '' }) }))} />
              {!!settingOf(form.values.Settings, 'snell_obfs') && <TextInput label={t('inbounds.snellObfsHost')} placeholder="www.bing.com" value={String(settingOf(form.values.Settings, 'snell_obfs_host') ?? '')} onChange={(e) => form.setFieldValue('Settings', patchSettings(form.values.Settings, { snell_obfs_host: e.currentTarget.value }))} />}
            </Group>
            <Text size="xs" c="orange">{t('inbounds.snellHint')}</Text>
          </Stack>
        )}
        {(() => {
          const tls = tlsOf(form.values.Settings)
          if (tls?.mode !== 2) return null
          const fl = tls.reality?.fallback_limit
          const current = tls.reality?.handshake_server || tls.server_name || ''
          return (
            <Card p="sm">
              <Text size="sm" fw={600} mb={4}>{t('inbounds.realityTarget')}</Text>
              <Text size="xs" c="dimmed" mb="xs">{t('inbounds.realityTargetHint', { host: current || '—' })}</Text>
              {nodeID ? <RealityScan current={current} scan={(hosts) => scanViaNode(nodeID, hosts)} onPick={(host) => form.setFieldValue('Settings', patchReality(form.values.Settings, { server_name: host, handshake_server: host, handshake_port: 443 }))} /> : <Text size="xs" c="dimmed">{t('inbounds.realityNeedsNode')}</Text>}
              <Group grow align="flex-end" mt="sm">
                <Switch label={t('inbounds.fallbackLimit')} description={t('inbounds.fallbackLimitHint')} checked={!fl?.off} onChange={(e) => form.setFieldValue('Settings', patchReality(form.values.Settings, { fallback_limit: e.currentTarget.checked ? null : { off: true } }))} />
                <NumberInput label={t('inbounds.fallbackAfter')} min={0} disabled={!!fl?.off} value={Math.round((fl?.after_bytes || 1048576) / 1048576)} onChange={(v) => form.setFieldValue('Settings', patchReality(form.values.Settings, { fallback_limit: { after_bytes: Math.max(0, Number(v) || 0) * 1048576 || undefined, bytes_per_sec: fl?.bytes_per_sec } }))} />
                <NumberInput label={t('inbounds.fallbackRate')} min={1} disabled={!!fl?.off} value={Math.round((fl?.bytes_per_sec || 65536) / 1024)} onChange={(v) => form.setFieldValue('Settings', patchReality(form.values.Settings, { fallback_limit: { after_bytes: fl?.after_bytes, bytes_per_sec: Math.max(1, Number(v) || 64) * 1024 } }))} />
              </Group>
            </Card>
          )
        })()}
        <JsonInput label={t('inbounds.settings')} description={t('inbounds.settingsHint')} autosize minRows={4} maxRows={16} formatOnBlur {...form.getInputProps('Settings')} />
        <Group justify="flex-end"><Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button><Button type="submit" loading={busy}>{t('common.save')}</Button></Group>
      </Stack>
    </form>
  )
}
