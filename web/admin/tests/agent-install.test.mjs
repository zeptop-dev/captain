import assert from 'node:assert/strict'
import { execFileSync, spawnSync, spawn } from 'node:child_process'
import { mkdtempSync, writeFileSync, readdirSync, readFileSync, rmSync, mkdirSync } from 'node:fs'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import test from 'node:test'
import { agentInstallCommands } from '../src/lib/agent-install.ts'

test('install commands and manual config share the backend endpoint and download choice', () => {
  const result = agentInstallCommands('ABCD-EFGH', 'https://agent.example.com:18443/', 'monitor', 'captain')
  assert.match(result.install, /--max-redirs 0 -o \"\$script\" -- 'https:\/\/agent.example.com:18443\/api\/agent\/install.sh\?pair=ABCD-EFGH' && sh "\$script"\)$/)
  assert.match(result.docker, /&& sh "\$script" --mode docker --web-upgrade\)$/)
  assert.match(result.snippet, /^node_mode: monitor\n/)
  assert.match(result.snippet, /url: "https:\/\/agent.example.com:18443"/)
  assert.match(result.snippet, /download_source: captain$/)
  assert.doesNotMatch(agentInstallCommands('CODE', 'https://example.com').snippet, /download_source/)
  assert.doesNotMatch(result.install, /api\/agent\/health/, 'primary listener supports older health routes')
})

test('legacy endpoint strings cannot execute shell substitutions or alter YAML lines', () => {
  const endpoint = "https://example.com/'$(printf injected)`printf injected`"
  const pair = 'CODE\nnew_key: value'
  const result = agentInstallCommands(pair, endpoint)
  const commandArgument = result.install.split(' -- ')[1].split(' && sh "$script"')[0]
  assert.equal(execFileSync('sh', ['-c', `printf '%s' ${commandArgument}`], { encoding: 'utf8' }), `${endpoint}/api/agent/install.sh?pair=${encodeURIComponent(pair)}`)
  assert.ok(result.snippet.includes(`url: ${JSON.stringify(endpoint)}`))
  assert.ok(result.snippet.includes(`pair_code: ${JSON.stringify(pair)}`))
})

test('download failure never runs a partial script, script errors propagate, and temporary files are removed', () => {
  const root = mkdtempSync(join(tmpdir(), 'captain-install-command-')), bin = join(root, 'bin'), temp = join(root, 'tmp')
  mkdirSync(bin); mkdirSync(temp)
  writeFileSync(join(bin, 'curl'), '#!/bin/sh\nwhile [ "$#" -gt 0 ]; do if [ "$1" = "-o" ]; then shift; cp "$FIXTURE_SCRIPT" "$1"; fi; shift; done\nexit "$CURL_STATUS"\n', { mode: 0o700 })
  const source = join(root, 'installer'), called = join(root, 'called')
  writeFileSync(source, '#!/bin/sh\nprintf "%s\\n" "$@" > "$CALLED_FILE"\nexit "$INSTALL_STATUS"\n')
  const env = { ...process.env, PATH: `${bin}:${process.env.PATH}`, TMPDIR: temp, FIXTURE_SCRIPT: source, CALLED_FILE: called, CURL_STATUS: '22', INSTALL_STATUS: '0' }
  try {
    for (const source of ['captain', 'github']) {
      const commands = agentInstallCommands('EXAMPLE', 'https://example.com', 'full', source)
      assert.equal(spawnSync('sh', ['-c', commands.install], { env }).status, 22)
      assert.equal(readdirSync(root).includes('called'), false)
      assert.deepEqual(readdirSync(temp), [])
      assert.equal(spawnSync('sh', ['-c', commands.docker], { env: { ...env, CURL_STATUS: '0', INSTALL_STATUS: '7' } }).status, 7)
      assert.equal(readFileSync(called, 'utf8'), '--mode\ndocker\n--web-upgrade\n')
      assert.deepEqual(readdirSync(temp), [])
      rmSync(called)
    }
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test('a real curl download executes the saved installer with Docker arguments', async () => {
  const root = mkdtempSync(join(tmpdir(), 'captain-install-http-')), marker = join(root, 'called'), temp = join(root, 'tmp')
  mkdirSync(temp)
  const requests = []
  const server = createServer((request, response) => {
    requests.push(request.url)
    response.end('#!/bin/sh\nprintf "%s\\n" "$@" > "$CALLED_FILE"\n')
  })
  try {
    server.listen(0, '127.0.0.1'); await once(server, 'listening')
    writeFileSync(join(root, '.curlrc'), `url = http://127.0.0.1:${server.address().port}/unexpected\n`)
    const command = agentInstallCommands('EXAMPLE', `http://127.0.0.1:${server.address().port}`, 'full', 'captain').docker
    const child = spawn('sh', ['-c', command], { env: { ...process.env, TMPDIR: temp, CURL_HOME: root, CALLED_FILE: marker }, stdio: 'pipe' })
    const errors = []; child.stderr.on('data', data => errors.push(data.toString()))
    const [code] = await once(child, 'exit')
    assert.equal(code, 0, errors.join(''))
    assert.deepEqual(requests, ['/api/agent/install.sh?pair=EXAMPLE'])
    assert.equal(readFileSync(marker, 'utf8'), '--mode\ndocker\n--web-upgrade\n')
    assert.deepEqual(readdirSync(temp), [])
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); rmSync(root, { recursive: true, force: true }) }
})

test('native dependency choice is independent of program source, Docker and monitor installs', () => {
  for (const source of ['github', 'captain']) {
    const enabledByDefault = source === 'github'
    const defaults = agentInstallCommands('CODE', 'https://example.com', 'full', source, enabledByDefault)
    assert.doesNotMatch(defaults.install, /--system-packages/, 'unchanged defaults remain compatible with old installers')
    const changed = agentInstallCommands('CODE', 'https://example.com', 'full', source, !enabledByDefault)
    assert.match(changed.install, new RegExp(`--system-packages ${source === 'captain' ? 'install' : 'skip'}\\)$`))
    assert.equal(changed.docker, defaults.docker)
    assert.equal(changed.snippet, defaults.snippet, 'installation option is not a saved agent setting')
    assert.doesNotMatch(agentInstallCommands('CODE', 'https://example.com', 'monitor', source, !enabledByDefault).install, /--system-packages/)
  }
})
