import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { agentHTTPSOrigin, agentHTTPSDockerCommand, agentHTTPSCheckCommand, agentHTTPSFresh, agentHTTPSCheckFresh, suggestedAgentHTTPSURL } from '../src/lib/agent-https.ts'

test('HTTPS origin validation rejects credentials, paths and malformed ports', () => {
  for (const input of ['http://panel.example.com', 'https://user:pass@panel.example.com', 'https://panel.example.com/path', 'https://panel.example.com/?x=1', 'https://panel.example.com/#x', 'https://panel.example.com:0', 'https://panel.example.com:70000', "https://panel.example.com:2096; touch /tmp/example"]) {
    assert.equal(agentHTTPSOrigin(input), null, input)
    assert.equal(agentHTTPSDockerCommand(input), null, input)
    assert.equal(agentHTTPSCheckCommand(input), null, input)
  }
  assert.equal(agentHTTPSOrigin('https://[2001:db8::1]:2096').port, '2096')
  assert.equal(suggestedAgentHTTPSURL('https://panel.example.com/admin'), 'https://panel.example.com:2096')
  assert.equal(suggestedAgentHTTPSURL('https://[2001:db8::1]'), 'https://[2001:db8::1]:2096')
})

test('host commands use the saved port and target only the dedicated health route', () => {
  const installerPath = agentHTTPSDockerCommand('https://panel.example.com:2096').match(/https:\/\/raw\.githubusercontent\.com\/zeptop-dev\/captain\/master\/([^ ]+)/)?.[1]
  assert.ok(installerPath && existsSync(new URL(`../../../${installerPath}`, import.meta.url)), 'command must download an installer that exists in this repository')
  assert.match(agentHTTPSDockerCommand('https://panel.example.com:2087'), /enable-agent-https --agent-https-port 2087 --agent-https-url 'https:\/\/panel.example.com:2087' --agent-firewall auto$/)
  for (const port of ['', ':443', ':1023', ':8080']) {
    assert.equal(agentHTTPSDockerCommand(`https://panel.example.com${port}`), null)
    assert.equal(agentHTTPSDockerCommand(`https://panel.example.com${port}`, true), null)
    assert.ok(agentHTTPSCheckCommand(`https://panel.example.com${port}`), 'node health check still supports HTTPS origins on reserved ports')
  }
  for (const port of [1024, 2096, 65535]) assert.match(agentHTTPSDockerCommand(`https://panel.example.com:${port}`), /^curl -q /)
  assert.equal(agentHTTPSCheckCommand('https://panel.example.com:2096/'), "curl -q --fail --show-error --globoff --max-time 10 --proto '=https' -- 'https://panel.example.com:2096/api/agent/health'")
  assert.ok(!agentHTTPSCheckCommand('https://panel.example.com').includes('-k'))
})

test('freshness expires after suspension or time rollback', () => {
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 2000), true)
  assert.equal(agentHTTPSFresh(1000, 1000, 31000, 2000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 31000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 999, 2000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 999), false)
})

test('read-only host command cannot modify firewalls and result identity is configuration-bound', () => {
  const command = agentHTTPSDockerCommand('https://[2001:db8::1]:2096', true)
  assert.match(command, /check-agent-https --agent-https-port 2096 --agent-https-url 'https:\/\/\[2001:db8::1\]:2096'$/)
  assert.match(command, /^curl -q /)
  assert.doesNotMatch(command, /--agent-firewall|enable-agent-https/)
  const settings = { enabled: true, url: 'https://example.com:2096', revision: 'r1' }
  const observation = { result: { perspective: 'panel', url: settings.url, revision: 'r1', status: 'ok' }, start: { wall: 1000, mono: 1000 } }
  assert.equal(agentHTTPSCheckFresh(observation, settings, 2000, 2000), true)
  for (const update of [{ revision: 'r2' }, { url: 'https://example.com:2087' }, { enabled: false }]) assert.equal(agentHTTPSCheckFresh(observation, { ...settings, ...update }, 2000, 2000), false)
  assert.equal(agentHTTPSCheckFresh(observation, settings, 31000, 2000), false)
  assert.equal(agentHTTPSCheckFresh(observation, settings, 2000, 31000), false)
  assert.equal(agentHTTPSCheckFresh(observation, settings, 999, 2000), false)
})
