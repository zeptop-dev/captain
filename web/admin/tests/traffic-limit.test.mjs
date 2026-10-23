import assert from 'node:assert/strict'
import test from 'node:test'
import { trafficLimitBytes, trafficLimitInput } from '../src/lib/traffic-limit.ts'

test('decimal GB and TB allowances reopen as entered instead of whole GB', () => {
  for (const unit of ['GB', 'TB']) {
    const bytes = trafficLimitBytes('1.10', unit)
    assert.deepEqual(trafficLimitInput(bytes), { bytes, amount: 1.1, unit })
  }
  assert.equal(trafficLimitBytes(1.1, 'GB'), 1181116006)
  assert.equal(trafficLimitBytes(1.1, 'TB'), 1209462790554)
})

test('old allowances and unit changes preserve every stored byte', () => {
  const allowances = [0, 1, 1048576, 1181116006, 1126 * 2 ** 30, 1209462790554, 1234567890123, Number.MAX_SAFE_INTEGER]
  for (const bytes of allowances) {
    let input = trafficLimitInput(bytes)
    for (const unit of ['MB', 'GB', 'TB', 'GB', 'TB', 'MB']) {
      input = trafficLimitInput(input.bytes, unit)
      assert.equal(input.bytes, bytes)
      assert.equal(trafficLimitBytes(input.amount, input.unit), bytes)
    }
  }
})

test('blank and zero remain unlimited in every unit', () => {
  for (const unit of ['MB', 'GB', 'TB']) {
    assert.equal(trafficLimitBytes('', unit), 0)
    assert.equal(trafficLimitBytes('0.00', unit), 0)
    assert.deepEqual(trafficLimitInput(0, unit), { bytes: 0, amount: 0, unit })
  }
})
