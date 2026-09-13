import { Button, Card, Group, JsonInput, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput, UnstyledButton } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useTranslation } from 'react-i18next'
import type { Group as UGroup, Inbound } from '../lib/api'

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'anytls', 'mieru', 'socks', 'http', 'naive']
const cores = ['', 'singbox', 'xray', 'mita', 'hysteria']

// Recipes fill the protocol-specific settings; values follow bosun's
// spec.Inbound JSON so bosun renders them without translation.
const recipes: { key: string; protocol: string; port: number; settings: Record<string, unknown> }[] = [
  { key: 'vlessReality', protocol: 'vless', port: 443, settings: { flow: 'xtls-rprx-vision', tls: { mode: 2, server_name: 'www.apple.com', reality: { private_key: '', public_key: '', short_ids: ['0123abcd'], handshake_server: 'www.apple.com', handshake_port: 443 } } } },
  { key: 'hysteria2', protocol: 'hysteria2', port: 8443, settings: { tls: { mode: 1, server_name: 'node.example.com', auto_cert: true, acme: 'http' }, obfs: 'salamander', obfs_password: 'change-me', up_mbps: 100, down_mbps: 500 } },
  { key: 'mieru', protocol: 'mieru', port: 24450, settings: { mieru_transport: 'TCP' } },
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

export type InboundValues = { Tag: string; Protocol: string; Listen: string; Port: number; Core: string; GroupID: string; Enabled: boolean; Settings: string }

export function toValues(ib?: Inbound): InboundValues {
  return ib
    ? { Tag: ib.Tag, Protocol: ib.Protocol, Listen: ib.Listen, Port: ib.Port, Core: ib.Core, GroupID: ib.GroupID ? String(ib.GroupID) : '', Enabled: ib.Enabled, Settings: JSON.stringify(stripIdentity(ib.Settings), null, 2) }
    : { Tag: '', Protocol: 'vless', Listen: '', Port: 443, Core: '', GroupID: '', Enabled: true, Settings: '{}' }
}

function stripIdentity(s: Record<string, unknown>) {
  const { tag: _t, protocol: _p, listen: _l, port: _po, core: _c, ...rest } = s as Record<string, unknown>
  return rest
}

export function toPayload(v: InboundValues) {
  const settings = JSON.parse(v.Settings || '{}')
  return { Tag: v.Tag, Protocol: v.Protocol, Listen: v.Listen, Port: v.Port, Core: v.Core, GroupID: v.GroupID ? Number(v.GroupID) : null, Enabled: v.Enabled, Settings: settings }
}

export function InboundForm({ initial, groups, onSubmit, busy, onCancel, domain }: { initial: InboundValues; groups: UGroup[]; onSubmit: (v: InboundValues) => void; busy: boolean; onCancel: () => void; domain?: string }) {
  const { t } = useTranslation()
  const form = useForm<InboundValues>({
    initialValues: initial,
    validate: { Tag: (v) => (v ? null : 'required'), Port: (v) => (v > 0 && v < 65536 ? null : 'port'), Settings: (v) => { try { JSON.parse(v || '{}'); return null } catch { return 'invalid JSON' } } },
  })
  // Recipes name node.example.com; a node with a registered host name gets it instead.
  const apply = (r: (typeof recipes)[number]) => form.setValues({ Protocol: r.protocol, Port: r.port, Settings: JSON.stringify(r.settings, null, 2).replaceAll('node.example.com', domain || 'node.example.com'), Tag: form.values.Tag || r.protocol })
  return (
    <form onSubmit={form.onSubmit(onSubmit)}>
      <Stack>
        <div>
          <Text size="sm" fw={600}>{t('inbounds.recipe')}</Text>
          <Text size="xs" c="dimmed" mb="xs">{t('inbounds.recipeHint')}</Text>
          <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="xs">
            {recipes.map((r) => (
              <UnstyledButton key={r.key} onClick={() => apply(r)}>
                <Card p="sm" style={{ height: '100%' }}>
                  <Text size="sm" fw={600}>{t(`inbounds.recipes.${r.key}`)}</Text>
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
        <Group grow>
          <TextInput label={t('inbounds.listen')} placeholder="::" {...form.getInputProps('Listen')} />
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
              <Select label={t('inbounds.mieruTransport')} data={['TCP', 'UDP']} allowDeselect={false}
                value={(() => { try { return String(JSON.parse(form.values.Settings || '{}').mieru_transport || 'TCP').toUpperCase() } catch { return 'TCP' } })()} onChange={(v) => v && form.setFieldValue('Settings', withMieru(form.values.Settings, { transport: v }))} />
            </Group>
          )}
          <Switch label={t('inbounds.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
        </Group>
        <JsonInput label={t('inbounds.settings')} description={t('inbounds.settingsHint')} autosize minRows={4} maxRows={16} formatOnBlur {...form.getInputProps('Settings')} />
        <Group justify="flex-end"><Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button><Button type="submit" loading={busy}>{t('common.save')}</Button></Group>
      </Stack>
    </form>
  )
}
