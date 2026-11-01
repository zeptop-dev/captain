// Both command variants and the manual configuration must use the server's
// resolved endpoint. Browser-origin fallback only supports older Captain APIs.
export function agentInstallCommands(code: string, origin: string, mode = 'full', downloadSource = 'github') {
  const base = origin.replace(/\/+$/, '')
  const quote = (value: string) => `'${value.replaceAll("'", "'\"'\"'")}'`
  const tls = base.toLowerCase().startsWith('https://') ? " --proto '=https' --proto-redir '=https'" : ''
  const redirects = downloadSource === 'captain' ? ' --max-redirs 0' : ''
  const install = `curl -fsSL --globoff${tls}${redirects} -- ${quote(`${base}/api/agent/install.sh?pair=${encodeURIComponent(code)}`)} | sh`
  const docker = `${install} -s -- --mode docker --web-upgrade`
  const snippet = `${mode === 'monitor' ? 'node_mode: monitor\n' : ''}panel:\n  driver: captain\n  captain:\n    url: ${JSON.stringify(base)}\n    pair_code: ${JSON.stringify(code)}${downloadSource === 'captain' ? '\n    download_source: captain' : ''}`
  return { install, docker, snippet }
}
