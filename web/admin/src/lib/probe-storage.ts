export type ProbeStorageKind = 'system' | 'volume' | 'pool'
export interface ProbeStorageItem { mount: string; name: string }
export interface ProbeStorage { mode: 'auto' | 'manual'; items: ProbeStorageItem[] }
export interface ProbeStorageCandidate {
  mount: string
  device: string
  type: string
  kind: ProbeStorageKind
  total: number | null
  used: number | null
  recommended: boolean
}

// Clone the saved selection once on load. Candidate refreshes must never remove
// an unavailable mount or overwrite a name/order the administrator is editing.
export function storageDraft(value?: ProbeStorage | null): ProbeStorage {
  return value?.mode === 'manual'
    ? { mode: 'manual', items: value.items.map(item => ({ ...item })) }
    : { mode: 'auto', items: [] }
}

export function recommendedStorage(candidates: ProbeStorageCandidate[]): ProbeStorageItem[] {
  const seen = new Set<string>()
  return candidates.filter(candidate => {
    if (!candidate.recommended || seen.has(candidate.mount)) return false
    seen.add(candidate.mount)
    return true
  }).map(candidate => ({ mount: candidate.mount, name: '' }))
}

export function moveStorage(items: ProbeStorageItem[], index: number, target: number): ProbeStorageItem[] {
  if (index < 0 || index >= items.length || target < 0 || target >= items.length) return items
  const next = [...items]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}
