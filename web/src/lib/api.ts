// Thin client for the gatekeepd REST API. The bearer token lives in
// localStorage; every call goes through the same origin (proxied in dev).

export type Level = 0 | 1 | 2 | 3

export type Status =
  | 'pending' | 'approved' | 'rejected' | 'expired' | 'cancelled'
  | 'running' | 'succeeded' | 'failed' | 'denied'

export interface Me {
  name: string
  kind: 'human' | 'agent'
  role?: 'viewer' | 'operator' | 'admin'
  targets?: string[]
}

export interface Target {
  name: string
  transport: 'local' | 'ssh'
  host?: string
  user?: string
  labels?: string[]
  description?: string
}

export interface GkRequest {
  id: string
  created_at: number
  requester: string
  requester_kind: 'human' | 'agent'
  target: string
  argv: string[]
  reason: string
  level: Level
  rule: string
  status: Status
  approver?: string
  decided_at?: number
  decision_note?: string
  expires_at?: number
  started_at?: number
  finished_at?: number
  exit_code: number
  stdout?: string
  stderr?: string
  truncated?: boolean
}

export interface AuditEvent {
  seq: number
  ts: number
  actor: string
  actor_kind: string
  action: string
  request_id?: string
  target?: string
  detail?: Record<string, unknown>
  remote?: string
  prev_hash: string
  hash: string
}

export interface Check {
  argv: string[]
  target: string
  level: Level
  level_name: string
  rule: string
  custom?: boolean
  outcome: 'allow' | 'need_approval' | 'deny'
}

export interface Verify {
  ok: boolean
  count: number
  broken_at?: number
  reason?: string
}

export interface CustomRule {
  id?: number
  name: string
  program: string
  args?: string
  not_args?: string
  level: Level
  target?: string
  priority: number
  enabled: boolean
  note?: string
  created_by?: string
  created_at?: number
  updated_by?: string
  updated_at?: number
}

export interface BuiltinRule {
  name: string
  program: string
  args?: string
  not_args?: string
  level: Level
  locked?: boolean
}

export interface Rules {
  custom: CustomRule[]
  builtin: BuiltinRule[]
  default_level: Level
  can_edit: boolean
}

export interface Decision {
  level: Level
  rule: string
  custom?: boolean
}

export interface RuleTest {
  command: string
  target: string
  before: Decision
  after: Decision
  error?: string
}

const TOKEN_KEY = 'gatekeep.token'

export const token = {
  get: () => (typeof localStorage === 'undefined' ? null : localStorage.getItem(TOKEN_KEY)),
  set: (t: string) => localStorage.setItem(TOKEN_KEY, t),
  clear: () => localStorage.removeItem(TOKEN_KEY),
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function call<T>(method: string, path: string, body?: unknown, tok = token.get()): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(tok ? { Authorization: `Bearer ${tok}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok && !(res.status === 403 && data?.id)) {
    throw new ApiError(res.status, data?.error ?? res.statusText)
  }
  return data as T
}

export const api = {
  me: (tok?: string) => call<Me>('GET', '/api/v1/me', undefined, tok),
  targets: () => call<Target[]>('GET', '/api/v1/targets'),
  requests: (q: { status?: string; requester?: string; limit?: number } = {}) => {
    const p = new URLSearchParams()
    Object.entries(q).forEach(([k, v]) => v !== undefined && v !== '' && p.set(k, String(v)))
    return call<GkRequest[]>('GET', `/api/v1/requests?${p}`)
  },
  request: (id: string) => call<GkRequest>('GET', `/api/v1/requests/${id}`),
  submit: (b: { target: string; command: string; reason?: string }) =>
    call<GkRequest>('POST', '/api/v1/requests', b),
  check: (command: string, target?: string) =>
    call<Check>('POST', '/api/v1/requests', { command, target, dry_run: true }),
  rules: () => call<Rules>('GET', '/api/v1/rules'),
  saveRule: (r: CustomRule) =>
    r.id ? call<CustomRule>('PUT', `/api/v1/rules/${r.id}`, r) : call<CustomRule>('POST', '/api/v1/rules', r),
  deleteRule: (id: number) => call<{ ok: boolean }>('DELETE', `/api/v1/rules/${id}`),
  testRule: (rule: CustomRule, commands: string[], target?: string) =>
    call<RuleTest[]>('POST', '/api/v1/rules/test', { rule, commands, target }),
  approve: (id: string, note: string) => call<GkRequest>('POST', `/api/v1/requests/${id}/approve`, { note }),
  reject: (id: string, note: string) => call<GkRequest>('POST', `/api/v1/requests/${id}/reject`, { note }),
  cancel: (id: string) => call<GkRequest>('POST', `/api/v1/requests/${id}/cancel`, {}),
  audit: (q: { actor?: string; action?: string; request_id?: string; before?: number; limit?: number } = {}) => {
    const p = new URLSearchParams()
    Object.entries(q).forEach(([k, v]) => v !== undefined && v !== '' && p.set(k, String(v)))
    return call<AuditEvent[]>('GET', `/api/v1/audit?${p}`)
  },
  verify: () => call<Verify>('GET', '/api/v1/audit/verify'),
}

// Which request levels a human role may approve (mirrors internal/policy/authz.go).
export function canApprove(me: Me | undefined, r: GkRequest): boolean {
  if (!me || me.kind !== 'human' || me.name === r.requester) return false
  if (me.role === 'admin') return true
  if (me.role === 'operator') return r.level <= 2
  return false
}
