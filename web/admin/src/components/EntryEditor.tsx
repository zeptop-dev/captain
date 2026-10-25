import { Alert, Button, Code, Group, JsonInput, Modal, NumberInput, Select, SimpleGrid, Stack, Switch, TagsInput, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Entry, type EntryCategory, type Line, type Node } from '../lib/api'
import { canDialThrough, changeEntryInbound, entryEndpoint, type InboundChoice } from '../lib/entry-flow'
import { hostPort } from '../lib/ingress'
import { flag, REGIONS } from '../lib/regions'
import { useSettingsDirty } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { EntryInboundCreator } from './EntryInboundCreator'

type Mode = 'direct' | 'line' | 'client'
interface Values {
  Name: string; InboundID: string; DisplayHost: string; DisplayPort: number; Rate: number; Sort: number; Enabled: boolean; Tags: string[]; Region: string; ClientExtra: string
  CategoryID: string; mode: Mode; LineID: string; LineMemberID: string; ClientDialerEntryID: string
}

export function EntryEditor({ entry, nodes, inbounds, entries, lines, tags, categories, initialInboundID, initialLineID, initialServerID, canManageLines, loading, loadError, onReload, onClose }: {
  entry?: Entry; nodes: Node[]; inbounds: InboundChoice[]; entries: Entry[]; lines: Line[]; tags: string[]; categories: EntryCategory[]
  initialInboundID?: number; initialLineID?: number; initialServerID?: number; canManageLines: boolean; loading: boolean; loadError: boolean; onReload: () => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const initialInbound = inbounds.find(c => c.ib.ID === (entry?.InboundID || initialInboundID))
  const initialLine = lines.find(line => line.id === (entry?.LineID || initialLineID))
  const lineExtraReadOnly = !!entry?.LineID && !canManageLines
  const initial: Values = {
    Name: entry?.Name ?? initialInbound?.ib.Tag ?? initialLine?.name ?? '', InboundID: String(entry?.InboundID || initialLine?.target_inbound_id || initialInboundID || ''),
    ...entryEndpoint(initialInbound), ...(entry ? { DisplayHost: entry.DisplayHost, DisplayPort: entry.DisplayPort } : {}),
    Rate: entry?.Rate ?? 1, Sort: entry?.Sort ?? 0, Enabled: entry?.Enabled ?? true, Tags: entry?.Tags ?? [], Region: entry?.Region ?? '', ClientExtra: entry?.ClientExtra ? JSON.stringify(entry.ClientExtra, null, 2) : '',
    CategoryID: String(entry?.CategoryID || 0), mode: entry?.LineID || initialLineID ? 'line' : entry?.ClientDialerEntryID ? 'client' : 'direct', LineID: String(entry?.LineID || initialLineID || ''), LineMemberID: entry?.LineMemberID || initialLine?.endpoints.find(e => e.host.trim())?.member_id || '', ClientDialerEntryID: String(entry?.ClientDialerEntryID || ''),
  }
  const form = useForm<Values>({ initialValues: initial, validate: {
    Name: value => value.trim() ? null : t('entryFlow.required'),
    InboundID: (value, values) => value || values.mode === 'line' ? null : t('entryFlow.required'),
    DisplayHost: (value, values) => values.mode === 'line' || value.trim() ? null : t('entryFlow.required'),
    DisplayPort: (value, values) => values.mode === 'line' || Number.isInteger(value) && value > 0 && value < 65536 ? null : t('entryFlow.invalidPort'),
    LineID: (value, values) => values.mode !== 'line' || value ? null : t('entryFlow.required'),
    LineMemberID: (value, values) => values.mode !== 'line' || value ? null : t('entryFlow.required'),
    ClientDialerEntryID: (value, values) => values.mode !== 'client' || value && canDialThrough(entries, Number(value), entry?.ID) ? null : t('entryFlow.invalidDialer'),
    ClientExtra: value => { try { const parsed = JSON.parse(value || '{}'); return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? null : t('overrides.invalid') } catch { return t('overrides.invalid') } },
  } })
  const [creatingInbound, setCreatingInbound] = useState(false)
  const [createdInbound, setCreatedInbound] = useState<InboundChoice | null>(null)
  const choices = createdInbound && !inbounds.some(c => c.ib.ID === createdInbound.ib.ID) ? [...inbounds, createdInbound] : inbounds
  const selected = choices.find(c => String(c.ib.ID) === form.values.InboundID)
  const line = lines.find(l => String(l.id) === form.values.LineID)
  const endpoint = line?.endpoints.find(e => e.member_id === form.values.LineMemberID)
  const save = useMutation({
    mutationFn: (v: Values) => {
      const extra = JSON.parse(v.ClientExtra || '{}')
      const payload = { Name: v.Name.trim(), InboundID: v.mode === 'line' ? line?.target_inbound_id ?? Number(v.InboundID) : Number(v.InboundID), DisplayHost: v.mode === 'line' ? !canManageLines && entry ? entry.DisplayHost : '' : v.DisplayHost.trim(), DisplayPort: v.mode === 'line' ? !canManageLines && entry ? entry.DisplayPort : 0 : v.DisplayPort, Rate: v.Rate, Sort: v.Sort, Enabled: v.Enabled, Tags: v.Tags, Region: v.Region, CategoryID: Number(v.CategoryID), ClientExtra: lineExtraReadOnly ? entry?.ClientExtra ?? null : Object.keys(extra).length ? extra : null,
        LineID: v.mode === 'line' ? Number(v.LineID) : null, LineMemberID: v.mode === 'line' ? v.LineMemberID : '', ClientDialerEntryID: v.mode === 'client' ? Number(v.ClientDialerEntryID) : null }
      return entry ? api.patch<Entry>(`/api/admin/entries/${entry.ID}`, payload) : api.post<Entry>('/api/admin/entries', payload)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['entry-tags'] }); toast.ok(t('common.saved')); onClose() },
  })
  const dirty = form.isDirty() || !!createdInbound
  useSettingsDirty(dirty || save.isPending)
  const close = () => {
    if (save.isPending) return
    if (!dirty) { onClose(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{createdInbound ? t('entryFlow.createdResourcesRemain') : t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose })
  }
  const pickInbound = (id: string | null) => {
    const next = choices.find(c => String(c.ib.ID) === id)
    if (!next) { form.setFieldValue('InboundID', id ?? ''); return }
    form.setValues({ InboundID: String(next.ib.ID), ...changeEntryInbound(form.getValues(), selected, next), ...(!form.values.Name ? { Name: next.ib.Tag } : {}) })
  }
  const pickLine = (id: string | null) => {
    const next = lines.find(l => String(l.id) === id)
    form.setValues({ LineID: id ?? '', LineMemberID: next?.endpoints.find(e => e.host.trim())?.member_id ?? '', InboundID: String(next?.target_inbound_id ?? ''), ...(!form.values.Name && next ? { Name: next.name } : {}) })
  }
  return <>
    <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={entry ? t('entryFlow.editNode') : t('entryFlow.createNode')} size="lg">
      <form onSubmit={form.onSubmit(v => save.mutate(v))}>
        <Stack>
          {loadError && <Alert color="yellow"><Text size="sm">{t('entryFlow.partialLoad')}</Text><Button size="xs" variant="subtle" onClick={onReload}>{t('entryFlow.retry')}</Button></Alert>}
          {save.isError && <Alert color="red">{save.error.message}</Alert>}
          <Select label={t('entryFlow.connection')} allowDeselect={false} disabled={save.isPending || !!entry?.LineID && !canManageLines} data={[
            { value: 'direct', label: t('entryFlow.direct') }, { value: 'line', label: t('entryFlow.viaLine'), disabled: !canManageLines }, { value: 'client', label: t('entryFlow.clientChain') },
          ]} value={form.values.mode} onChange={value => {
            const mode = value as Mode
            if (mode !== 'line' && form.values.mode === 'line') {
              const target = choices.find(c => c.ib.ID === line?.target_inbound_id)
              form.setValues({ mode, ...entryEndpoint(target), InboundID: String(target?.ib.ID ?? '') })
            } else form.setFieldValue('mode', mode)
          }} />
          <Text size="sm" c="dimmed">{form.values.mode === 'line' ? t('entryFlow.lineHint') : form.values.mode === 'client' ? t('entryFlow.clientHint') : t('entryFlow.directHint')}</Text>
          {form.values.mode === 'line' ? <>
            {canManageLines ? <><Select label={t('entryFlow.line')} required searchable disabled={save.isPending} data={lines.map(l => ({ value: String(l.id), label: l.endpoints.some(e => e.host.trim()) ? l.name : `${l.name} · ${t('entryFlow.lineEndpointMissingLabel')}`, disabled: !l.endpoints.some(e => e.host.trim()) }))} value={form.values.LineID || null} onChange={pickLine} error={form.errors.LineID} />
            <Select label={t('entryFlow.lineEndpoint')} required disabled={save.isPending} data={(line?.endpoints ?? []).map(e => ({ value: e.member_id, label: `${nodes.find(n => n.id === e.node_id)?.name ?? `#${e.node_id}`} · ${e.host.trim() ? hostPort(e.host, e.port) : t('entryFlow.lineEndpointMissingLabel')}`, disabled: !e.host.trim() }))} value={form.values.LineMemberID || null} onChange={value => form.setFieldValue('LineMemberID', value ?? '')} error={form.errors.LineMemberID} /></> : <Text size="sm">{t('entryFlow.line')}: #{entry?.LineID}</Text>}
            {line && (!line.endpoints.length || line.endpoints.some(e => !e.host.trim())) && <Alert color="yellow">{t('entryFlow.lineEndpointMissing')}</Alert>}
            <Text size="sm">{t('entryFlow.publishedAddress')}: <Code>{endpoint?.host.trim() ? hostPort(endpoint.host, endpoint.port) : entry?.LineID ? hostPort(entry.DisplayHost, entry.DisplayPort) : '—'}</Code></Text>
            {line && <Text size="sm" c="dimmed">{t('entryFlow.targetInbound')}: {choices.find(c => c.ib.ID === line.target_inbound_id)?.ib.Tag ?? `#${line.target_inbound_id}`}</Text>}
          </> : <>
            <Group justify="space-between" align="flex-end"><Text size="sm" fw={500}>{t('entryFlow.targetInbound')}</Text><Button size="xs" variant="light" disabled={save.isPending || !!createdInbound || loading} onClick={() => setCreatingInbound(true)}>{t('entryFlow.newInbound')}</Button></Group>
            <Select aria-label={t('entryFlow.targetInbound')} required searchable disabled={save.isPending} placeholder={loading ? t('entryFlow.loading') : t('entryFlow.pickInbound')} data={choices.map(c => ({ value: String(c.ib.ID), label: `${c.node.name} / ${c.ib.Tag} (${c.ib.Protocol}:${c.ib.Port})` }))} value={form.values.InboundID || null} onChange={pickInbound} error={form.errors.InboundID} />
            {createdInbound && <Alert color="blue">{t('entryFlow.inboundSaved', { name: createdInbound.ib.Tag })}</Alert>}
            {form.values.mode === 'client' && <>
              <Select label={t('entryFlow.dialer')} required searchable data={entries.filter(e => canDialThrough(entries, e.ID, entry?.ID)).map(e => ({ value: String(e.ID), label: e.Name }))} disabled={save.isPending} {...form.getInputProps('ClientDialerEntryID')} />
              <Alert color="blue">{t('entryFlow.clientCompatibility')}</Alert>
            </>}
            <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput label={t('entryFlow.connectHost')} required disabled={save.isPending} placeholder="example.com" {...form.getInputProps('DisplayHost')} /><NumberInput label={t('entryFlow.connectPort')} required disabled={save.isPending} min={1} max={65535} {...form.getInputProps('DisplayPort')} /></SimpleGrid>
            <Text size="xs" c="dimmed">{t('entryFlow.addressHint')}</Text>
          </>}
          <TextInput label={t('entries.name')} required disabled={save.isPending} {...form.getInputProps('Name')} />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Select label={t('entries.region')} searchable clearable data={REGIONS.map(r => ({ value: r.code, label: `${flag(r.code)} ${r.code} ${r.name}` }))} disabled={save.isPending} value={form.values.Region || null} onChange={v => form.setFieldValue('Region', v ?? '')} />
            <NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} disabled={save.isPending} {...form.getInputProps('Rate')} />
          </SimpleGrid>
          <Select label={t('entryCategories.label')} description={t('entryCategories.hint')} searchable allowDeselect={false} data={[{ value: '0', label: t('entryCategories.unclassified') }, ...categories.map(category => ({ value: String(category.ID), label: category.Name }))]} disabled={save.isPending} {...form.getInputProps('CategoryID')} />
          <TagsInput label={t('entries.tags')} data={tags} disabled={save.isPending} {...form.getInputProps('Tags')} />
          <JsonInput label={t('entries.clientExtra')} description={lineExtraReadOnly ? t('entryFlow.lineExtraAdminOnly') : t('entries.clientExtraHint')} autosize minRows={2} maxRows={8} formatOnBlur validationError={t('overrides.invalid')} disabled={save.isPending || lineExtraReadOnly} {...form.getInputProps('ClientExtra')} />
          <Switch label={t('entries.enabled')} disabled={save.isPending} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={loading || form.values.mode === 'line' && canManageLines && !endpoint?.host.trim()}>{t('common.save')}</Button></Group>
        </Stack>
      </form>
    </Modal>
    {creatingInbound && <EntryInboundCreator nodes={nodes} initialServerID={initialServerID} onClose={() => setCreatingInbound(false)} onCreated={choice => {
      setCreatedInbound(choice); setCreatingInbound(false)
      form.setValues({ InboundID: String(choice.ib.ID), ...entryEndpoint(choice), ...(!form.values.Name ? { Name: choice.ib.Tag } : {}) })
    }} />}
  </>
}
