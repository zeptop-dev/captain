import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import test from 'node:test'
import { agentInstallCommands } from '../src/lib/agent-install.ts'

test('install commands and manual config share the backend endpoint and download choice', () => {
  const result = agentInstallCommands('ABCD-EFGH', 'https://agent.example.com:18443/', 'monitor', 'captain')
  assert.equal(result.install, "curl -fsSL --globoff --proto '=https' --proto-redir '=https' --max-redirs 0 -- 'https://agent.example.com:18443/api/agent/install.sh?pair=ABCD-EFGH' | sh")
  assert.equal(result.docker, `${result.install} -s -- --mode docker --web-upgrade`)
  assert.match(result.snippet, /^node_mode: monitor\n/)
  assert.match(result.snippet, /url: "https:\/\/agent.example.com:18443"/)
  assert.match(result.snippet, /download_source: captain$/)
  assert.doesNotMatch(agentInstallCommands('CODE', 'https://example.com').snippet, /download_source/)
})

test('legacy endpoint strings cannot execute shell substitutions or alter YAML lines', () => {
  const endpoint = "https://example.com/'$(printf injected)`printf injected`"
  const pair = 'CODE\nnew_key: value'
  const result = agentInstallCommands(pair, endpoint)
  const commandArgument = result.install.split(' -- ')[1].slice(0, -' | sh'.length)
  assert.equal(execFileSync('sh', ['-c', `printf '%s' ${commandArgument}`], { encoding: 'utf8' }), `${endpoint}/api/agent/install.sh?pair=${encodeURIComponent(pair)}`)
  assert.ok(result.snippet.includes(`url: ${JSON.stringify(endpoint)}`))
  assert.ok(result.snippet.includes(`pair_code: ${JSON.stringify(pair)}`))
})
