import type { Line, LineChangeForward } from './api'

export type LineRolloutDraft = Partial<Line> & Pick<Line, 'name' | 'protocol' | 'backend' | 'target_inbound_id' | 'groups'>
export interface LineRolloutNode { node_id: number; node_name: string; before: LineChangeForward[]; after: LineChangeForward[] }
export interface LineRolloutTarget {
  id: string; node_id: number; node_name: string; phase: 'expand' | 'cleanup'; order: number; attempt: number
  status: 'pending' | 'released' | 'applied'; expected: LineChangeForward[]
  delivery_id: string; config_revision: string; state_revision: string
  issued_at?: string; confirmed_at?: string; observed_at?: string; last_receipt_at?: string; deadline_at?: string
  receipt_applied: boolean; fresh: boolean; fresh_for_ms: number
}
export interface LineRollout {
  id: string; cursor: number; line_id: number; scope_id: string; version: number
  kind: 'create' | 'edit' | 'delete'; status: 'running' | 'paused' | 'completed' | 'compensated'; stage: 'expand' | 'cleanup'
  actor: string; before?: Line; after?: Line; error_code?: string; compensates?: string
  created_at: string; updated_at: string; published_at?: string; targets: LineRolloutTarget[]; nodes: LineRolloutNode[]
}
export interface LineRolloutPreview { preview_revision: string; draft: Line; remove: boolean; targets: LineRolloutTarget[]; nodes: LineRolloutNode[] }
export interface LineRolloutPage { items: LineRollout[]; next_before?: number }

// A review is tied to exactly the draft, operation and deadline shown to the user.
export function rolloutReviewKey(line: LineRolloutDraft, remove: boolean, seconds: number): string {
  return JSON.stringify({ line, remove, timeout_seconds: seconds })
}
export function rolloutHistoryURL(lineID?: number, before?: number): string {
  const q = new URLSearchParams({ limit: '20' })
  if (lineID) q.set('line_id', String(lineID))
  if (before) q.set('before', String(before))
  return `/api/admin/line-rollouts?${q}`
}
