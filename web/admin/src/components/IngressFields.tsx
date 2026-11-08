import { ActionIcon, Button, Group, NumberInput, Select, SimpleGrid, Stack, Switch, Text, TextInput, Tooltip } from '@mantine/core'
import type { useForm } from '@mantine/form'
import { IconTrash } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import type { Ingress, IngressInput, PortMapping, Node, IngressAddressSource } from '../lib/api'

import { useEffect, useState } from 'react'
import { ManagedDomainFields } from './ManagedDomainFields'
import { entryHostFamily } from '../lib/entry-ip-policy'
import { type DomainDraft } from '../lib/node-domain'

export type IngressValues = { EntryAddressSource: IngressAddressSource; DomainDraft?: DomainDraft; Name: string; Kind: string; BindIP: string; LineIP: string; EntryHost: string; EntryDomain: string; PortFrom: number | string; PortTo: number | string; PortOffset: number | string; ReservedPorts: string; PortMode: string; PortMappings: PortMapping[]; RequireIngress: boolean }
export const parsePorts = (s: string) => s.split(/[\s,]+/).filter(Boolean).map(Number)
export const emptyIngress: IngressValues = { EntryAddressSource: 'custom', Name: '', Kind: 'nat', BindIP: '', LineIP: '', EntryHost: '', EntryDomain: '', PortFrom: '', PortTo: '', PortOffset: 0, ReservedPorts: '', PortMode: 'range', PortMappings: [], RequireIngress: true }
export const ingressPayload = (v: IngressValues): IngressInput => ({ EntryAddressSource: v.Kind === 'nat' ? v.EntryAddressSource : 'custom', Name: v.Name, Kind: v.Kind, BindIP: v.BindIP, LineIP: v.LineIP, EntryHost: v.EntryHost, EntryDomain: v.EntryDomain, PortFrom: v.PortMode === 'range' ? Number(v.PortFrom) || 0 : 0, PortTo: v.PortMode === 'range' ? Number(v.PortTo) || 0 : 0, PortOffset: v.PortMode === 'range' ? Number(v.PortOffset) || 0 : 0, ReservedPorts: parsePorts(v.ReservedPorts), PortMappings: v.PortMode === 'mapped' ? v.PortMappings : [], RequireIngress: v.RequireIngress })
export const ingressValues = (g: Ingress): IngressValues => ({ EntryAddressSource: g.entry_address_source || 'custom', Name: g.name, Kind: g.kind === 'nat' ? 'nat' : g.kind === 'iplc' ? 'iplc' : 'mapped', BindIP: g.bind_ip, LineIP: g.line_ip, EntryHost: g.entry_host, EntryDomain: g.entry_domain ?? '', PortFrom: g.port_from || '', PortTo: g.port_to || '', PortOffset: g.port_offset, ReservedPorts: (g.reserved_ports ?? []).join(', '), PortMode: g.port_mappings?.length ? 'mapped' : 'range', PortMappings: (g.port_mappings ?? []).map(m => ({ ...m })), RequireIngress: !!g.require_ingress })
export const clientHost = (g: Ingress) => g.entry_domain || g.entry_host

export const derivedPool = (bindIP: string) => {
  const m = /^(\d+)\.(\d+)\.(\d+)\.(\d+)$/.exec(bindIP.trim())
  if (!m || m.slice(1).some(x => Number(x) > 255)) return null
  const n = Number(m[4]), base = n * 100
  if (n < 1 || n > 254 || base + 99 > 65535 || base < 1024) return null
  return { reserved: base, from: base + 1, to: base + 99 }
}

