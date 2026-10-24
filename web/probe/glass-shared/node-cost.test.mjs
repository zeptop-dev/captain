import { test } from 'node:test'
import assert from 'node:assert/strict'
import { calculateNodeCost, formatCostAmount, summarizeNodeCosts } from '../src/lib/node-cost.ts'

const now = new Date('2026-10-23T00:00:00.000Z')
const info = (amount = '120', cycle = 'annual', currency = 'USD', extra = {}) => ({
  billing: { amount, currency, cycle }, expires_at: '2027-10-23', ...extra,
})
function money(actual, amount, currency = 'USD') {
  assert.notEqual(actual, null)
  assert.equal(actual.currency, currency)
  assert(Number.isFinite(actual.amount))
  assert(Math.abs(actual.amount - amount) <= Math.max(1, Math.abs(amount)) * 1e-12,
    `expected ${currency} ${amount}, got ${actual.currency} ${actual.amount}`)
}

test('monthly cost uses the saved payment period without a fixed days-per-month conversion', () => {
  for (const [cycle, amount, monthly] of [
    ['monthly', '3.81', 3.81], ['quarterly', '36', 12],
    ['semiannual', '72', 12], ['annual', '45.68', 45.68 / 12],
  ]) {
    money(calculateNodeCost(info(amount, cycle), now).monthly, monthly)
  }
  // Custom years can normalize a monthly price without inventing a day count.
  const custom = info('54', 'custom', 'EUR')
  custom.billing.years = '1.5'
  const result = calculateNodeCost(custom, now)
  money(result.monthly, 3, 'EUR')
  assert.equal(result.remaining, null)
  assert.equal(result.remainingReason, 'unsupported-cycle')
})

test('unknown prices and free servers remain distinct, and legacy text is never parsed into money', () => {
  for (const value of [{}, { billing: { amount: '', currency: 'USD', cycle: 'annual' } }]) {
    const result = calculateNodeCost(value, now)
    assert.equal(result.monthly, null)
    assert.equal(result.remaining, null)
    assert.equal(result.remainingReason, 'missing-price')
  }
  for (const price of ['$45.68 / year', 'USD 45.68 / year', 'Free', '免费']) {
    const result = calculateNodeCost({ price, expires_at: '2027-10-23' }, now)
    assert.equal(result.monthly, null)
    assert.equal(result.remaining, null)
    assert.equal(result.remainingReason, 'legacy-price')
  }
  const free = calculateNodeCost(info('0', 'annual', 'JPY', { expires_at: undefined }), now)
  money(free.monthly, 0, 'JPY')
  money(free.remaining, 0, 'JPY')
  assert.equal(free.remainingReason, null)
})

test('structured billing remains authoritative over its legacy display string', () => {
  const result = calculateNodeCost(info('120', 'annual', 'AUD', { price: 'USD 9999 / year' }), now)
  money(result.monthly, 10, 'AUD')
  money(result.remaining, 120, 'AUD')
})

test('remaining value uses actual calendar periods across month ends, leap days and year boundaries', () => {
  const cases = [
    // March 31 minus one month clamps to February 28, not March 3.
    ['monthly', '31', '2026-03-31', '2026-03-01T00:00:00Z', 30],
    ['monthly', '31', '2024-03-31', '2024-03-01T00:00:00Z', 30],
    // February 29 minus one year clamps to February 28 of the previous year.
    ['annual', '366', '2024-02-29', '2024-02-01T00:00:00Z', 28],
    ['annual', '366', '2025-02-28', '2025-02-01T00:00:00Z', 27],
    ['quarterly', '92', '2026-01-31', '2025-12-31T00:00:00Z', 31],
    ['semiannual', '184', '2026-08-31', '2026-08-01T00:00:00Z', 30],
  ]
  for (const [cycle, amount, expires_at, at, expected] of cases) {
    const result = calculateNodeCost(info(amount, cycle, 'USD', { expires_at }), new Date(at))
    money(result.remaining, expected)
    assert.equal(result.remainingReason, null)
  }
})

