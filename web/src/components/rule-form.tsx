import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type CustomRule, type Level } from '~/lib/api'
import { LevelBadge } from './ui'

export const emptyRule: CustomRule = { name: '', program: '', args: '', not_args: '', level: 2, target: '', priority: 100, enabled: true, note: '' }

const levels: { v: Level; t: string }[] = [
  { v: 0, t: 'L0 只读 — Agent 可直接执行' },
  { v: 1, t: 'L1 低风险 — 运维可直接执行' },
  { v: 2, t: 'L2 高风险 — 管理员可直接执行' },
  { v: 3, t: 'L3 危险 — 总是需要审批' },
]

export function RuleForm({ initial, onDone }: { initial: CustomRule; onDone: () => void }) {
  const qc = useQueryClient()
  const targets = useQuery({ queryKey: ['targets'], queryFn: api.targets })
  const [r, setR] = useState<CustomRule>(initial)
  const [samples, setSamples] = useState(initial.program ? `${initial.program} ` : '')
  const set = <K extends keyof CustomRule>(k: K, v: CustomRule[K]) => setR((x) => ({ ...x, [k]: v }))
  const save = useMutation({
    mutationFn: () => api.saveRule(r),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['rules'] })
      onDone()
    },
  })
  const test = useMutation({
    mutationFn: () => api.testRule(r, samples.split('\n').map((l) => l.trim()).filter(Boolean)),
  })
  return (
    <form
      className="card p-5"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="grid gap-4 md:grid-cols-3">
        <Field label="名称">
          <input className="input" value={r.name} onChange={(e) => set('name', e.target.value)} placeholder="kubelet-restart-gpu" />
        </Field>
        <Field label="程序（glob，支持 {a,b}）">
          <input className="input code" value={r.program} onChange={(e) => set('program', e.target.value)} placeholder="systemctl" />
        </Field>
        <Field label="级别">
          <select className="input" value={r.level} onChange={(e) => set('level', Number(e.target.value) as Level)}>
            {levels.map((l) => <option key={l.v} value={l.v}>{l.t}</option>)}
          </select>
        </Field>
        <Field label="参数须匹配（正则，可空）">
          <input className="input code" value={r.args ?? ''} onChange={(e) => set('args', e.target.value)} placeholder="^restart\s+kubelet$" />
        </Field>
        <Field label="参数不得匹配（正则，可空）">
          <input className="input code" value={r.not_args ?? ''} onChange={(e) => set('not_args', e.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="目标">
            <select className="input" value={r.target ?? ''} onChange={(e) => set('target', e.target.value)}>
              <option value="">全部</option>
              {targets.data?.map((t) => <option key={t.name} value={t.name}>{t.name}</option>)}
            </select>
          </Field>
          <Field label="优先级（小先）">
            <input className="input" type="number" value={r.priority} onChange={(e) => set('priority', Number(e.target.value))} />
          </Field>
        </div>
      </div>
      <Field label="说明（为什么需要这条规则）" className="mt-4">
        <input className="input" value={r.note ?? ''} onChange={(e) => set('note', e.target.value)} />
      </Field>
      <Field label="试算：每行一条命令，对比保存前后的分级" className="mt-4">
        <textarea className="input code h-20 py-2" value={samples} onChange={(e) => setSamples(e.target.value)} placeholder="systemctl restart kubelet" />
      </Field>
      {test.data && (
        <div className="mt-3 space-y-1 text-[13px]">
          {test.data.map((t) => (
            <div key={t.command} className="flex flex-wrap items-center gap-2">
              <code className="code text-ink">{t.command}</code>
              {t.error ? <span className="text-error">{t.error}</span> : (
                <>
                  <LevelBadge level={t.before.level} /> <span className="text-faint">→</span> <LevelBadge level={t.after.level} />
                  <span className="code text-[12px] text-mute">{t.after.rule}</span>
                  {t.after.level !== t.before.level && <span className="text-warning-deep">分级变化</span>}
                </>
              )}
            </div>
          ))}
        </div>
      )}
      {(save.error || test.error) && <div className="mt-3 text-[13px] text-error">{((save.error || test.error) as Error).message}</div>}
      <div className="mt-5 flex items-center gap-2">
        <label className="flex items-center gap-2 text-body">
          <input type="checkbox" checked={r.enabled} onChange={(e) => set('enabled', e.target.checked)} /> 启用
        </label>
        <button type="button" className="btn ml-auto" onClick={onDone}>取消</button>
        <button type="button" className="btn" disabled={!r.program || !samples.trim() || test.isPending} onClick={() => test.mutate()}>试算</button>
        <button className="btn btn-primary" disabled={!r.name || !r.program || save.isPending}>{r.id ? '保存' : '创建'}</button>
      </div>
    </form>
  )
}

function Field({ label, children, className = '' }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <label className="eyebrow mb-2 block normal-case">{label}</label>
      {children}
    </div>
  )
}
