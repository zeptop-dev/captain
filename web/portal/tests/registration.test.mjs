import test from 'node:test'
import assert from 'node:assert/strict'
import { registrationErrorKey, registrationErrorField } from '../src/lib/registration.ts'

test('403 is not treated as closed registration; stable codes identify the actual problem', () => {
  assert.equal(registrationErrorKey('email_domain', 'not allowed'), 'auth.domainDenied')
  assert.equal(registrationErrorField('email_domain'), 'Email')
  assert.equal(registrationErrorKey('captcha_failed', 'forbidden'), 'auth.captchaFailed')
  assert.equal(registrationErrorKey(undefined, 'forbidden'), undefined)
  assert.equal(registrationErrorKey(undefined, 'registration is closed'), 'auth.closed')
  assert.equal(registrationErrorKey('legal_changed', ''), 'auth.legalChanged')
  assert.equal(registrationErrorField('code_invalid'), 'Code')
})