export function IngressFields({ form, node, active = true, onBlockedChange }: { form: ReturnType<typeof useForm<IngressValues>>; node?: Pick<Node, 'public_addr' | 'v6_addr'>; active?: boolean; onBlockedChange: (blocked: boolean) => void }) {
  const { t } = useTranslation()
  const source = form.values.Kind === 'nat' ? form.values.EntryAddressSource : 'custom'
  const address = source === 'server_ipv4' ? node?.public_addr : source === 'server_ipv6' ? node?.v6_addr : form.values.EntryHost
  const missingAddress = source !== 'custom' && entryHostFamily(address || '') !== (source === 'server_ipv4' ? 'ipv4' : 'ipv6')
  const [domainBlocked, setDomainBlocked] = useState(false)
  useEffect(() => { onBlockedChange(missingAddress || domainBlocked) }, [missingAddress, domainBlocked, onBlockedChange])
  const order = ['label', 'input', 'description', 'error'] as const
  return <>
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
      <TextInput label={t('ingress.name')} required {...form.getInputProps('Name')} />
      <Select label={t('ingress.kind')} allowDeselect={false} data={[{ value: 'nat', label: t('ingress.kindNat') }, { value: 'iplc', label: t('ingress.kindIplc') }, ...(form.values.Kind === 'mapped' ? [{ value: 'mapped', label: t('ingress.kindMapped') }] : [])]} value={form.values.Kind} onChange={v => form.setValues({ Kind: v || 'nat', RequireIngress: v === 'nat', ...(v !== 'nat' ? { EntryAddressSource: 'custom' } : {}) })} />
      <TextInput label={t('ingress.bindIP')} inputWrapperOrder={[...order]} description={t('ingress.bindIPHint')} placeholder="10.10.0.2" {...form.getInputProps('BindIP')} />
      <TextInput label={t('ingress.lineIP')} inputWrapperOrder={[...order]} description={t('ingress.lineIPHint')} placeholder="198.51.100.20" {...form.getInputProps('LineIP')} />
      <Stack gap="xs">
        {form.values.Kind === 'nat' && <Select label={t('ingress.addressSource')} allowDeselect={false} data={[{ value: 'custom', label: t('ingress.addressCustom') }, { value: 'server_ipv4', label: t('ingress.addressIPv4') }, { value: 'server_ipv6', label: t('ingress.addressIPv6') }]} value={source} onChange={value => form.setFieldValue('EntryAddressSource', (value || 'custom') as IngressAddressSource)} />}
        {source === 'custom' ? <TextInput label={t('ingress.entryHost')} inputWrapperOrder={[...order]} description={t('ingress.entryHostHint')} required={form.values.Kind === 'nat'} placeholder="203.0.113.30" {...form.getInputProps('EntryHost')} /> : <TextInput label={t('ingress.currentAddress')} readOnly value={address || ''} error={missingAddress ? t('ingress.addressMissing') : undefined} description={t('ingress.addressFollowHint')} />}
      </Stack>
    </SimpleGrid>
    <ManagedDomainFields domain={form.values.EntryDomain} active={active} label={t('ingress.entryDomain')} hint={t('ingress.entryDomainHint')}
      draft={form.values.DomainDraft} onDraftChange={value => form.setFieldValue('DomainDraft', value)}
      onDomainChange={value => form.setFieldValue('EntryDomain', value)} onIncompleteChange={setDomainBlocked}
      suggestionEndpoint="/api/admin/ingresses/domain-suggestion" />
    <Select label={t('ingress.portMode')} allowDeselect={false} data={[{ value: 'range', label: t('ingress.modeRange') }, { value: 'mapped', label: t('ingress.modeMapped') }]} value={form.values.PortMode} onChange={v => form.setValues({ PortMode: v || 'range', ...(v === 'mapped' && !form.values.PortMappings.length ? { PortMappings: [{ local_from: 20001, local_to: 20001, public_from: 30001 }] } : {}) })} />
    {form.values.PortMode === 'range' ? <SimpleGrid cols={{ base: 1, sm: 3 }}>
      <NumberInput label={t('ingress.portFrom')} min={1} max={65535} allowDecimal={false} placeholder="20001" {...form.getInputProps('PortFrom')} />
      <NumberInput label={t('ingress.portTo')} min={1} max={65535} allowDecimal={false} placeholder="20099" {...form.getInputProps('PortTo')} />
      <NumberInput label={t('ingress.portOffset')} inputWrapperOrder={[...order]} description={t('ingress.portOffsetHint')} allowDecimal={false} {...form.getInputProps('PortOffset')} />
    </SimpleGrid> : <Stack gap="xs">
      <Text size="xs" c="dimmed">{t('ingress.mappingsHint')}</Text>
      {form.values.PortMappings.map((m, i) => <Stack key={i} gap={4} p="xs" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 8 }}>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <NumberInput label={t('ingress.localFrom')} min={1} max={65535} allowDecimal={false} required {...form.getInputProps(`PortMappings.${i}.local_from`)} />
          <NumberInput label={t('ingress.localTo')} min={1} max={65535} allowDecimal={false} required {...form.getInputProps(`PortMappings.${i}.local_to`)} />
          <NumberInput label={t('ingress.publicFrom')} min={1} max={65535} allowDecimal={false} required {...form.getInputProps(`PortMappings.${i}.public_from`)} />
        </SimpleGrid>
        <Group justify="space-between"><Text size="xs" c="dimmed">{t('ingress.publicRange', { from: m.public_from, to: Number(m.public_from) + Number(m.local_to) - Number(m.local_from) })}</Text><ActionIcon variant="subtle" color="red" aria-label={t('common.delete')} disabled={form.values.PortMappings.length === 1} onClick={() => form.removeListItem('PortMappings', i)}><IconTrash size={16} /></ActionIcon></Group>
      </Stack>)}
      <Button size="xs" variant="light" disabled={form.values.PortMappings.length >= 128} onClick={() => form.insertListItem('PortMappings', { local_from: 20001, local_to: 20001, public_from: 30001 })}>{t('ingress.addMapping')}</Button>
    </Stack>}
    <TextInput label={t('ingress.reserved')} inputWrapperOrder={[...order]} description={t('ingress.reservedHint')} placeholder="20000" {...form.getInputProps('ReservedPorts')} />
    {form.values.Kind !== 'nat' && form.values.PortMode === 'range' && <Group><Tooltip label={t('ingress.deriveHint')}><Button size="xs" variant="light" disabled={!derivedPool(form.values.BindIP)} onClick={() => { const d = derivedPool(form.values.BindIP); if (d) form.setValues({ PortFrom: d.from, PortTo: d.to, ReservedPorts: String(d.reserved) }) }}>{t('ingress.derive')}</Button></Tooltip></Group>}
    <Switch label={t('ingress.requireIngress')} description={t('ingress.requireIngressHint')} {...form.getInputProps('RequireIngress', { type: 'checkbox' })} />
    <Text size="xs" c="dimmed">{t('ingress.providerHint')}</Text>
  </>
}
