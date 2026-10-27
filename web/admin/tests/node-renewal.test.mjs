import assert from 'node:assert/strict'
import test from 'node:test'
import { renewalAnchorDay } from '../src/lib/node-renewal.ts'

test('billing-day hints prefer explicit month-end anchors and retain legacy dates', () => {
 assert.equal(renewalAnchorDay({ enabled: true, anchor_date: '2027-02-28', anchor_day: 31 }, '2027-02-28'), 31)
 assert.equal(renewalAnchorDay({ enabled: false, anchor_date: '2027-02-28', anchor_day: 31 }), 31)
 assert.equal(renewalAnchorDay({ enabled: true, anchor_date: '2027-01-31' }, '2027-02-28'), 31)
 for (const anchor_day of [0, 32, -1, 1.5, NaN, '31']) assert.equal(renewalAnchorDay({ enabled: true, anchor_date: '2027-01-31', anchor_day }, '2027-02-28'), 31)
})
test('new expiry drafts use their actual calendar day rather than the saved billing anchor', () => {
 assert.equal(renewalAnchorDay(undefined, '2027-02-28'), 28)
 assert.equal(renewalAnchorDay({ enabled: true, anchor_date: 'invalid' }, '2028-02-29'), 29)
 assert.equal(renewalAnchorDay({ enabled: true, anchor_date: '2027-02-30' }, '2027-03-30'), 30)
 assert.equal(renewalAnchorDay(undefined, '2027-02-29'), null)
})