test('remaining value retains sub-day precision and does not cap prepaid time to one payment', () => {
  money(calculateNodeCost(info('31', 'monthly', 'USD', { expires_at: '2026-03-31' }),
    new Date('2026-03-01T12:00:00Z')).remaining, 29.5)
  money(calculateNodeCost(info('365', 'annual', 'USD', { expires_at: '2028-01-01' }),
    new Date('2026-01-01T00:00:00Z')).remaining, 730)
})

test('renewed estimates retain the original calendar day after short months and leap years', () => {
  for (const enabled of [true, false]) {
    const auto_renewal = { enabled, anchor_date: '2026-01-31' }
    const february = info('28', 'monthly', 'USD', { expires_at: '2026-02-28', auto_renewal })
    money(calculateNodeCost(february, new Date('2026-02-01T00:00:00Z')).remaining, 27)
    const march = { ...february, expires_at: '2026-03-31' }
    money(calculateNodeCost(march, new Date('2026-03-01T00:00:00Z')).remaining, 28 * 30 / 31)
    const annual = info('365', 'annual', 'USD', {
      expires_at: '2025-02-28', auto_renewal: { enabled, anchor_date: '2024-02-29' },
    })
    money(calculateNodeCost(annual, new Date('2025-02-01T00:00:00Z')).remaining, 27)
  }
})

test('custom renewal estimates require a valid persisted calendar anchor', () => {
  const value = info('18', 'custom', 'EUR', {
    expires_at: '2027-07-31', auto_renewal: { enabled: true, anchor_date: '2026-01-31' },
  })
  value.billing.years = '1.5'
  money(calculateNodeCost(value, new Date('2026-01-31T00:00:00Z')).remaining, 18, 'EUR')
  for (const anchor_date of ['invalid', '2026-02-30', '2028-01-31', '2026-02-28', '2026-01-30']) {
    const invalid = { ...value, auto_renewal: { enabled: true, anchor_date } }
    assert.equal(calculateNodeCost(invalid, now).remainingReason, 'unsupported-cycle', anchor_date)
  }
  value.billing.years = '0.1'
  assert.equal(calculateNodeCost(value, now).remainingReason, 'unsupported-cycle')
})

test('expiry boundaries are UTC dates and expired prices have a known zero remaining value', () => {
  const value = info('120', 'annual', 'USD', { expires_at: '2026-10-23' })
  money(calculateNodeCost(value, now).remaining, 0)
  money(calculateNodeCost(value, new Date('2026-10-24T00:00:00Z')).remaining, 0)
  // This instant is the same UTC midnight even though its textual offset differs.
  money(calculateNodeCost(value, new Date('2026-10-22T17:00:00-07:00')).remaining, 0)
  const custom = { ...value, billing: { ...value.billing, cycle: 'custom', years: '1.5' } }
  money(calculateNodeCost(custom, now).remaining, 0)
})

test('valid four-digit years below 100 are not interpreted as twentieth-century dates', () => {
  money(calculateNodeCost(info('365', 'annual', 'USD', { expires_at: '0099-02-28' }),
    new Date('0099-02-01T00:00:00Z')).remaining, 27)
})

test('missing or malformed expiry never turns a paid server into zero remaining value', () => {
  const missing = calculateNodeCost(info('120', 'annual', 'USD', { expires_at: undefined }), now)
  money(missing.monthly, 10)
  assert.equal(missing.remaining, null)
  assert.equal(missing.remainingReason, 'missing-expiry')
  for (const expires_at of ['not-a-date', '2026-02-30', '2025-02-29', '2026-13-01', '2026-01-00', '2026-10-23T12:00:00Z']) {
    const result = calculateNodeCost(info('120', 'annual', 'USD', { expires_at }), now)
    money(result.monthly, 10)
    assert.equal(result.remaining, null, expires_at)
    assert.equal(result.remainingReason, 'invalid', expires_at)
  }
  const invalidClock = calculateNodeCost(info('120'), new Date('invalid'))
  money(invalidClock.monthly, 10)
  assert.equal(invalidClock.remaining, null)
  assert.equal(invalidClock.remainingReason, 'invalid')
})

