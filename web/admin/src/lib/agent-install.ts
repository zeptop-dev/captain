// Both command variants and the manual configuration must use the server's
// resolved endpoint. Browser-origin fallback only supports older Captain APIs.
export function agentInstallCommands(code: string, origin: string, mode = 'full', downloadSource = 'github', installSystemPackages?: boolean) {
  const base = origin.replace(/\/+$/, '')
  const quote = (value: string) => `'${value.replaceAll("'", "'\"'\"'")}'`
  const tls = base.toLowerCase().startsWith('https://') ? " --proto '=https' --proto-redir '=https'" : ''
  const redirects = downloadSource === 'captain' ? ' --max-redirs 0' : ''
  // A failed first download must never become a successful empty shell. Keep
  // the script private and clean it up on success, download failure and signals.
  const downloadedScript = `script=$(mktemp) || exit 1; trap 'rm -f "$script"' EXIT; trap 'exit 1' HUP INT TERM; curl -q -fsSL --globoff${tls}${redirects} -o "$script" -- ${quote(`${base}/api/agent/install.sh?pair=${encodeURIComponent(code)}`)} && sh "$script"`
  // Preserve legacy installers for the unchanged source default. An explicit
  // override is checked by Captain's wrapper before the installer is executed.
  const packageArg = mode === 'full' && installSystemPackages !== undefined && installSystemPackages !== (downloadSource !== 'captain')
    ? ` --system-packages ${installSystemPackages ? 'install' : 'skip'}` : ''
  const install = `(${downloadedScript}${packageArg})`
  const docker = `(${downloadedScript} --mode docker --web-upgrade)`
  const snippet = `${mode === 'monitor' ? 'node_mode: monitor\n' : ''}panel:\n  driver: captain\n  captain:\n    url: ${JSON.stringify(base)}\n    pair_code: ${JSON.stringify(code)}${downloadSource === 'captain' ? '\n    download_source: captain' : ''}`
  return { install, docker, snippet }
}
