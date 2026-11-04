import test from 'node:test'
import assert from 'node:assert/strict'
import { agentHTTPSOrigin, agentHTTPSDockerCommand, agentHTTPSCheckCommand, agentHTTPSFresh, suggestedAgentHTTPSURL } from '../src/lib/agent-https.ts'

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
  assert.match(agentHTTPSDockerCommand('https://panel.example.com:2087'), /enable-agent-https --agent-https-port 2087$/)
  assert.match(agentHTTPSDockerCommand('https://panel.example.com'), /--agent-https-port 443$/)
  assert.equal(agentHTTPSCheckCommand('https://panel.example.com:2096/'), "curl --fail --show-error --max-time 10 --proto '=https' -- 'https://panel.example.com:2096/api/agent/health'")
  assert.ok(!agentHTTPSCheckCommand('https://panel.example.com').includes('-k'))
})

test('freshness expires after suspension or time rollback', () => {
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 2000), true)
  assert.equal(agentHTTPSFresh(1000, 1000, 31000, 2000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 31000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 999, 2000), false)
  assert.equal(agentHTTPSFresh(1000, 1000, 2000, 999), false)
})
