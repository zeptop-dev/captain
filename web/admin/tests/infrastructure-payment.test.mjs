import assert from 'node:assert/strict'
import test from 'node:test'
import { newPaymentDraft, nextBillingDate, paymentDateError, paymentPayload, reloadPaymentDraft } from '../src/lib/infrastructure-payment.ts'

const asset = { id: 1, revision: 7, primary_node_id: 4, auto_renewal_enabled: true, amount_minor: 1200, currency: 'USD', period_months: 1, anchor_day: 31, next_due: '2027-01-31', active: true }
test('online date estimation defaults to payment-only and never submits a hidden due date', () => {
 const draft = newPaymentDraft(asset, '2027-01-20', 'key-1')
 assert.equal(draft.date_action, 'record_only')
 assert.equal(draft.confirm, false)
 const payload = paymentPayload({ ...draft, next_due: '2099-01-01' })
 assert.equal(payload.date_action, 'record_only'); assert.equal(payload.next_due, undefined)
 assert.equal(payload.revision, 7); assert.equal(payload.request_key, 'key-1')
})
test('manual payment previews a single explicit date with month-end anchor recovery', () => {
 const draft = newPaymentDraft({ ...asset, auto_renewal_enabled: false }, '2027-01-20', 'key-2')
 assert.equal(draft.date_action, 'set_due_date'); assert.equal(draft.next_due, '2027-02-28')
 assert.equal(paymentPayload(draft).next_due, '2027-02-28')
 assert.equal(nextBillingDate({ ...asset, next_due: '2027-02-28' }), '2027-03-31')
 assert.equal(nextBillingDate({ ...asset, next_due: '2028-01-31' }), '2028-02-29')
 assert.equal(nextBillingDate({ ...asset, period_months: 3 }), '2027-04-30')
})
test('explicit targets may equal current expiry but cannot rewind or normalize invalid dates', () => {
 const draft = { ...newPaymentDraft(asset, '2027-01-20', 'key-3'), date_action: 'set_due_date' }
 assert.equal(paymentDateError({ ...draft, next_due: asset.next_due }), null)
 assert.equal(paymentDateError({ ...draft, next_due: '2027-01-30' }), 'earlierDate')
 assert.equal(paymentDateError({ ...draft, next_due: '2027-02-29' }), 'invalidDate')
 assert.equal(paymentDateError({ ...draft, next_due: '9901-01-01' }), 'invalidDate')
 assert.equal(paymentDateError({ ...draft, date_action: 'record_only', next_due: '' }), null)
})
test('a fresh conflict snapshot resets action, target and approval without replaying stale dates', () => {
 const previous = { ...newPaymentDraft({ ...asset, auto_renewal_enabled: false }, '2027-01-20', 'old-key'), next_due: '2030-12-31', confirm: true, amount_minor: 1500, reference: 'Receipt 42', notes: 'Keep these payment details' }
 const fresh = { ...asset, revision: 8, next_due: '2027-03-31' }
 const reloaded = reloadPaymentDraft(previous, fresh)
 assert.equal(reloaded.date_action, 'record_only'); assert.equal(reloaded.next_due, '2027-04-30'); assert.equal(reloaded.confirm, false)
 assert.equal(reloaded.reference, previous.reference); assert.equal(reloaded.notes, previous.notes); assert.equal(reloaded.amount_minor, 1500)
 assert.equal(paymentPayload(reloaded).revision, 8); assert.equal(paymentPayload(reloaded).request_key, 'old-key'); assert.equal(paymentPayload(reloaded).next_due, undefined)
 assert.equal(reloadPaymentDraft(previous, { ...fresh, currency: 'JPY', amount_minor: 900 }).amount_minor, 900)
})
test('independent one-time assets keep the legacy payment-and-archive contract', () => {
 const draft = newPaymentDraft({ ...asset, primary_node_id: null, auto_renewal_enabled: false, period_months: 0 }, '2027-01-20', 'one-time')
 assert.equal(draft.date_action, ''); assert.equal(draft.next_due, '')
 assert.equal(Object.hasOwn(paymentPayload(draft), 'date_action'), false)
 assert.equal(Object.hasOwn(paymentPayload(draft), 'next_due'), false)
})

test('ambiguous payment retries retain the original request key across repeated reloads', () => {
 const submitted = { ...newPaymentDraft(asset, '2027-01-20', 'committed-response-lost'), confirm: true }
 const editedRetry = { ...submitted, notes: 'Edited after the response was lost', amount_minor: 1400 }
 const firstReload = reloadPaymentDraft(editedRetry, { ...asset, revision: 8, next_due: '2027-02-28' })
 const secondReload = reloadPaymentDraft(firstReload, { ...asset, revision: 9, next_due: '2027-03-31' })
 assert.equal(paymentPayload(firstReload).request_key, paymentPayload(submitted).request_key)
 assert.equal(paymentPayload(secondReload).request_key, paymentPayload(submitted).request_key)
 assert.equal(secondReload.confirm, false)
 assert.equal(secondReload.notes, editedRetry.notes)
 assert.equal(paymentPayload(secondReload).revision, 9)
})
