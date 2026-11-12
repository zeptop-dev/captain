import { ActionIcon, Alert, Badge, Button, Card, Group, Modal, NumberInput, Select, SimpleGrid, Stack, Switch, Table, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { api, ApiError, type Group as UGroup, type Plan } from '../lib/api'
import { bytes, money } from '../lib/format'
import { toast } from '../lib/notify'
import { PlanAccessFields, planAccessDraft, type PlanAccessDraft } from '../components/PlanAccessFields'
import { useNodeAccess } from '../lib/node-access-query'

import { planNameError, planNameKey } from '../lib/plan-names'
import { PageHeader } from '../components/PageHeader'
import { centsToYuan, periodDays, periodPricesPayload, yuanToCents, type PeriodPriceDraft, type PriceInput } from '../lib/plan-prices'

type Values = { Name: string; PriceYuan: PriceInput; PeriodDays: number; ResetDays: number; ResetMode: string; Prices: PeriodPriceDraft[]; QuotaGiB: number; DeviceLimit: number; SpeedLimitMbps: number; GroupID: string; Enabled: boolean }
const empty: Values = { Name: '', PriceYuan: '10.00', PeriodDays: 30, ResetDays: 0, ResetMode: '', Prices: [], QuotaGiB: 100, DeviceLimit: 0, SpeedLimitMbps: 0, GroupID: '', Enabled: true }

export default function PlansPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const {me}=useAuth()
  const admin=me?.role==='admin'
  const q = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const access=useNodeAccess()
  const dismissNotice=useMutation({mutationFn:async()=>{
   const receipt=access.data?.conversion
   if(!receipt)throw new Error(t('nodeAccess.reloadHint'))
   await qc.cancelQueries({queryKey:['node-access']})
   return api.post('/api/admin/node-access/conversion/dismiss',{at:receipt.at,inbounds:receipt.inbounds,plans:receipt.plans})
  },onSuccess:async()=>{await qc.cancelQueries({queryKey:['node-access']});await qc.invalidateQueries({queryKey:['node-access']})},onError:toast.err})
  const [originalGrant,setOriginalGrant]=useState<PlanAccessDraft|null>(null)
  const [grant,setGrant]=useState<PlanAccessDraft|null>(null)
  const [editing, setEditing] = useState<Plan | 'new' | null>(null)
  const form = useForm<Values>({ initialValues: empty, validate: values => {
    const errors: Record<string, string> = {}
    const nameError=planNameError(values.Name,q.data??[],editing && editing!=='new'?editing.ID:0)
    if(nameError)errors.Name=t(`planNames.${nameError}`)
    if (yuanToCents(values.PriceYuan) === null) errors.PriceYuan = t('plans.invalidPrice')
    const seen = new Set([Number(values.PeriodDays)])
    values.Prices.forEach((price, index) => {
      const days = periodDays(price.days)
      if (days === null) errors[`Prices.${index}.days`] = t('plans.invalidPeriod')
      else if (seen.has(days)) errors[`Prices.${index}.days`] = t('plans.duplicatePeriod')
      else seen.add(days)
      if (yuanToCents(price.yuan) === null) errors[`Prices.${index}.yuan`] = t('plans.invalidPrice')
    })
    return errors
  } })
  const accessChanged=editing==='new'||JSON.stringify(grant&&{...grant,confirm_shared:false})!==JSON.stringify(originalGrant&&{...originalGrant,confirm_shared:false})
  const payload = (v: Values) => ({ Name: editing && editing!=='new' && editing.Name===v.Name ? v.Name : v.Name.trim(), PriceCents: yuanToCents(v.PriceYuan)!, PeriodDays: v.PeriodDays, ResetDays: v.ResetMode === 'days' ? v.ResetDays : 0, ResetMode: v.ResetMode, Prices: periodPricesPayload(v.Prices), QuotaBytes: Math.round(v.QuotaGiB * 2 ** 30), DeviceLimit: v.DeviceLimit, SpeedLimitMbps: v.SpeedLimitMbps, Enabled: v.Enabled, ...(grant&&grant.mode!=='legacy'&&accessChanged?{Access:grant}:{}) })
  const save = useMutation({ mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/plans', payload(v)) : api.patch(`/api/admin/plans/${(editing as Plan).ID}`, payload(v)), onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); setGrant(null);qc.invalidateQueries({queryKey:['groups']}); qc.invalidateQueries({ queryKey: ['plans'] }); qc.invalidateQueries({ queryKey: ['node-access'] }) }, onError: (error: Error) => {
   if(error instanceof ApiError && error.message==='plan name is already in use'){form.setFieldError('Name',t('planNames.duplicate'));void qc.invalidateQueries({queryKey:['plans']})}
   else if(error instanceof ApiError && error.message==='plan name is already used by another permission scheme'){form.setFieldError('Name',t('planNames.schemeConflict'))}
   else toast.err(error)
  } })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/plans/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['plans'] }); qc.invalidateQueries({ queryKey: ['node-access'] }) }, onError: toast.err })
  const openEdit = (p: Plan | 'new') => { form.clearErrors(); form.setValues(p === 'new' ? empty : { Name: p.Name, PriceYuan: centsToYuan(p.PriceCents), PeriodDays: p.PeriodDays, ResetDays: p.ResetDays ?? 0, ResetMode: p.ResetMode || (p.ResetDays ? 'days' : ''), Prices: (p.Prices ?? []).map(price => ({ days: price.period_days, yuan: centsToYuan(price.price_cents) })), QuotaGiB: p.QuotaBytes / 2 ** 30, DeviceLimit: p.DeviceLimit, SpeedLimitMbps: p.SpeedLimitMbps, GroupID: p.GroupID ? String(p.GroupID) : '', Enabled: p.Enabled });const draft=access.data?planAccessDraft(access.data,p==='new'?0:p.ID,p==='new'?null:p.GroupID):null;setGrant(draft);setOriginalGrant(draft);form.resetDirty(form.getValues());setEditing(p) }
  const close=()=>{if(save.isPending)return;if(form.isDirty()||JSON.stringify(grant)!==JSON.stringify(originalGrant)){modals.openConfirmModal({title:t('workspace.unsaved'),children:<Text>{t('workspace.leaveHint')}</Text>,labels:{confirm:t('workspace.discard'),cancel:t('workspace.stay')},onConfirm:()=>setEditing(null)})}else setEditing(null)}
  return (
    <>
      <PageHeader title={t('plans.title')} subtitle={t('plans.subtitle')} actions={<Button disabled={!admin||!access.data||access.isError} leftSection={<IconPlus size={16} />} onClick={() => openEdit('new')}>{t('plans.create')}</Button>} />
      {admin && !!access.data?.conversion?.inbounds && !access.data.conversion.dismissed && <Alert mb="md" color="blue" withCloseButton={!dismissNotice.isPending} closeButtonLabel={t('planSelection.dismissNotice')} onClose={()=>dismissNotice.mutate()}>{t('planSelection.converted', {nodes:access.data.conversion.inbounds,plans:access.data.conversion.plans})}</Alert>}
      {q.data?.some((p,i,all)=>all.some((v,j)=>i!==j && planNameKey(v.Name)===planNameKey(p.Name))) && <Alert mb="md" color="yellow">{t('planNames.legacyDuplicates')}</Alert>}
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('plans.name')}</Table.Th><Table.Th>{t('plans.price')}</Table.Th><Table.Th>{t('plans.period')}</Table.Th><Table.Th>{t('plans.quota')}</Table.Th><Table.Th>{t('planAccess.mode')}</Table.Th><Table.Th>{t('plans.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((p) => (
            <Table.Tr key={p.ID}>
              <Table.Td><Text fw={600}>{p.Name}</Text></Table.Td>
              <Table.Td>{money(p.PriceCents)}</Table.Td>
              <Table.Td>{p.PeriodDays || '∞'}</Table.Td>
              <Table.Td>{p.QuotaBytes ? bytes(p.QuotaBytes) : '∞'}</Table.Td>
              <Table.Td>{access.data?.plans.find(v=>v.id===p.ID)?.independent?t('planAccess.independent'):p.GroupID?`${t('planAccess.shared')} · ${groups.data?.find(g=>g.ID===p.GroupID)?.Name??p.GroupID}`:t('planAccess.legacy')}</Table.Td>
              <Table.Td>{p.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
              <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => openEdit(p)}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(p.ID) })}><IconTrash size={16} /></ActionIcon></Group></Table.Td>
            </Table.Tr>
          ))}
          {q.data?.length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Modal opened={editing !== null} onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={editing === 'new' ? t('plans.create') : t('common.edit')} size="lg">
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><fieldset disabled={save.isPending} style={{border:0,padding:0,margin:0,minWidth:0}}><Stack>
          <TextInput label={t('plans.name')} description={t('planNames.hint')} required maxLength={200} {...form.getInputProps('Name')} />
          <Group grow align="flex-start"><NumberInput label={t('plans.price')} min={0} decimalScale={2} fixedDecimalScale allowNegative={false} clampBehavior="none" {...form.getInputProps('PriceYuan')} /><NumberInput label={t('plans.period')} description={t('plans.periodHint')} min={0} {...form.getInputProps('PeriodDays')} /></Group>
          <Group grow align="flex-start">
            <Select label={t('plans.resetMode')} data={[{ value: '', label: t('plans.resetNever') }, { value: 'days', label: t('plans.resetDaysMode') }, { value: 'monthly', label: t('plans.resetMonthly') }, { value: 'yearly', label: t('plans.resetYearly') }]} allowDeselect={false} {...form.getInputProps('ResetMode')} />
            {form.values.ResetMode === 'days' && <NumberInput label={t('plans.resetDays')} description={t('plans.resetHint')} min={1} {...form.getInputProps('ResetDays')} />}
          </Group>
          <Stack gap="xs">
            <Text size="sm" fw={500}>{t('plans.prices')}</Text>
            <Text size="xs" c="dimmed">{t('plans.pricesHint')}</Text>
            {form.values.Prices.map((_, index) => <Group key={index} align="flex-start" wrap="nowrap" gap="xs">
              <SimpleGrid cols={{ base: 1, xs: 2 }} spacing="xs" style={{ flex: 1, minWidth: 0 }}>
                <NumberInput label={t('plans.period')} min={1} allowDecimal={false} allowNegative={false} clampBehavior="none" {...form.getInputProps(`Prices.${index}.days`)} />
                <NumberInput label={t('plans.price')} min={0} decimalScale={2} fixedDecimalScale allowNegative={false} clampBehavior="none" {...form.getInputProps(`Prices.${index}.yuan`)} />
              </SimpleGrid>
              <ActionIcon mt={26} variant="subtle" color="red" aria-label={t('plans.removePeriod', { index: index + 1 })} onClick={() => form.removeListItem('Prices', index)}><IconTrash size={16} /></ActionIcon>
            </Group>)}
            <Button variant="light" size="xs" leftSection={<IconPlus size={14} />} style={{ alignSelf: 'flex-start' }} onClick={() => form.insertListItem('Prices', { days: '', yuan: '' })}>{t('plans.addPeriod')}</Button>
          </Stack>
          <Group grow><NumberInput label={t('plans.quota')} description={t('plans.quotaHint')} min={0} decimalScale={2} {...form.getInputProps('QuotaGiB')} /><NumberInput label={t('plans.deviceLimit')} description={t('plans.deviceHint')} min={0} {...form.getInputProps('DeviceLimit')} /><NumberInput label={t('plans.speedLimit')} min={0} {...form.getInputProps('SpeedLimitMbps')} /></Group>
          {editing && editing!=='new' && access.data?.plans.find(p=>p.id===editing.ID)?.independent && groups.data?.find(g=>g.ID===editing.GroupID)?.Name!==editing.Name && <Alert color="yellow">{t('planNames.legacyGroup')}</Alert>}
          {admin&&(grant&&access.data?<PlanAccessFields value={grant} onChange={setGrant} directory={access.data} disabled={save.isPending}/>:<Text c="orange">{t('planAccess.reload')}</Text>)}
          <Switch label={t('plans.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" disabled={admin&&(!grant||!access.data||access.isError||access.isFetching||accessChanged&&(grant.revision!==access.data.revision||(grant.mode!=='legacy'&&!grant.confirm_shared)||(grant.mode==='shared'&&!grant.group_id&&!grant.name.trim())))} loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></fieldset></form>
      </Modal>
    </>
  )
}
