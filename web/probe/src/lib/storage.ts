import type { Node } from './api'

export interface StorageVolume {
  id: string
  name: string
  kind: 'system' | 'volume' | 'pool'
  total: number | null
  used: number | null
}

export interface StorageItem extends StorageVolume { label: string; percent: number | null }

const count = (value: unknown): number | null => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null

/** Only the explicit public projection is read. An empty projection is intentional. */
export function storageVolumes(node?: Pick<Node, 'storage' | 'host'>): StorageVolume[] {
  if (!node) return []
  if (node.storage !== undefined) return Array.isArray(node.storage) ? node.storage : []
  const h = node.host
  const valid = h && h.valid?.disk !== false
  return [{ id: 'legacy-root', name: '', kind: 'system', total: valid ? count(h.disk_total) : null, used: valid ? count(h.disk_used ?? 0) : null }]
}

export function storageItems(node: Pick<Node, 'storage' | 'host'> | undefined, translate: (key: string) => string): StorageItem[] {
  const ordinals = { system: 0, volume: 0, pool: 0 }
  return storageVolumes(node).map(volume => {
    const kind = volume.kind === 'system' || volume.kind === 'pool' ? volume.kind : 'volume'
    const ordinal = ++ordinals[kind]
    const total = count(volume.total), used = count(volume.used)
    return { ...volume, kind, total, used,
      label: volume.name?.trim() || `${translate(`storage.${kind}`)}${kind === 'system' ? '' : ` ${ordinal}`}`,
      percent: used !== null && total !== null && total > 0 ? Math.min(100, used / total * 100) : null,
    }
  })
}

export function storageBytes(value: number | null, dense = false): string {
  if (value === null || count(value) === null) return '—'
  const units = ['B', 'K', 'M', 'G', 'T', 'P', 'E']
  let unit = 0, size = value
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
  return `${Number(size.toFixed(size < 10 ? dense ? 1 : 2 : size < 100 ? 1 : 0))}${units[unit]}`
}

export function storagePercent(value: number | null): string {
  return value === null ? '—' : `${value.toFixed(1)}%`
}

export function storageCapacity(volume: Pick<StorageVolume, 'used' | 'total'>, dense = false): string {
  return `${storageBytes(volume.used, dense)}${dense ? '/' : ' / '}${storageBytes(volume.total, dense)}`
}

export function storageFullCapacity(volume: Pick<StorageVolume, 'used' | 'total'>): string {
  const bytes = (value: number | null) => {
    if (value === null || count(value) === null) return '—'
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
    let unit = 0, size = value
    while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
    return `${unit ? size.toFixed(2) : size} ${units[unit]}`
  }
  return `${bytes(volume.used)} / ${bytes(volume.total)}`
}
