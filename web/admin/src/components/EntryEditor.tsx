import { localizedName } from '../lib/localized-name'
import { Alert, Button, Checkbox, Code, Group, JsonInput, Modal, NumberInput, Select, SimpleGrid, Stack, Switch, TagsInput, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useAuth } from '../lib/auth'
import { accessChanged, accessDraft, changedAccessPayload, sharedEntries, validAccess, type AccessDraft } from '../lib/node-access'
import { AccessFields } from './NodeAccess'
import { useTranslation } from 'react-i18next'
import { useNodeAccess } from '../lib/node-access-query'
import { api, ApiError, type Entry, type EntryCategory, type Line, type Node } from '../lib/api'
import { canFollowIngress, canDialThrough, changeEntryInbound, entryEndpoint, type InboundChoice } from '../lib/entry-flow'
import { hostPort } from '../lib/ingress'
import { flag, REGIONS } from '../lib/regions'
import { useSettingsDirty } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { canCreateIPVariants, defaultEntryIPVersion, ENTRY_IP_VERSIONS, entryHostFamily, entryIPExtraConflicts, entryIPLabelKey, entryIPPolicyError, type EntryIPVersion } from '../lib/entry-ip-policy'
import { EntryIPSupport } from './EntryIPSupport'
import { EntryInboundCreator } from './EntryInboundCreator'

type Mode = 'direct' | 'line' | 'client'
interface Values {
  FollowIngress: boolean; Name: string; InboundID: string; DisplayHost: string; DisplayPort: number; Rate: number; Sort: number; Enabled: boolean; Tags: string[]; Region: string; ClientExtra: string
  IPVersion: EntryIPVersion; CreateIPVariants: boolean; CategoryID: string; mode: Mode; LineID: string; LineMemberID: string; ClientDialerEntryID: string
}

