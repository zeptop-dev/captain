import { Alert, Button, Checkbox, Modal, NumberInput, Select, Stack, Text, Textarea, TextInput } from '@mantine/core'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { infraMoney, type InfraAsset } from '../lib/node-infrastructure'
import { newPaymentDraft, paymentDateError, paymentPayload, reloadPaymentDraft, validBillingDate, type PaymentDateAction, type PaymentDraft } from '../lib/infrastructure-payment'
import { toast } from '../lib/notify'

export function InfrastructurePaymentModal({ asset, onClose, onSaved }: { asset: InfraAsset; onClose: () => void; onSaved: () => void }) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState(() => newPaymentDraft(asset, new Date().toISOString().slice(0, 10), crypto.randomUUID()))
  const [conflict, setConflict] = useState(false)
  const [reloaded, setReloaded] = useState(false)
  const record = useMutation({ mutationFn: (submitted: PaymentDraft) => api.post(`/api/admin/settings/infrastructure/assets/${submitted.asset.id}/payments`, paymentPayload(submitted)), onSuccess: () => { onSaved(); onClose(); toast.ok(t('common.saved')) }, onError: error => {
    if (error instanceof ApiError && error.status === 409) { setConflict(true); setDraft(previous => ({ ...previous, confirm: false })) }
    else toast.err(error)
  } })
  const reload = useMutation({ mutationFn: async () => {
    const fresh = await api.get<{ assets: InfraAsset[] }>('/api/admin/settings/infrastructure')
    const latest = fresh.assets.find(item => item.id === asset.id)
    if (!latest?.active) throw new Error(t('infraPayment.unavailable'))
    return latest
  }, onSuccess: latest => { setDraft(previous => reloadPaymentDraft(previous, latest)); setConflict(false); setReloaded(true) }, onError: toast.err })
  const busy = record.isPending || reload.isPending
  const edit = (patch: Partial<PaymentDraft>) => setDraft(previous => ({ ...previous, ...patch, confirm: false }))
  const dateError = paymentDateError(draft)
  const oneTime = draft.date_action === ''
  return <Modal opened onClose={() => !busy && onClose()} title={t('infra.record')} centered>
    <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack>
      <Text fw={600}>{draft.asset.name} · {draft.asset.supplier_name}</Text>
      <Text size="sm">{t('infra.nextDue')}: {draft.asset.next_due}</Text>
      {oneTime ? <Text size="sm">{t('infraPayment.oneTimeHint')}</Text> : <>
        <Select label={t('infraPayment.dateAction')} allowDeselect={false} data={['record_only', 'set_due_date'].map(value => ({ value, label: t(`infraPayment.${value}`) }))} value={draft.date_action} onChange={value => value && edit({ date_action: value as PaymentDateAction })} />
        <Text size="xs" c="dimmed">{t(draft.asset.auto_renewal_enabled ? 'infraPayment.automaticHint' : 'infraPayment.manualHint')}</Text>
        {draft.date_action === 'set_due_date' ? <>
          <TextInput type="date" label={t('infraPayment.targetDate')} min={draft.asset.next_due} max="9900-12-31" value={draft.next_due} onChange={event => edit({ next_due: event.currentTarget.value })} error={dateError ? t(`infraPayment.${dateError}`) : undefined} />
          <Text size="sm">{draft.asset.next_due} → {draft.next_due || '—'}</Text>
          <Text size="xs" c="dimmed">{t('infraPayment.setDateHint')}</Text>
        </> : <Text size="sm">{t('infraPayment.unchangedDate', { date: draft.asset.next_due })}</Text>}
      </>}
      <NumberInput label={`${t('infra.amountMinor')} (${draft.asset.currency})`} allowDecimal={false} min={0} max={1e12} value={draft.amount_minor} onChange={value => edit({ amount_minor: Number(value) })} />
      <Text>{infraMoney(draft.amount_minor, draft.asset.currency, i18n.language)}</Text>
      <TextInput type="date" label={t('infra.paidDate')} min="1970-01-01" max="9900-12-31" value={draft.paid_date} onChange={event => edit({ paid_date: event.currentTarget.value })} />
      <TextInput label={t('infra.reference')} value={draft.reference} onChange={event => edit({ reference: event.currentTarget.value })} />
      <Textarea label={t('infra.notes')} value={draft.notes} onChange={event => edit({ notes: event.currentTarget.value })} />
      {reloaded && <Alert color="blue">{t('infraPayment.reloaded')}</Alert>}
      {conflict && <Alert color="yellow"><Text size="sm">{t('infraPayment.conflict')}</Text><Button variant="subtle" size="xs" onClick={() => reload.mutate()} loading={reload.isPending}>{t('infraPayment.reload')}</Button></Alert>}
      <Checkbox label={t('infra.confirmPayment')} checked={draft.confirm} disabled={conflict} onChange={event => { const confirm = event.currentTarget.checked; setDraft(previous => ({ ...previous, confirm })) }} />
      <Button disabled={conflict || !draft.confirm || !validBillingDate(draft.paid_date) || !!dateError} loading={record.isPending} onClick={() => record.mutate(draft)}>{t('infra.record')}</Button>
    </Stack></fieldset>
  </Modal>
}
