import type { Level, Status } from '~/lib/api'

const levelStyle: Record<Level, { label: string; cls: string }> = {
  0: { label: 'L0 只读', cls: 'bg-hairline-soft text-body border-hairline' },
  1: { label: 'L1 低风险', cls: 'bg-accent-soft text-accent-deep border-accent-soft' },
  2: { label: 'L2 高风险', cls: 'bg-warning-soft text-warning-deep border-warning-soft' },
  3: { label: 'L3 危险', cls: 'bg-error-soft text-error border-error-soft' },
}

export function LevelBadge({ level }: { level: Level }) {
  const s = levelStyle[level] ?? levelStyle[2]
  return (
    <span className={`code inline-flex h-5 items-center rounded-[4px] border px-1.5 text-[11px] font-medium ${s.cls}`}>
      {s.label}
    </span>
  )
}

const statusStyle: Record<Status, { label: string; dot: string }> = {
  pending: { label: '待审批', dot: 'bg-warning' },
  approved: { label: '已批准', dot: 'bg-accent' },
  running: { label: '执行中', dot: 'bg-link animate-pulse' },
  succeeded: { label: '成功', dot: 'bg-accent' },
  failed: { label: '失败', dot: 'bg-error' },
  rejected: { label: '已驳回', dot: 'bg-error' },
  denied: { label: '策略拒绝', dot: 'bg-error' },
  expired: { label: '已过期', dot: 'bg-faint' },
  cancelled: { label: '已撤回', dot: 'bg-faint' },
}

export function StatusBadge({ status }: { status: Status }) {
  const s = statusStyle[status] ?? { label: status, dot: 'bg-faint' }
  return (
    <span className="inline-flex items-center gap-1.5 text-[13px] text-body">
      <span className={`h-2 w-2 rounded-full ${s.dot}`} />
      {s.label}
    </span>
  )
}

export function KindBadge({ kind }: { kind: string }) {
  return kind === 'agent' ? (
    <span className="code rounded-[4px] bg-violet-soft px-1 text-[11px] text-violet">agent</span>
  ) : kind === 'human' ? (
    <span className="code rounded-[4px] bg-hairline-soft px-1 text-[11px] text-body">human</span>
  ) : (
    <span className="code rounded-[4px] bg-hairline-soft px-1 text-[11px] text-mute">{kind}</span>
  )
}

export function Command({ argv, className = '' }: { argv: string[]; className?: string }) {
  return (
    <code className={`code break-all text-ink ${className}`}>
      <span className="select-none text-faint">$ </span>
      {argv.map((a) => (/[\s'"]/.test(a) ? `'${a.replace(/'/g, `'\\''`)}'` : a)).join(' ')}
    </code>
  )
}

export function Empty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="card flex flex-col items-center justify-center gap-1 px-6 py-14 text-center">
      <div className="font-medium text-ink">{title}</div>
      {hint && <div className="text-mute">{hint}</div>}
    </div>
  )
}

export function PageHeader({ eyebrow, title, desc, right }: { eyebrow: string; title: string; desc?: string; right?: React.ReactNode }) {
  return (
    <div className="mb-6 flex items-end justify-between gap-4">
      <div>
        <div className="eyebrow mb-2">{eyebrow}</div>
        <h1 className="text-[28px] font-semibold leading-9 tracking-[-1px] text-ink">{title}</h1>
        {desc && <p className="mt-1 text-body">{desc}</p>}
      </div>
      {right}
    </div>
  )
}
