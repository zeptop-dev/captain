import assert from 'node:assert/strict'
import test from 'node:test'
import { centsToYuan, periodDays, periodPricesPayload, yuanToCents } from '../src/lib/plan-prices.ts'

test('existing cent prices survive opening and saving without an amount change', () => {
  for (const cents of [0, 1, 10, 29, 57, 1000, 2700, 9900, 999999999, Number.MAX_SAFE_INTEGER]) {
    assert.equal(yuanToCents(centsToYuan(cents)), cents)
  }
  assert.equal(centsToYuan(2700), '27.00')
  assert.equal(yuanToCents(0.29), 29)
  assert.equal(yuanToCents('1.01'), 101)
  assert.equal(yuanToCents('27'), 2700)
})

test('incomplete, negative, imprecise and unsafe prices cannot become zero or rounded money', () => {
  for (const input of ['', ' ', '-', '1.', '-1', '0.001', '1e3', 'NaN', Infinity, '90071992547409.92']) {
    assert.equal(yuanToCents(input), null, String(input))
  }
  assert.equal(yuanToCents('0'), 0)
  assert.equal(centsToYuan(-1), '')
  assert.equal(centsToYuan(1.1), '')
})

test('period rows keep the existing API shape, including free periods and deletion', () => {
  assert.deepEqual(periodPricesPayload([{ days: 90, yuan: '27.00' }, { days: '365', yuan: 99 }, { days: 7, yuan: '0.00' }]), [
    { period_days: 90, price_cents: 2700 }, { period_days: 365, price_cents: 9900 }, { period_days: 7, price_cents: 0 },
  ])
  assert.deepEqual(periodPricesPayload([]), [])
  for (const days of ['', 0, -1, 1.5, '1e3']) assert.equal(periodDays(days), null)
  assert.throws(() => periodPricesPayload([{ days: 90, yuan: '' }]))
  assert.throws(() => periodPricesPayload([{ days: '', yuan: '10' }]))
})
