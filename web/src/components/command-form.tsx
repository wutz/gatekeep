import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type CmdParam, type CustomCommand, type Level } from '~/lib/api'

export const emptyCommand: CustomCommand = { name: '', description: '', template: '', params: [], level: 1, targets: [], enabled: true }

const levels: { v: Level; t: string }[] = [
  { v: 0, t: 'L0 只读 — Agent 可直接执行' },
  { v: 1, t: 'L1 低风险 — 运维可直接执行' },
  { v: 2, t: 'L2 高风险 — 管理员可直接执行' },
  { v: 3, t: 'L3 危险 — 总是需要审批' },
]

const placeholderRe = /\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g

// placeholders returns the parameter names used in a template, in order.
export function placeholders(template: string): string[] {
  const out: string[] = []
  for (const m of template.matchAll(placeholderRe)) if (!out.includes(m[1])) out.push(m[1])
  return out
}

export function CommandForm({ initial, onDone }: { initial: CustomCommand; onDone: () => void }) {
  const qc = useQueryClient()
  const targets = useQuery({ queryKey: ['targets'], queryFn: api.targets })
  const [c, setC] = useState<CustomCommand>(initial)
  const set = <K extends keyof CustomCommand>(k: K, v: CustomCommand[K]) => setC((x) => ({ ...x, [k]: v }))
  // Keep the parameter list in sync with the template's placeholders,
  // preserving whatever was already configured for each name.
  const setTemplate = (template: string) =>
    setC((x) => ({
      ...x,
      template,
      params: placeholders(template).map((n) => x.params.find((p) => p.name === n) ?? { name: n }),
    }))
  const setParam = (i: number, p: Partial<CmdParam>) =>
    setC((x) => ({ ...x, params: x.params.map((q, j) => (j === i ? { ...q, ...p } : q)) }))
  const toggleTarget = (t: string) =>
    set('targets', c.targets.includes(t) ? c.targets.filter((x) => x !== t) : [...c.targets, t])
  const save = useMutation({
    mutationFn: () => api.saveCommand(c),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['commands'] })
      onDone()
    },
  })
  return (
    <form
      className="card p-5"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="grid gap-4 md:grid-cols-[1fr_2fr_1fr]">
        <Field label="名称（小写、数字、- _）">
          <input className="input code" value={c.name} onChange={(e) => set('name', e.target.value)} placeholder="restart-service" />
        </Field>
        <Field label="说明">
          <input className="input" value={c.description ?? ''} onChange={(e) => set('description', e.target.value)} placeholder="重启 GPU 节点上的系统服务" />
        </Field>
        <Field label="级别">
          <select className="input" value={c.level} onChange={(e) => set('level', Number(e.target.value) as Level)}>
            {levels.map((l) => <option key={l.v} value={l.v}>{l.t}</option>)}
          </select>
        </Field>
      </div>
      <Field label="命令模板 · 用 {{参数}} 占位，不经过 shell" className="mt-4">
        <input className="input code" value={c.template} onChange={(e) => setTemplate(e.target.value)} placeholder="systemctl restart {{service}}" />
      </Field>
      {c.params.length > 0 && (
        <div className="mt-4">
          <div className="eyebrow mb-2 normal-case">参数 · 取值必须完整匹配正则；留空则只允许不以 - 开头的普通单词</div>
          <div className="space-y-2">
            {c.params.map((p, i) => (
              <div key={p.name} className="grid items-center gap-2 md:grid-cols-[120px_1.2fr_1fr_120px_auto]">
                <code className="code text-ink">{p.name}</code>
                <input className="input code" value={p.pattern ?? ''} onChange={(e) => setParam(i, { pattern: e.target.value })} placeholder="正则，如 kubelet|containerd" />
                <input className="input" value={p.description ?? ''} onChange={(e) => setParam(i, { description: e.target.value })} placeholder="说明" />
                <input className="input code" value={p.default ?? ''} onChange={(e) => setParam(i, { default: e.target.value })} placeholder="默认值" />
                <label className="flex items-center gap-1.5 text-[13px] text-body whitespace-nowrap">
                  <input type="checkbox" checked={!!p.optional} onChange={(e) => setParam(i, { optional: e.target.checked })} /> 可选
                </label>
              </div>
            ))}
          </div>
        </div>
      )}
      <Field label="允许的目标（不选 = 全部）" className="mt-4">
        <div className="flex flex-wrap gap-2">
          {targets.data?.map((t) => (
            <label key={t.name} className={`btn h-7 cursor-pointer px-2 text-[12px] ${c.targets.includes(t.name) ? 'btn-primary' : ''}`}>
              <input type="checkbox" className="hidden" checked={c.targets.includes(t.name)} onChange={() => toggleTarget(t.name)} />
              {t.name}
            </label>
          ))}
        </div>
      </Field>
      {save.error && <div className="mt-3 text-[13px] text-error">{(save.error as Error).message}</div>}
      <div className="mt-5 flex items-center gap-2">
        <label className="flex items-center gap-2 text-body">
          <input type="checkbox" checked={c.enabled} onChange={(e) => set('enabled', e.target.checked)} /> 启用
        </label>
        <span className="text-[12px] text-mute">锁定规则与 shell 元字符仍会把实际级别抬高，不会被命令级别降低。</span>
        <button type="button" className="btn ml-auto" onClick={onDone}>取消</button>
        <button className="btn btn-primary" disabled={!c.name || !c.template || save.isPending}>{c.id ? '保存' : '创建'}</button>
      </div>
    </form>
  )
}

export function Field({ label, children, className = '' }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <label className="eyebrow mb-2 block normal-case">{label}</label>
      {children}
    </div>
  )
}
