export interface CPUInfo {
  /** Legacy field: logical CPUs, not physical cores. */
  cpu_cores?: number
  cpu_physical_cores?: number
  cpu_sockets?: number
  virtualization_role?: string
}

const logicalCPU: Record<string, string> = {
  en: 'logical CPU', 'zh-CN': '逻辑 CPU', 'zh-TW': '邏輯 CPU',
  ja: '論理 CPU', ko: '논리 CPU', ru: 'логич. CPU',
}

// A hypervisor name alone does not distinguish a guest from its host. Older
// agents therefore keep an explicit logical-CPU fallback instead of guessed C/T.
export function cpuSpec(info?: CPUInfo, locale = 'en'): string {
  const positive = (n: number | undefined): n is number => Number.isSafeInteger(n) && n! > 0
  const threads = info?.cpu_cores
  if (!positive(threads)) return '—'
  if (info?.virtualization_role === 'guest') return `${threads} vCPU`
  const cores = info?.cpu_physical_cores, sockets = info?.cpu_sockets
  if ((info?.virtualization_role === 'none' || info?.virtualization_role === 'host')
      && positive(cores) && positive(sockets) && sockets <= cores && cores <= threads) {
    return `${sockets > 1 ? `${sockets}S · ` : ''}${cores}C / ${threads}T`
  }
  const label = logicalCPU[locale] ?? logicalCPU[locale.split('-')[0]] ?? logicalCPU.en
  return `${threads} ${label}`
}
