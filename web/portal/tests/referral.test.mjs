import assert from 'node:assert/strict'
import test from 'node:test'
import { authDestination, createReferralWriter, referralCode, withReferral } from '../src/lib/referral.ts'
import { loginReturn } from '../src/lib/login-return.ts'
import { readFileSync } from 'node:fs'

test('invitation survives protected entry, login/register and forgot navigation without widening return URLs', () => {
  const search = '?ref=A%26B&next=https://example.com'
  const code = referralCode(search)
  assert.equal(withReferral('/login', code), '/login?ref=A%26B')
  const register = authDestination('/register', loginReturn(search), code)
  assert.equal(register, '/register?next=%2Fportal%2F&ref=A%26B')
  const params = register.slice(register.indexOf('?'))
  assert.equal(referralCode(params), code)
  assert.equal(authDestination('/forgot', loginReturn(params), code), '/forgot?next=%2Fportal%2F&ref=A%26B')
  for (const next of ['//example.com', 'https://example.com', '/portal/plans', '/portal/authorize-evil']) {
    assert.equal(loginReturn('?next=' + encodeURIComponent(next)), '/portal/')
  }
  assert.equal(loginReturn('?next=%2Fportal%2Fauthorize%3Fstate%3Dtest'), '/portal/authorize?state=test')
})

test('OAuth shares the pending bootstrap write and a changed invite waits for it', async () => {
  const calls = []
  const pending = []
  const write = createReferralWriter(code => new Promise(resolve => { calls.push(code); pending.push(resolve) }))
  const initial = write('FIRST')
  assert.equal(write(' FIRST '), initial)
  assert.equal(write(''), initial)
  const edited = write('SECOND')
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(calls, ['FIRST'])
  let ready = false
  void edited.then(() => { ready = true })
  pending.shift()()
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(calls, ['FIRST', 'SECOND'])
  assert.equal(ready, false)
  pending.shift()()
  await edited
  assert.equal(ready, true)
  assert.equal(write('SECOND'), edited)
})

test('failed bootstrap can retry and does not block a corrected invitation', async () => {
  let count = 0
  const write = createReferralWriter(async () => { if (++count === 1) throw Error('network failure') })
  await assert.rejects(write('FIRST'))
  await write('FIRST')
  await write('SECOND')
  await write('')
  assert.equal(count, 3)
})

test('homepage and referral notices exist in all six portal languages', () => {
  for (const language of ['en', 'zh-CN', 'zh-TW', 'ja', 'ko', 'ru']) {
    const auth = JSON.parse(readFileSync(new URL('../src/i18n/' + language + '.json', import.meta.url))).auth
    for (const key of ['brandHome', 'brandPortal', 'inviteSaveFailed']) assert.ok(auth[key]?.trim(), language + ':' + key)
  }
})
