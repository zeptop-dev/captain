import { Alert, Anchor, Button, Card, Checkbox, Group, Modal, NumberInput, Select, SimpleGrid, Stack, Text, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { toast } from '../lib/notify'
import { createBillingReason, eligibleBillingAsset, infraMoney, supplierURL, nodeInfrastructureKey, nodeInfrastructureURL, suppliersKey, type FinanceDraft, type InfraAsset, type NodeInfrastructure, type SupplierOption } from '../lib/node-infrastructure'

const suppliersURL = '/api/admin/infrastructure/suppliers'

export function NodeSupplierFields({ value, options, selected, canManage, disabled, linked, billingLinked, onChange }: { value: FinanceDraft; options: SupplierOption[]; selected?: SupplierOption | null; canManage: boolean; disabled: boolean; linked: boolean; billingLinked: boolean; onChange: (value: Partial<FinanceDraft>) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [draft, setDraft] = useState<{ name: string; url: string } | null>(null)
  const create = useMutation({ mutationFn: (input: { name: string; url: string }) => api.post<SupplierOption>(suppliersURL, input), onSuccess: supplier => {
    qc.setQueryData<SupplierOption[]>(suppliersKey, old => [...(old ?? []).filter(s => s.id !== supplier.id), supplier])
    qc.invalidateQueries({ queryKey: suppliersKey }); qc.invalidateQueries({ queryKey: ['infrastructure'] })
    onChange({ SupplierID: supplier.id, provider: supplier.name, provider_url: supplier.url }); setDraft(null); toast.ok(t('infraLink.supplierCreated'))
  }, onError: toast.err })
  const all = selected ? [...options.filter(s => s.id !== selected.id), selected] : [...options]
  const current = all.find(s => s.id === value.SupplierID)
  if (value.SupplierID && !current) all.push({ id: value.SupplierID, name: value.provider || `#${value.SupplierID}`, url: value.provider_url })
  const url = supplierURL(value.provider_url)
  return <>
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
      <Stack gap={4}>
        {canManage ? <Select label={t('nodeProbe.provider')} searchable clearable={!billingLinked} nothingFoundMessage={t('infraLink.noSuppliers')} data={[...(!billingLinked ? [{ value: '0', label: t('infraLink.unlinkedProvider') }] : []), ...all.map(s => ({ value: String(s.id), label: s.name })), { value: '__new_supplier__', label: `＋ ${t('infra.addSupplier')}` }]} value={String(value.SupplierID)} disabled={disabled} onChange={id => {
          if (id === '__new_supplier__') { setDraft({ name: '', url: '' }); return }
          const supplier = all.find(s => s.id === Number(id))
          onChange(supplier ? { SupplierID: supplier.id, provider: supplier.name, provider_url: supplier.url } : { SupplierID: 0 })
        }} /> : <TextInput label={t('nodeProbe.provider')} value={value.provider} readOnly={linked} disabled={disabled} onChange={event => onChange({ provider: event.currentTarget.value })} />}
        {canManage && !value.SupplierID && <TextInput label={t('infraLink.legacyProvider')} value={value.provider} disabled={disabled} onChange={event => onChange({ provider: event.currentTarget.value })} />}
        {canManage && <Group gap="xs"><Button size="compact-xs" variant="light" disabled={disabled} onClick={() => setDraft({ name: '', url: '' })}>{t('infra.addSupplier')}</Button><Button size="compact-xs" variant="subtle" component={Link} to="/infrastructure?tab=suppliers">{t('infraLink.manageSuppliers')}</Button></Group>}
      </Stack>
      <Stack gap={4}><TextInput label={t('nodeProbe.providerURL')} placeholder="https://example.com" value={value.provider_url} readOnly={!!value.SupplierID || linked} disabled={disabled} onChange={event => onChange({ provider_url: event.currentTarget.value })} />{url && <Anchor size="xs" href={url} target="_blank" rel="noopener noreferrer">{t('infraLink.visitSupplier')}</Anchor>}</Stack>
    </SimpleGrid>
    {billingLinked && canManage && <Text size="xs" c="dimmed">{t('infraLink.supplierClearRequiresUnlink')}</Text>}
    <Text size="xs" c="dimmed">{t(canManage ? 'infraLink.supplierHint' : linked ? 'infraLink.operatorSupplier' : 'infraLink.legacyHint')}</Text>
    <Modal opened={!!draft} onClose={() => !create.isPending && setDraft(null)} title={t('infra.addSupplier')} centered><Stack>
      <TextInput label={t('infra.name')} maxLength={100} value={draft?.name ?? ''} disabled={create.isPending} onChange={event => { const name = event.currentTarget.value; setDraft(previous => previous && ({ ...previous, name })) }} />
      <TextInput label={t('infra.url')} placeholder="https://example.com" maxLength={2000} value={draft?.url ?? ''} disabled={create.isPending} onChange={event => { const url = event.currentTarget.value; setDraft(previous => previous && ({ ...previous, url })) }} />
      <Text size="xs" c="dimmed">{t('infraLink.newSupplierHint')}</Text>
      <Button disabled={!draft?.name.trim() || !!draft.url && !supplierURL(draft.url)} loading={create.isPending} onClick={() => draft && create.mutate({ name: draft.name.trim(), url: draft.url.trim() })}>{t('common.save')}</Button>
    </Stack></Modal>
  </>
}

export function NodeBillingLink({ nodeID, infra, disabled, dirty, onApplied }: { nodeID: number; infra: NodeInfrastructure; disabled: boolean; dirty: boolean; onApplied: () => Promise<boolean> }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const [action, setAction] = useState<'create_asset' | 'link_asset' | 'unlink_asset' | null>(null)
  const [selected, setSelected] = useState<InfraAsset | null>(null)
  const [snapshot, setSnapshot] = useState<NodeInfrastructure | null>(null)
  const [name, setName] = useState('')
  const [remindDays, setRemindDays] = useState<string | number>(7)
  const [confirm, setConfirm] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [refreshFailed, setRefreshFailed] = useState(false)
  const assets = useQuery({ queryKey: ['infrastructure'], queryFn: () => api.get<{ assets: InfraAsset[] }>('/api/admin/settings/infrastructure'), enabled: action === 'link_asset', refetchOnWindowFocus: false })
  const eligible = (assets.data?.assets ?? []).filter(asset => eligibleBillingAsset(asset, nodeID))
  const open = (next: typeof action) => { setAction(next); setSnapshot(next ? infra : null); setSelected(null); setName(''); setRemindDays(7); setConfirm(false); setConflict(false) }
  const cancelReads = () => Promise.all([qc.cancelQueries({ queryKey: ['node-probe', nodeID] }), qc.cancelQueries({ queryKey: nodeInfrastructureKey(nodeID) })])
  const apply = useMutation({ onMutate: cancelReads, mutationFn: () => api.put<NodeInfrastructure>(nodeInfrastructureURL(nodeID), { revision: snapshot?.revision, action, ...(action === 'create_asset' ? { name: name.trim(), remind_days: Number(remindDays) } : {}), ...(action === 'link_asset' ? { asset_id: selected?.id, asset_revision: selected?.revision } : {}), confirm_disable_auto_renewal: confirm }), onSuccess: async result => {
    await cancelReads()
    qc.setQueryData(nodeInfrastructureKey(nodeID), result)
    qc.invalidateQueries({ queryKey: ['infrastructure'] }); qc.invalidateQueries({ queryKey: ['infra-payments'] })
    setAction(null); toast.ok(t('common.saved'))
    setRefreshFailed(!await onApplied())
  }, onError: error => { if (error instanceof ApiError && error.status === 409) setConflict(true); else toast.err(error) } })
  const reason = createBillingReason(action ? snapshot ?? infra : infra)
  const previewInfra = snapshot ?? infra
  const preview = (asset?: InfraAsset | null) => <Stack gap={3}><Text size="sm">{t('infra.supplier')}: {asset?.supplier_name ?? previewInfra.supplier?.name ?? '—'}</Text><Text size="sm">{t('infra.amount')}: {asset ? infraMoney(asset.amount_minor, asset.currency, i18n.language) : previewInfra.billing ? `${previewInfra.billing.currency} ${previewInfra.billing.amount}` : previewInfra.price || '—'}</Text><Text size="sm">{t('nodeCost.cycle')}: {asset ? t('infra.everyMonths', { count: asset.period_months }) : previewInfra.billing ? `${t(`nodeCost.${previewInfra.billing.cycle}`)}${previewInfra.billing.cycle === 'custom' ? ` (${previewInfra.billing.years})` : ''}` : '—'}</Text><Text size="sm">{t('nodeProbe.expires')}: {asset?.next_due ?? (previewInfra.expires_at || '—')}</Text></Stack>
  return <>
    <Alert id="billing-link" color={infra.asset ? 'blue' : 'gray'} title={t(infra.asset ? 'infraLink.linkedTitle' : 'infraLink.unlinkedTitle')}>
      <Stack gap="xs"><Text size="sm">{t(infra.asset ? 'infraLink.linkedHint' : 'infraLink.unlinkedHint')}</Text>
        {infra.asset && <Text fw={500} size="sm">{infra.asset.name}</Text>}{infra.asset && !infra.asset.active && <Text size="sm" c="orange">{t('infraLink.archivedLinked')}</Text>}
        <Group gap="xs">{infra.asset ? <><Button component={Link} to={`/infrastructure?asset=${infra.asset.id}&filter=all`} variant="light" size="xs">{t('infraLink.openAsset')}</Button><Button variant="subtle" color="orange" size="xs" disabled={disabled || dirty} onClick={() => open('unlink_asset')}>{t('infraLink.unlink')}</Button></> : <><Button size="xs" variant="light" disabled={disabled || dirty || !!reason} onClick={() => open('create_asset')}>{t('infraLink.createAsset')}</Button><Button size="xs" variant="subtle" disabled={disabled || dirty} onClick={() => open('link_asset')}>{t('infraLink.linkExisting')}</Button></>}</Group>
        {dirty ? <Text size="xs">{t('infraLink.saveFirst')}</Text> : !infra.asset && reason && <Text size="xs">{t(`infraLink.${reason}`)}</Text>}
      </Stack>
    </Alert>
    {refreshFailed && <Alert color="yellow"><Text size="sm">{t('infraLink.savedRefreshNeeded')}</Text><Button size="xs" variant="subtle" onClick={async () => setRefreshFailed(!await onApplied())}>{t('infraLink.reloadFinance')}</Button></Alert>}
    <Modal opened={!!action} onClose={() => !apply.isPending && open(null)} title={t(action === 'unlink_asset' ? 'infraLink.unlink' : action === 'link_asset' ? 'infraLink.linkExisting' : 'infraLink.createAsset')} size="lg" centered>
      <Stack>
        {action === 'unlink_asset' ? <Text>{t('infraLink.unlinkHint')}</Text> : <>
          <Text size="sm">{t(action === 'create_asset' ? 'infraLink.createDirection' : 'infraLink.linkDirection')}</Text>
          {action === 'create_asset' ? <><TextInput label={t('infra.name')} maxLength={150} value={name} disabled={apply.isPending} onChange={event => setName(event.currentTarget.value)} /><NumberInput label={t('infra.remindDays')} min={0} max={90} allowDecimal={false} value={remindDays} disabled={apply.isPending} onChange={setRemindDays} />{preview()}</> : <>
            {assets.isError && <Alert color="red">{t('workspace.loadError')}</Alert>}
            <Select label={t('infra.asset')} searchable clearable disabled={assets.isPending || assets.isError || apply.isPending} data={eligible.map(asset => ({ value: String(asset.id), label: `${asset.name} · ${asset.supplier_name}` }))} value={selected ? String(selected.id) : null} onChange={value => setSelected(eligible.find(asset => String(asset.id) === value) ?? null)} nothingFoundMessage={t('infraLink.noAssets')} />
            <Text size="xs" c="dimmed">{t('infraLink.eligibleHint')}</Text>
            <SimpleGrid cols={{ base: 1, sm: 2 }}><Card withBorder><Text fw={500} mb="xs">{t('infraLink.currentServer')}</Text>{preview()}</Card>{selected && <Card withBorder><Text fw={500} mb="xs">{t('infraLink.selectedAsset')}</Text>{preview(selected)}</Card>}</SimpleGrid>
          </>}
          <Alert color="yellow"><Text size="sm">{t('infraLink.renewalWarning')}</Text></Alert>
        </>}
        <Checkbox label={t(action === 'unlink_asset' ? 'infraLink.confirmUnlink' : 'infraLink.confirmLink')} checked={confirm} disabled={apply.isPending} onChange={event => setConfirm(event.currentTarget.checked)} />
        {conflict && <Alert color="yellow"><Text size="sm">{t('infraLink.conflict')}</Text><Button variant="subtle" size="xs" onClick={() => { open(null); void onApplied() }}>{t('infraLink.reloadFinance')}</Button></Alert>}
        <Button disabled={!confirm || conflict || action === 'create_asset' && (!name.trim() || !!reason) || action === 'link_asset' && (!selected || assets.isError)} loading={apply.isPending} onClick={() => apply.mutate()}>{t('common.save')}</Button>
      </Stack>
    </Modal>
  </>
}
