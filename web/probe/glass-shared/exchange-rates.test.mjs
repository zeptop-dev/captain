import { test } from 'node:test'
import assert from 'node:assert/strict'
import { summarizeConvertedNodeCosts } from '../src/lib/node-cost.ts'

const now = new Date('2026-10-24T00:00:00Z')
const info = (amount, currency = 'USD') => ({ billing: { amount, currency, cycle: 'annual' }, expires_at: '2027-10-24' })
// Fabricated rates intentionally differ from real quotations.
const fx = () => ({ base: 'USD', source: 'Frankfurter', fetched_at: now.getTime() / 1000, stale: false,
  rates: { USD: 1, CNY: 8, EUR: 2 }, dates: { USD: '2026-10-24', CNY: '2026-10-23', EUR: '2026-10-22' } })
const sum = (infos, exchange = fx(), currency = 'CNY', kind = 'remaining') => summarizeConvertedNodeCosts(infos, kind, currency, exchange, now)

test('cross rates convert all currencies into the configured summary currency without rounding each server', () => {
  const infos = [info('120'), info('30', 'EUR'), info('1.23', 'CNY')]
  const original = structuredClone(infos)
  assert.deepEqual(sum(infos), { money: { currency: 'CNY', amount: 1081.23 }, missingCurrencies: [], stale: false, rateDate: '2026-10-22' })
  assert.equal(sum(infos, fx(), 'EUR').money.amount, 270.3075)
  assert.equal(sum(infos, fx(), 'CNY', 'yearly').money.amount, 1081.23)
  assert(Math.abs(sum(infos, fx(), 'CNY', 'monthly').money.amount - 90.1025) < 1e-10)
  assert.deepEqual(infos, original)
  assert.equal(sum([info('0.0001'), info('0.0001')]).money.amount, 0.0016)
})

test('same-currency and zero totals do not require exchange rates; missing prices remain unknown', () => {
  assert.deepEqual(sum([info('12.3', 'CNY'), info('0', 'JPY'), {}], null), {
    money: { currency: 'CNY', amount: 12.3 }, missingCurrencies: [], stale: false,
  })
  assert.equal(sum([info('0', 'EUR')], null).money.amount, 0)
  assert.equal(sum([{ price: 'USD 1 / year' }], null).money, null)
  assert.equal(sum([info('12.3', 'CNY')], null, 'XXX').money, null)
})

test('one missing or invalid quote suppresses the complete total instead of silently undercounting', () => {
  for (const rate of [undefined, 0, -1, Infinity, NaN, '2']) {
    const exchange = fx(); exchange.rates.EUR = rate
    const summary = sum([info('120'), info('30', 'EUR')], exchange)
    assert.equal(summary.money, null)
    assert.deepEqual(summary.missingCurrencies, ['EUR'])
  }
  assert.deepEqual(sum([info('120')], null).missingCurrencies, ['CNY', 'USD'])
  const missingTarget = fx(); delete missingTarget.rates.CNY
  assert.equal(sum([info('1', 'CNY'), info('120')], missingTarget).money, null)
  const overflow = fx(); overflow.rates.CNY = Number.MAX_VALUE
  assert.equal(sum([info('120')], overflow).money, null)
})

test('actual quote dates are retained and cached estimates are marked stale without discarding them', () => {
  const exchange = fx(); exchange.dates.CNY = '2026-10-25'
  assert.equal(sum([info('1')], exchange).money.amount, 8)
  exchange.dates.EUR = '2026-10-10'
  assert.equal(sum([info('1', 'EUR')], exchange).stale, true)
  exchange.dates.EUR = '2026-10-22'; exchange.fetched_at -= 2 * 86400
  assert.equal(sum([info('1', 'EUR')], exchange).stale, true)
  assert.equal(sum([info('1', 'EUR')], exchange).money.amount, 4)
  exchange.fetched_at = now.getTime() / 1000; exchange.stale = true
  assert.equal(sum([info('1')], exchange).stale, true)
  for (const date of [undefined, '2026-02-30', '2030-01-01']) {
    exchange.dates.EUR = date
    assert.equal(sum([info('1', 'EUR')], exchange).money, null)
  }
})
