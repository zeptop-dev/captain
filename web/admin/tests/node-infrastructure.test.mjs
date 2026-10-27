import assert from 'node:assert/strict'
import test from 'node:test'
import { applyDraftSave, createBillingReason, eligibleBillingAsset, financeDraft, reloadFinance, sameInfrastructureSnapshot, supplierURL, supportedAssetPeriod } from '../src/lib/node-infrastructure.ts'
const asset = { id: 1, active: true, node_id: null, primary_node_id: null, period_months: 1 }
const infra = { revision: 'a', supplier: { id: 2, name: 'Example', url: 'https://example.com' }, billing: { amount: '0', currency: 'USD', cycle: 'annual' }, expires_at: '2027-01-31' }
test('probe and ledger must share the same snapshot before combining initial data', () => {
 assert.equal(sameInfrastructureSnapshot('a', infra), true)
 for (const revision of ['b', '', undefined]) assert.equal(sameInfrastructureSnapshot(revision, infra), false)
 assert.equal(sameInfrastructureSnapshot('a', undefined), false)
})
test('reload replaces only finance and successful save preserves later local edits', () => {
 const before = { ...financeDraft({ provider: 'Legacy', price: '$10?', expires_at: '2027-01-01' }, 'a'), note: 'unsaved note', Hidden: true }
 const saved = financeDraft({ billing: infra.billing, expires_at: infra.expires_at }, 'b', 2)
 assert.equal(reloadFinance(before, saved).note, 'unsaved note')
 assert.equal(reloadFinance(before, saved).InfrastructureRevision, 'b')
 assert.equal(financeDraft({ provider: 'Legacy', price: '$10?' }).costMode, 'legacy')
 const current = { ...before, amount: '12', note: 'typed while saving' }
 const merged = applyDraftSave(current, before, { ...before, ...saved })
 assert.equal(merged.amount, '12'); assert.equal(merged.note, current.note); assert.equal(merged.InfrastructureRevision, 'b')
})
test('primary billing selection rejects other servers, primary assets and inexact cycles', () => {
 assert.equal(eligibleBillingAsset(asset, 5), true)
 assert.equal(eligibleBillingAsset({ ...asset, node_id: 5 }, 5), true)
 for (const changed of [{ node_id: 6 }, { primary_node_id: 5 }, { active: false }, { period_months: 2 }, { period_months: 0 }, { period_months: 121 }]) assert.equal(eligibleBillingAsset({ ...asset, ...changed }, 5), false)
 for (const months of [1, 3, 6, 9, 12, 120]) assert.equal(supportedAssetPeriod(months), true)
})
test('creating a primary asset requires saved structured data and never interprets legacy free text', () => {
 assert.equal(createBillingReason(infra), null)
 assert.equal(createBillingReason({ ...infra, supplier: null }), 'supplierRequired')
 assert.equal(createBillingReason({ ...infra, billing: null, price: '$10/year' }), 'structuredRequired')
 assert.equal(createBillingReason({ ...infra, billing: { ...infra.billing, amount: '' } }), 'structuredRequired')
 assert.equal(createBillingReason({ ...infra, billing: { ...infra.billing, cycle: 'custom', years: '0.1667' } }), 'periodRequired')
 assert.equal(createBillingReason({ ...infra, expires_at: '' }), 'expiryRequired')
})
test('supplier navigation accepts web URLs without embedded credentials', () => {
 for (const value of ['javascript:alert(1)', 'https://u:p@example.com', 'data:text/html,x']) assert.equal(supplierURL(value), undefined)
 assert.equal(supplierURL('https://example.com'), 'https://example.com/')
})
