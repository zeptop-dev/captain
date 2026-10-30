import type { ForwardAddressMode, Line, LineDependencyView, LineEndpoint, LineMember, LinePreview } from './api'

export interface PathStep {
  id: string
  node_id?: number
  ingress_id?: number | null
  address_mode?: ForwardAddressMode
  path_id?: number
  path_revision?: number
  line_id?: number
  line_version?: number
  include_landing?: boolean
}
export interface PathHop { key: string; node_id: number; ingress_id?: number | null; address_mode?: ForwardAddressMode }
export interface ForwardPath { id: number; name: string; version: number; steps: PathStep[]; hops: PathHop[]; depth: number }
export interface PathUse { kind: 'path' | 'route'; id: number; version: number; name: string }
export interface PathBranch { id: string; path_id: number; path_revision: number; weight: number; ports: number[] }
export interface PathRoute {
  id: number; version: number; name: string; entry: LineMember
  target_inbound_id: number; target_address_mode: ForwardAddressMode
  strategy: 'failover' | 'roundrobin'; branches: PathBranch[]
  preview_revision?: string; change_id?: string
}
export interface PathRoutePreview extends LineDependencyView {
  revision: string; draft: PathRoute; changes: LinePreview['changes']; endpoints: LineEndpoint[]
}

export function pathInput(path: Pick<ForwardPath, 'name' | 'version' | 'steps'>) {
  return { name: path.name.trim(), version: path.version, steps: path.steps.map(step => {
    if (step.path_id !== undefined) return { id: step.id, path_id: step.path_id, path_revision: step.path_revision }
    if (step.line_id !== undefined) return { id: step.id, line_id: step.line_id, line_version: step.line_version, include_landing: !!step.include_landing }
    return { id: step.id, node_id: step.node_id || 0, ingress_id: step.ingress_id || null, address_mode: step.address_mode || 'default' }
  }) }
}
export function routeInput(route: PathRoute): PathRoute {
  return {
    id: route.id, version: route.version, name: route.name.trim(),
    entry: { id: route.entry.id, node_id: route.entry.node_id, port: route.entry.port, ingress_id: route.entry.ingress_id || null, address_mode: 'default', weight: 1 },
    target_inbound_id: route.target_inbound_id, target_address_mode: route.target_address_mode || 'default', strategy: route.strategy,
    branches: route.branches.map(branch => ({ id: branch.id, path_id: branch.path_id, path_revision: branch.path_revision, weight: branch.weight, ports: [...branch.ports] })),
  }
}
export const validPathPort = (value: number) => Number.isInteger(value) && value > 0 && value <= 65535
export const validPathRevision = (value: number | undefined) => Number.isInteger(value) && (value ?? 0) > 0
export function pathStepsComplete(steps: PathStep[]) {
  return steps.length > 0 && steps.length <= 8 && steps.every(step => step.path_id !== undefined
    ? step.path_id > 0 && validPathRevision(step.path_revision)
    : step.line_id !== undefined ? step.line_id > 0 && validPathRevision(step.line_version) : (step.node_id ?? 0) > 0)
}
export function routePortsComplete(branches: PathBranch[], paths: (ForwardPath | undefined)[]) {
  return branches.length > 0 && branches.length <= 8 && branches.every((branch, index) => {
    const path = paths[index]
    return path && path.id === branch.path_id && path.version === branch.path_revision && path.hops.length > 0
      && branch.ports.length === path.hops.length && branch.ports.every(validPathPort)
      && Number.isInteger(branch.weight) && branch.weight >= 1 && branch.weight <= 100
  })
}
export function linearSourceLine(line: Line & { path_route?: unknown }) {
  return !line.path_route && line.protocol === 'tcp' && line.backend === 'relay' && line.groups.length > 0 && line.groups.every(group => group.members.length === 1)
}
export function pathSourceLineOptions(lines: Pick<Line, 'id' | 'name'>[], sources: Pick<Line, 'id' | 'name'>[], step: PathStep, pinnedLabel: string) {
  const options: { value: string; label: string; disabled?: boolean }[] = sources.map(line => ({ value: String(line.id), label: line.name }))
  // A captured version remains valid after the live source stops being reusable.
  // Keep it visible in this row without offering it as a new source elsewhere.
  if (step.line_id && validPathRevision(step.line_version) && !sources.some(line => line.id === step.line_id)) {
    const name = lines.find(line => line.id === step.line_id)?.name || `#${step.line_id}`
    options.push({ value: String(step.line_id), label: `${name} · ${pinnedLabel}`, disabled: true })
  }
  return options
}
export function selectPathSourceLine(step: PathStep, id: number, sources: Pick<Line, 'id' | 'version'>[]): Partial<PathStep> {
  if (id === step.line_id) return {}
  if (id === 0) return { line_id: 0, line_version: 0 }
  const source = sources.find(line => line.id === id)
  return source ? { line_id: source.id, line_version: source.version } : {}
}
export function movePathItem<T>(items: T[], from: number, to: number): T[] {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length) return items
  const next = [...items]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}
