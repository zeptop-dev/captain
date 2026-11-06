import assert from 'node:assert/strict'
import test from 'node:test'
import { locationIPDraft, locationIPSettings, locationIPChanged, locationIPError, validLocationIP, locationIPSourceKey } from '../src/lib/location-ip.ts'

test('location source defaults preserve existing auto behavior without entry fallback', () => {
  assert.deepEqual(locationIPDraft(), { mode: 'auto', manual_ip: '', allow_entry_fallback: false })
  assert.equal(locationIPChanged(locationIPDraft(), undefined), false)
  assert.equal(locationIPError(locationIPDraft()), null)
  const saved = { mode: 'manual', manual_ip: '198.51.100.20', allow_entry_fallback: true }
  const draft = locationIPDraft(saved)
  draft.manual_ip = '2001:db8::20'
  assert.equal(saved.manual_ip, '198.51.100.20')
  assert.equal(locationIPChanged(draft, saved), true)
  assert.deepEqual(locationIPSettings({ ...draft, mode: 'entry' }), { mode: 'entry', manual_ip: '2001:db8::20', allow_entry_fallback: true })
  assert.equal(locationIPChanged({ ...saved, manual_ip: ' 198.51.100.20 ' }, saved), false)
})

test('manual location IP requires a strict public address literal', () => {
  for (const ip of ['198.51.100.20', '203.0.113.30', '192.0.2.10', '2001:db8::20', '2001:db8:0:0:0:0:0:20', ' 2001:db8::20 ']) {
    assert.equal(validLocationIP(ip), true, ip)
    assert.equal(locationIPError({ mode: 'manual', manual_ip: ip }), null, ip)
  }
  for (const ip of ['', 'example.com', 'https://198.51.100.20', '198.51.100.20:443', '[2001:db8::20]:443', '[2001:db8::20]', '198.051.100.20', '192.0.2.256', '0xc000020a', '3221225994', '127.1', '10.10.0.2', '172.16.0.1', '192.168.0.1', '100.64.0.1', '100.127.255.255', '169.254.0.1', '127.0.0.1', '0.0.0.0', '255.255.255.255', '224.0.0.1', '::', '::1', '::ffff:198.51.100.20', '::ffff:c633:6414', 'fc00::1', 'fd00::1', 'fe80::1', 'febf::1', 'ff02::1', '2001:db8::20%eth0', '2001:::20', '2001:db8::20/64', '2001:db8::20@other.example.com']) {
    assert.equal(validLocationIP(ip), false, ip)
    assert.equal(locationIPError({ mode: 'manual', manual_ip: ip }), 'invalid', ip)
  }
})

test('other modes retain a manual draft without using its validation or inventing provenance', () => {
  assert.equal(locationIPError({ mode: 'auto', manual_ip: 'unfinished' }), null)
  assert.equal(locationIPError({ mode: 'entry', manual_ip: '' }), null)
  assert.deepEqual(locationIPSettings({ mode: 'auto', manual_ip: 'unfinished' }), { mode: 'auto', manual_ip: '', allow_entry_fallback: false })
  for (const source of ['manual', 'entry_override', 'primary', 'fallback', 'entry_fallback']) assert.equal(locationIPSourceKey(source), `locationIP.sources.${source}`)
  assert.equal(locationIPSourceKey(), 'locationIP.sources.unknown')
  assert.equal(locationIPSourceKey('future_source'), 'locationIP.sources.unknown')
})