export function EntryEditor({ entry, nodes, inbounds, entries, lines, tags, categories, initialInboundID, initialLineID, initialServerID, canManageLines, loading, loadError, onReload, onClose }: {
  entry?: Entry; nodes: Node[]; inbounds: InboundChoice[]; entries: Entry[]; lines: Line[]; tags: string[]; categories: EntryCategory[]
  initialInboundID?: number; initialLineID?: number; initialServerID?: number; canManageLines: boolean; loading: boolean; loadError: boolean; onReload: () => void; onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const { me } = useAuth()
  const admin = me?.role === 'admin'
  const access = useNodeAccess()
  const [accessEdit, setAccessEdit] = useState<AccessDraft | null>(null)
  const initialInbound = inbounds.find(c => c.ib.ID === (entry?.InboundID || initialInboundID))
  const initialLine = lines.find(line => line.id === (entry?.LineID || initialLineID))
  const lineExtraReadOnly = !!entry?.LineID && !canManageLines
  const [uncertain, setUncertain] = useState(false)
  const [policyTouched, setPolicyTouched] = useState(false)
  const initialHost = entry?.DisplayHost ?? initialLine?.endpoints.find(e => e.host.trim())?.host ?? entryEndpoint(initialInbound).DisplayHost
  const initial: Values = {
    FollowIngress: entry ? !!entry.FollowIngress : canFollowIngress(initialInbound),
    IPVersion: entry ? entry.IPVersion ?? '' : defaultEntryIPVersion(initialHost), CreateIPVariants: false,
    Name: entry?.Name ?? initialInbound?.ib.Tag ?? initialLine?.name ?? '', InboundID: String(entry?.InboundID || initialLine?.target_inbound_id || initialInboundID || ''),
    ...entryEndpoint(initialInbound), ...(entry ? { DisplayHost: entry.DisplayHost, DisplayPort: entry.DisplayPort } : {}),
    Rate: entry?.Rate ?? 1, Sort: entry?.Sort ?? 0, Enabled: entry?.Enabled ?? true, Tags: entry?.Tags ?? [], Region: entry?.Region ?? '', ClientExtra: entry?.ClientExtra ? JSON.stringify(entry.ClientExtra, null, 2) : '',
    CategoryID: String(entry?.CategoryID || 0), mode: entry?.LineID || initialLineID ? 'line' : entry?.ClientDialerEntryID ? 'client' : 'direct', LineID: String(entry?.LineID || initialLineID || ''), LineMemberID: entry?.LineMemberID || initialLine?.endpoints.find(e => e.host.trim())?.member_id || '', ClientDialerEntryID: String(entry?.ClientDialerEntryID || ''),
  }
  const form = useForm<Values>({ initialValues: initial, validate: {
    IPVersion: (value, values) => { const host = values.mode === 'line' ? lines.find(l => String(l.id) === values.LineID)?.endpoints.find(e => e.member_id === values.LineMemberID)?.host ?? entry?.DisplayHost ?? '' : values.DisplayHost; const target = values.mode === 'line' ? lines.find(l => String(l.id) === values.LineID)?.target_inbound_id : Number(values.InboundID); const error = entryIPPolicyError(value, host, values.mode === 'client', values.ClientExtra, inbounds.find(c => c.ib.ID === target)?.ib.Protocol ?? (createdInbound && createdInbound.ib.ID === target ? createdInbound.ib.Protocol : '')); return error ? t(`entryIP.errors.${error}`) : null },
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
  const followingEndpoint = entryEndpoint(selected)
  useEffect(() => {
    if (form.values.mode !== 'line' && form.values.FollowIngress && canFollowIngress(selected, entry?.ChainID) && (form.values.DisplayHost !== followingEndpoint.DisplayHost || form.values.DisplayPort !== followingEndpoint.DisplayPort)) form.setValues(followingEndpoint)
  }, [form, selected, entry?.ChainID, followingEndpoint])
  const line = lines.find(l => String(l.id) === form.values.LineID)
  const endpoint = line?.endpoints.find(e => e.member_id === form.values.LineMemberID)
  const connectionHost = form.values.mode === 'line' ? endpoint?.host ?? entry?.DisplayHost ?? '' : form.values.DisplayHost
  const targetID = form.values.mode === 'line' ? line?.target_inbound_id ?? 0 : Number(form.values.InboundID)
  const protocol = choices.find(c => c.ib.ID === targetID)?.ib.Protocol ?? ''
  const ipError = entryIPPolicyError(form.values.IPVersion, connectionHost, form.values.mode === 'client', form.values.ClientExtra, protocol)
  const variantsAvailable = canCreateIPVariants(connectionHost, form.values.mode === 'client', form.values.ClientExtra, protocol)
  const extraConflicts = entryIPExtraConflicts(form.values.ClientExtra)
  const accessRow = access.data?.inbounds.find(row => row.inbound_id === targetID)
  const accessValue = accessEdit?.source.inbound_id === targetID ? accessEdit : accessRow ? accessDraft(accessRow) : null
  const accessDirty = !!accessValue && accessChanged(accessValue)
  const accessBlocked = admin && !!targetID && (access.isPending || access.isError || !accessValue || accessDirty && (!access.data || !validAccess(accessValue, access.data) || (form.values.CreateIPVariants || !!sharedEntries(accessValue.source, entry?.ID).length) && !accessValue.confirm_shared))
  const reloadAccess = () => {
    const run = () => { setAccessEdit(null); void access.refetch() }
    if (!accessDirty) { run(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: run })
  }
  const save = useMutation({
    mutationFn: async (v: Values) => {
      if (uncertain) throw new Error(t('entryIP.uncertain'))
      if (accessBlocked) throw new Error(t('nodeAccess.reloadHint'))
      const Access = admin ? changedAccessPayload(accessValue, targetID) : undefined
      const extra = JSON.parse(v.ClientExtra || '{}')
      const payload = { FollowIngress: v.mode !== 'line' && canFollowIngress(selected, entry?.ChainID) && v.FollowIngress, ...(Access ? { Access } : {}), Name: v.Name.trim(), InboundID: v.mode === 'line' ? line?.target_inbound_id ?? Number(v.InboundID) : Number(v.InboundID), DisplayHost: v.mode === 'line' ? !canManageLines && entry ? entry.DisplayHost : '' : v.DisplayHost.trim(), DisplayPort: v.mode === 'line' ? !canManageLines && entry ? entry.DisplayPort : 0 : v.DisplayPort, Rate: v.Rate, Sort: v.Sort, Enabled: v.Enabled, Tags: v.Tags, Region: v.Region, CategoryID: Number(v.CategoryID), IPVersion: lineExtraReadOnly ? entry?.IPVersion ?? '' : v.IPVersion, ClientExtra: lineExtraReadOnly ? entry?.ClientExtra ?? null : Object.keys(extra).length ? extra : null,
        LineID: v.mode === 'line' ? Number(v.LineID) : null, LineMemberID: v.mode === 'line' ? v.LineMemberID : '', ClientDialerEntryID: v.mode === 'client' ? Number(v.ClientDialerEntryID) : null }
      if (!entry && v.CreateIPVariants) {
        if (!variantsAvailable) throw new Error(t('entryIP.variantsHint'))
        try { return await api.post<{ entries: Entry[] }>('/api/admin/entries/variants', payload) }
        catch (error) { if (!(error instanceof ApiError) || error.status >= 500) { setUncertain(true); void qc.invalidateQueries({ queryKey: ['entries'] }) }; throw error }
      }
      return entry ? api.patch<Entry>(`/api/admin/entries/${entry.ID}`, payload) : api.post<Entry>('/api/admin/entries', payload)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['entry-tags'] }); qc.invalidateQueries({ queryKey: ['node-access'] }); toast.ok(t('common.saved')); onClose() },
  })
  const busy = save.isPending || uncertain
  const newHostPolicy = (host: string) => !entry && !policyTouched ? { IPVersion: defaultEntryIPVersion(host) } : {}
  const dirty = form.isDirty() || !!createdInbound || accessDirty
  useSettingsDirty(dirty || save.isPending)
  const close = () => {
    if (save.isPending) return
    if (!dirty) { onClose(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{createdInbound ? t('entryFlow.createdResourcesRemain') : t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose })
  }
  const pickInbound = (id: string | null) => {
    if (Number(id) !== targetID) setAccessEdit(null)
    const next = choices.find(c => String(c.ib.ID) === id)
    if (!next) { form.setFieldValue('InboundID', id ?? ''); return }
    const changed = changeEntryInbound(form.getValues(), selected, next)
    form.setValues({ CreateIPVariants: false, InboundID: String(next.ib.ID), ...changed, FollowIngress: canFollowIngress(next, entry?.ChainID) && (form.values.FollowIngress || !entry && changed.DisplayHost === entryEndpoint(next).DisplayHost && changed.DisplayPort === entryEndpoint(next).DisplayPort), ...(form.values.mode !== 'client' ? newHostPolicy(changed.DisplayHost ?? form.values.DisplayHost) : {}), ...(!form.values.Name ? { Name: next.ib.Tag } : {}) })
  }
  const pickLine = (id: string | null) => {
    const next = lines.find(l => String(l.id) === id)
    if (next?.target_inbound_id !== targetID) setAccessEdit(null)
    form.setValues({ ...newHostPolicy(next?.endpoints.find(e => e.host.trim())?.host ?? ''), CreateIPVariants: false, LineID: id ?? '', LineMemberID: next?.endpoints.find(e => e.host.trim())?.member_id ?? '', InboundID: String(next?.target_inbound_id ?? ''), ...(!form.values.Name && next ? { Name: next.name } : {}) })
  }
  return <>
    <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={entry ? t('entryFlow.editNode') : t('entryFlow.createNode')} size="lg">
      <form onSubmit={form.onSubmit(v => { if (!busy && !ipError && !loading && !accessBlocked && !(v.mode === 'line' && canManageLines && !endpoint?.host.trim())) save.mutate(v) })}>
        <Stack>
          {loadError && <Alert color="yellow"><Text size="sm">{t('entryFlow.partialLoad')}</Text><Button size="xs" variant="subtle" onClick={onReload}>{t('entryFlow.retry')}</Button></Alert>}
          {save.isError && <Alert color="red">{save.error.message}</Alert>}
          {uncertain && <Alert color="yellow">{t('entryIP.uncertain')}</Alert>}
          <Select label={t('entryFlow.connection')} allowDeselect={false} disabled={busy || !!entry?.LineID && !canManageLines} data={[
            { value: 'direct', label: t('entryFlow.direct') }, { value: 'line', label: t('entryFlow.viaLine'), disabled: !canManageLines }, { value: 'client', label: t('entryFlow.clientChain') },
          ]} value={form.values.mode} onChange={value => {
            const mode = value as Mode
            form.setFieldValue('CreateIPVariants', false)
            if (mode === 'client') form.setFieldValue('IPVersion', '')
            const nextTargetID = mode === 'line' ? line?.target_inbound_id ?? 0 : form.values.mode === 'line' ? line?.target_inbound_id ?? 0 : Number(form.values.InboundID)
            if (nextTargetID !== targetID) setAccessEdit(null)
            if (mode !== 'line' && form.values.mode === 'line') {
              const target = choices.find(c => c.ib.ID === line?.target_inbound_id)
              form.setValues({ mode, FollowIngress: canFollowIngress(target, entry?.ChainID), ...entryEndpoint(target), ...(mode === 'client' ? { IPVersion: '' } : newHostPolicy(entryEndpoint(target).DisplayHost)), InboundID: String(target?.ib.ID ?? '') })
            } else form.setValues({ mode, ...(mode === 'client' ? { IPVersion: '' } : newHostPolicy(mode === 'line' ? endpoint?.host ?? '' : form.values.DisplayHost)) })
          }} />
          <Text size="sm" c="dimmed">{form.values.mode === 'line' ? t('entryFlow.lineHint') : form.values.mode === 'client' ? t('entryFlow.clientHint') : t('entryFlow.directHint')}</Text>
          {form.values.mode === 'line' ? <>
            {canManageLines ? <><Select label={t('entryFlow.line')} required searchable disabled={busy} data={lines.map(l => ({ value: String(l.id), label: l.endpoints.some(e => e.host.trim()) ? l.name : `${l.name} · ${t('entryFlow.lineEndpointMissingLabel')}`, disabled: !l.endpoints.some(e => e.host.trim()) }))} value={form.values.LineID || null} onChange={pickLine} error={form.errors.LineID} />
            <Select label={t('entryFlow.lineEndpoint')} required disabled={busy} data={(line?.endpoints ?? []).map(e => ({ value: e.member_id, label: `${nodes.find(n => n.id === e.node_id)?.name ?? `#${e.node_id}`} · ${e.host.trim() ? hostPort(e.host, e.port) : t('entryFlow.lineEndpointMissingLabel')}`, disabled: !e.host.trim() }))} value={form.values.LineMemberID || null} onChange={value => form.setValues({ LineMemberID: value ?? '', CreateIPVariants: false, ...newHostPolicy(line?.endpoints.find(e => e.member_id === value)?.host ?? '') })} error={form.errors.LineMemberID} /></> : <Text size="sm">{t('entryFlow.line')}: #{entry?.LineID}</Text>}
            {line && (!line.endpoints.length || line.endpoints.some(e => !e.host.trim())) && <Alert color="yellow">{t('entryFlow.lineEndpointMissing')}</Alert>}
            <Text size="sm">{t('entryFlow.publishedAddress')}: <Code>{endpoint?.host.trim() ? hostPort(endpoint.host, endpoint.port) : entry?.LineID ? hostPort(entry.DisplayHost, entry.DisplayPort) : '—'}</Code></Text>
            {line && <Text size="sm" c="dimmed">{t('entryFlow.targetInbound')}: {choices.find(c => c.ib.ID === line.target_inbound_id)?.ib.Tag ?? `#${line.target_inbound_id}`}</Text>}
          </> : <>
            <Group justify="space-between" align="flex-end"><Text size="sm" fw={500}>{t('entryFlow.targetInbound')}</Text><Button size="xs" variant="light" disabled={busy || !!createdInbound || loading} onClick={() => setCreatingInbound(true)}>{t('entryFlow.newInbound')}</Button></Group>
            <Select aria-label={t('entryFlow.targetInbound')} required searchable disabled={busy} placeholder={loading ? t('entryFlow.loading') : t('entryFlow.pickInbound')} data={choices.map(c => ({ value: String(c.ib.ID), label: `${c.node.name} / ${c.ib.Tag} (${c.ib.Protocol}:${c.ib.Port})` }))} value={form.values.InboundID || null} onChange={pickInbound} error={form.errors.InboundID} />
            {createdInbound && <Alert color="blue">{t('entryFlow.inboundSaved', { name: createdInbound.ib.Tag })}</Alert>}
            {form.values.mode === 'client' && <>
              <Select label={t('entryFlow.dialer')} required searchable data={entries.filter(e => canDialThrough(entries, e.ID, entry?.ID)).map(e => ({ value: String(e.ID), label: e.Name }))} disabled={busy} {...form.getInputProps('ClientDialerEntryID')} />
              <Alert color="blue">{t('entryFlow.clientCompatibility')}</Alert>
            </>}
            {canFollowIngress(selected, entry?.ChainID) && <Switch label={t('ingress.followEndpoint')} description={t('ingress.followEndpointHint')} disabled={busy} checked={form.values.FollowIngress} onChange={event => { const follow = event.currentTarget.checked; form.setValues({ FollowIngress: follow, ...(follow ? entryEndpoint(selected) : {}) }) }} />}
            <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput label={t('entryFlow.connectHost')} required disabled={busy} placeholder="example.com" {...form.getInputProps('DisplayHost')} onChange={event => form.setValues({ DisplayHost: event.currentTarget.value, FollowIngress: false, CreateIPVariants: false, ...(form.values.mode !== 'client' ? newHostPolicy(event.currentTarget.value) : {}) })} /><NumberInput label={t('entryFlow.connectPort')} required disabled={busy} min={1} max={65535} {...form.getInputProps('DisplayPort')} onChange={value => form.setValues({ DisplayPort: Number(value), FollowIngress: false })} /></SimpleGrid>
            <Text size="xs" c="dimmed">{t('entryFlow.addressHint')}</Text>
          </>}
          <Stack gap="xs">
            <Select label={t('entryIP.label')} allowDeselect={false} value={form.values.IPVersion} disabled={busy || lineExtraReadOnly || form.values.mode === 'client' || form.values.CreateIPVariants}
              data={ENTRY_IP_VERSIONS.map(value => ({ value, label: t(entryIPLabelKey(value)), disabled: !!value && (form.values.mode === 'client' || entryIPPolicyError(value, connectionHost, false, '', protocol) === 'wireguardDomain' || ['ipv4', 'ipv6'].includes(entryHostFamily(connectionHost)) && value !== entryHostFamily(connectionHost)) }))}
              onChange={value => { setPolicyTouched(true); form.setFieldValue('IPVersion', (value ?? '') as EntryIPVersion) }} error={ipError ? t(`entryIP.errors.${ipError}`) : undefined} />
            <Text size="xs" c="dimmed">{t(form.values.mode === 'client' ? 'entryIP.chainHint' : form.values.mode === 'line' ? 'entryIP.lineHint' : 'entryIP.hint')}</Text>
            {protocol === 'wireguard' && <Text size="xs" c="dimmed">{t('entryIP.wireguardHint')}</Text>}
            {!!extraConflicts.length && <Text size="xs" c="orange">{t('entryIP.extraHint', { keys: extraConflicts.join(', ') })}</Text>}
            <EntryIPSupport host={connectionHost} protocol={protocol} policy={form.values.IPVersion} variants={form.values.CreateIPVariants} disabled={!!ipError} />
            {!entry && <><Checkbox label={t('entryIP.variants')} checked={form.values.CreateIPVariants} disabled={busy || !variantsAvailable && !form.values.CreateIPVariants} onChange={event => form.setValues({ CreateIPVariants: event.currentTarget.checked, ...(event.currentTarget.checked ? { IPVersion: 'dual' } : {}) })} /><Text size="xs" c="dimmed">{t('entryIP.variantsHint')}</Text></>}
          </Stack>
          {admin && !!targetID && <Stack gap="xs">
            {(access.isError || !access.isPending && !accessRow) && <Alert color="red">{t('nodeAccess.loadError')}</Alert>}
            {access.isPending && <Text size="sm" c="dimmed">{t('entryFlow.loading')}</Text>}
            {accessValue && access.data && <AccessFields draft={accessValue} directory={access.data} entryID={entry?.ID} onChange={setAccessEdit} busy={busy || access.isError || access.isFetching} />}
            {form.values.CreateIPVariants && accessDirty && accessValue && !sharedEntries(accessValue.source, entry?.ID).length && <Checkbox label={t('nodeAccess.confirmShared')} checked={accessValue.confirm_shared} disabled={busy} onChange={event => setAccessEdit({ ...accessValue, confirm_shared: event.currentTarget.checked })} />}
            {accessDirty && accessValue?.source.revision !== access.data?.revision && <Alert color="yellow">{t('nodeAccess.reloadHint')}</Alert>}
            <Button size="compact-xs" variant="subtle" w="fit-content" disabled={busy || access.isFetching} onClick={reloadAccess}>{t('nodeAccess.reload')}</Button>
          </Stack>}
          <TextInput label={t('entries.name')} required disabled={busy} {...form.getInputProps('Name')} />
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <Select label={t('entries.region')} searchable clearable data={REGIONS.map(r => ({ value: r.code, label: `${flag(r.code)} ${r.code} ${r.name}` }))} disabled={busy} value={form.values.Region || null} onChange={v => form.setFieldValue('Region', v ?? '')} />
            <NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} disabled={busy} {...form.getInputProps('Rate')} />
          </SimpleGrid>
          <Select label={t('entryCategories.label')} description={t('entryCategories.hint')} searchable allowDeselect={false} data={[{ value: '0', label: t('entryCategories.unclassified') }, ...categories.map(category => ({ value: String(category.ID), label: localizedName(i18n.resolvedLanguage || i18n.language, category.NameZH, category.NameEN, category.Name) }))]} disabled={busy || loading || loadError} {...form.getInputProps('CategoryID')} />
          <TagsInput label={t('entries.tags')} data={tags} disabled={busy} {...form.getInputProps('Tags')} />
          <JsonInput label={t('entries.clientExtra')} description={lineExtraReadOnly ? t('entryFlow.lineExtraAdminOnly') : t('entries.clientExtraHint')} autosize minRows={2} maxRows={8} formatOnBlur validationError={t('overrides.invalid')} disabled={busy || lineExtraReadOnly} {...form.getInputProps('ClientExtra')} />
          <Switch label={t('entries.enabled')} disabled={busy} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={uncertain || !!ipError || loading || accessBlocked || form.values.mode === 'line' && canManageLines && !endpoint?.host.trim()}>{t(form.values.CreateIPVariants ? 'entryIP.createVariants' : 'common.save')}</Button></Group>
        </Stack>
      </form>
    </Modal>
    {creatingInbound && <EntryInboundCreator nodes={nodes} initialServerID={initialServerID ?? selected?.node.id} onClose={() => setCreatingInbound(false)} onCreated={choice => {
      setAccessEdit(null); void access.refetch(); setCreatedInbound(choice); setCreatingInbound(false)
      form.setValues({ CreateIPVariants: false, InboundID: String(choice.ib.ID), FollowIngress: canFollowIngress(choice, entry?.ChainID), ...entryEndpoint(choice), ...(form.values.mode !== 'client' ? newHostPolicy(entryEndpoint(choice).DisplayHost) : {}), ...(!form.values.Name ? { Name: choice.ib.Tag } : {}) })
    }} />}
  </>
}