test('invalid structured amounts and periods stay unknown rather than becoming free or a full payment', () => {
  for (const amount of ['-1', 'NaN', 'Infinity', '1e3', '  ', '1,000']) {
    const result = calculateNodeCost(info(amount), now)
    assert.equal(result.monthly, null, amount)
    assert.equal(result.remaining, null, amount)
  }
  for (const years of ['0', '-1', 'NaN', 'Infinity', '']) {
    const value = info('120', 'custom')
    value.billing.years = years
    const result = calculateNodeCost(value, now)
    assert.equal(result.monthly, null, years)
    assert.equal(result.remaining, null, years)
  }
  const unsupported = calculateNodeCost(info('120', 'weekly'), now)
  assert.equal(unsupported.monthly, null)
  assert.equal(unsupported.remaining, null)
})

test('cost summaries group original currencies and disclose partial coverage', () => {
  const inputs = [
    info('120', 'annual', 'USD'), info('24', 'annual', 'USD'),
    info('60', 'annual', 'EUR'), info('36', 'annual', 'AUD'),
    info('0', 'annual', 'JPY'), {}, { price: '$9 / month' },
  ]
  for (const [kind, multiplier] of [['monthly', 1], ['yearly', 12], ['remaining', 12]]) {
    const result = summarizeNodeCosts(inputs, kind, now)
    assert.equal(result.included, 5)
    assert.equal(result.total, 7)
    assert.equal(result.amounts.length, 4)
    const amounts = new Map(result.amounts.map(value => [value.currency, value]))
    money(amounts.get('USD'), 12 * multiplier)
    money(amounts.get('EUR'), 5 * multiplier, 'EUR')
    money(amounts.get('AUD'), 3 * multiplier, 'AUD')
    money(amounts.get('JPY'), 0, 'JPY')
    assert.equal(amounts.has('CNY'), false)
  }
})

test('unavailable remaining values are excluded without hiding known prices or free servers', () => {
  const custom = info('18', 'custom')
  custom.billing.years = '1.5'
  const inputs = [info('120'), info('60', 'annual', 'EUR', { expires_at: undefined }), custom, {}]
  const monthly = summarizeNodeCosts(inputs, 'monthly', now)
  assert.equal(monthly.included, 3)
  assert.equal(monthly.total, 4)
  money(monthly.amounts.find(value => value.currency === 'USD'), 11)
  const remaining = summarizeNodeCosts(inputs, 'remaining', now)
  assert.equal(remaining.included, 1)
  assert.equal(remaining.total, 4)
  assert.deepEqual(remaining.amounts, [{ amount: 120, currency: 'USD' }])
  assert.deepEqual(summarizeNodeCosts([{}, { price: 'USD 9 / year' }], 'remaining', now),
    { amounts: [], included: 0, total: 2 })
  assert.deepEqual(summarizeNodeCosts([], 'monthly', now), { amounts: [], included: 0, total: 0 })
})

test('display formatting keeps small positive estimates distinct from free and omits spurious decimals', () => {
  assert.equal(formatCostAmount({ amount: 45.68 / 12, currency: 'USD' }, 'en'), '3.81')
  assert.equal(formatCostAmount({ amount: 0, currency: 'JPY' }, 'en'), '0')
  assert.equal(formatCostAmount({ amount: 120, currency: 'USD' }, 'en'), '120')
  assert.equal(formatCostAmount({ amount: 1.005, currency: 'USD' }, 'en'), '1.01')
  assert.equal(formatCostAmount({ amount: 0.01, currency: 'USD' }, 'en'), '0.01')
  assert.equal(formatCostAmount({ amount: 0.0001, currency: 'USD' }, 'en'), '<0.01')
  assert.equal(formatCostAmount({ amount: 3.81, currency: 'EUR' }, 'de'), '3,81')
})

test('calculations never rewrite saved decimal strings, periods or caller dates', () => {
  const value = info('36.3600', 'custom', 'AUD', { price: 'old display text' })
  value.billing.years = '1.5000'
  const before = structuredClone(value)
  const clock = new Date(now)
  Object.freeze(value.billing)
  Object.freeze(value)
  calculateNodeCost(value, clock)
  summarizeNodeCosts([value], 'monthly', clock)
  summarizeNodeCosts([value], 'remaining', clock)
  assert.deepEqual(value, before)
  assert.equal(clock.getTime(), now.getTime())
})
