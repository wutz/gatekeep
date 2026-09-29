import { useEffect, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type Check, type CustomCommand } from '~/lib/api'
import { fmtRelative } from '~/lib/hooks'
import { Command, Empty, LevelBadge, PageHeader, StatusBadge } from '~/components/ui'
import { CommandForm, Field, emptyCommand } from '~/components/command-form'

export const Route = createFileRoute('/commands')({ component: CommandsPage })

const outcomeText: Record<Check['outcome'], { t: string; cls: string }> = {
  allow: { t: '你可以直接执行', cls: 'text-accent-deep' },
  need_approval: { t: '需要另一位人类审批', cls: 'text-warning-deep' },
  deny: { t: '你的角色不允许提交此操作', cls: 'text-error' },
}

function CommandsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['commands'], queryFn: api.commands })
  const [editing, setEditing] = useState<CustomCommand | null>(null)
  const [running, setRunning] = useState<string | null>(null)
  const del = useMutation({
    mutationFn: (id: number) => api.deleteCommand(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['commands'] }),
  })
  const toggle = useMutation({
    mutationFn: (c: CustomCommand) => api.saveCommand({ ...c, enabled: !c.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['commands'] }),
  })
  const d = q.data
  return (
    <>
      <PageHeader
        eyebrow="Commands"
        title="自定义命令"
        desc="管理员把常用运维操作定义成带参数的命令模板，人类在这里或用 gk run 执行，Agent 通过 MCP 的 run_command 调用。参数按正则校验，每次执行都是一条普通请求，走同样的分级、审批与审计。"
        right={d?.can_edit && !editing && <button className="btn btn-primary" onClick={() => setEditing({ ...emptyCommand })}>新建命令</button>}
      />
      {d && !d.can_edit && <div className="mb-4 text-[13px] text-mute">只有管理员可以定义命令，你可以执行。</div>}
      {editing && (
        <div className="mb-6">
          <CommandForm key={editing.id ?? 'new'} initial={editing} onDone={() => setEditing(null)} />
        </div>
      )}
      {d && d.commands.length === 0 ? (
        <Empty title="暂无自定义命令" hint="例如：restart-service = systemctl restart {{service}}，service 限定为 kubelet|containerd。" />
      ) : (
        <div className="space-y-3">
          {d?.commands.map((c) => (
            <div key={c.id} className={`card ${c.enabled ? '' : 'opacity-60'}`}>
              <div className="flex flex-wrap items-start gap-3 px-5 py-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="code font-medium text-ink">{c.name}</span>
                    <LevelBadge level={c.level} />
                    {!c.enabled && <span className="text-[12px] text-mute">已停用</span>}
                  </div>
                  {c.description && <div className="mt-1 text-body">{c.description}</div>}
                  <div className="code mt-2 text-[12.5px] text-mute">{c.template}</div>
                  <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-mute">
                    <span>目标：<span className="code">{c.targets.length ? c.targets.join(', ') : '全部'}</span></span>
                    {c.params.map((p) => (
                      <span key={p.name}>
                        <span className="code text-ink">{p.name}</span>
                        {p.pattern && <span className="code"> ~ {p.pattern}</span>}
                        {p.optional && ' (可选)'}
                      </span>
                    ))}
                    <span>{c.updated_by} · {fmtRelative(c.updated_at)}</span>
                  </div>
                </div>
                <span className="inline-flex gap-1">
                  {c.enabled && (
                    <button className="btn btn-primary h-7 px-2 text-[12px]" onClick={() => setRunning(running === c.name ? null : c.name)}>
                      {running === c.name ? '收起' : '执行'}
                    </button>
                  )}
                  {d.can_edit && (
                    <>
                      <button className="btn h-7 px-2 text-[12px]" onClick={() => toggle.mutate(c)}>{c.enabled ? '停用' : '启用'}</button>
                      <button className="btn h-7 px-2 text-[12px]" onClick={() => setEditing(c)}>编辑</button>
                      <button className="btn h-7 px-2 text-[12px] text-error" onClick={() => confirm(`删除命令 ${c.name}？`) && del.mutate(c.id!)}>删除</button>
                    </>
                  )}
                </span>
              </div>
              {running === c.name && <RunPanel cmd={c} />}
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function RunPanel({ cmd }: { cmd: CustomCommand }) {
  const targets = useQuery({ queryKey: ['targets'], queryFn: api.targets })
  const allowed = (targets.data ?? []).filter((t) => !cmd.targets.length || cmd.targets.includes(t.name))
  const [target, setTarget] = useState('')
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(cmd.params.map((p) => [p.name, p.default ?? ''])),
  )
  const [reason, setReason] = useState('')
  const [check, setCheck] = useState<Check | null>(null)
  const [checkErr, setCheckErr] = useState('')
  useEffect(() => {
    if (!target && allowed.length) setTarget(allowed[0].name)
  }, [allowed, target])
  const body = () => ({ target, params: Object.fromEntries(Object.entries(values).filter(([, v]) => v !== '')), reason })
  // Live render + classification as parameters change.
  useEffect(() => {
    if (!target) return
    const t = setTimeout(
      () =>
        api.checkCommand(cmd.name, body()).then(
          (c) => (setCheck(c), setCheckErr('')),
          (e: Error) => (setCheck(null), setCheckErr(e.message)),
        ),
      250,
    )
    return () => clearTimeout(t)
  }, [values, target])
  const run = useMutation({ mutationFn: () => api.runCommand(cmd.name, body()) })
  const res = run.data
  return (
    <form
      className="border-t border-hairline bg-canvas px-5 py-4"
      onSubmit={(e) => {
        e.preventDefault()
        run.mutate()
      }}
    >
      <div className="grid gap-3 md:grid-cols-3">
        <Field label="目标">
          <select className="input" value={target} onChange={(e) => setTarget(e.target.value)}>
            {allowed.map((t) => <option key={t.name} value={t.name}>{t.name}</option>)}
          </select>
        </Field>
        {cmd.params.map((p) => (
          <Field key={p.name} label={`${p.name}${p.optional ? '（可选）' : ''}${p.description ? ' · ' + p.description : ''}`}>
            <input
              className="input code"
              value={values[p.name] ?? ''}
              placeholder={p.pattern || ''}
              onChange={(e) => setValues((v) => ({ ...v, [p.name]: e.target.value }))}
            />
          </Field>
        ))}
      </div>
      <Field label="理由（需要审批时必填）" className="mt-3">
        <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
      </Field>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        {check && (
          <>
            <Command argv={check.argv} className="text-[13px]" />
            <LevelBadge level={check.level} />
            <span className="code text-[12px] text-mute">{check.rule}</span>
            <span className={`text-[13px] ${outcomeText[check.outcome].cls}`}>{outcomeText[check.outcome].t}</span>
          </>
        )}
        {checkErr && <span className="text-[13px] text-error">{checkErr}</span>}
        <button className="btn btn-primary ml-auto" disabled={!check || run.isPending || check.outcome === 'deny'}>
          {check?.outcome === 'need_approval' ? '提交审批' : '执行'}
        </button>
      </div>
      {run.error && <div className="mt-3 text-[13px] text-error">{(run.error as Error).message}</div>}
      {res && (
        <div className="card mt-4 overflow-hidden">
          <div className="flex items-center gap-3 border-b border-hairline px-4 py-2.5">
            <StatusBadge status={res.status} />
            <Link to="/requests/$id" params={{ id: res.id }} className="code ml-auto text-[12px] text-mute hover:text-ink">{res.id} →</Link>
          </div>
          {res.status === 'pending' ? (
            <div className="px-4 py-3 text-body">已提交，等待另一位人类审批。</div>
          ) : (
            <pre className="code max-h-[360px] overflow-auto bg-ink p-4 text-[12.5px] text-[#ededed]">
              {res.stdout}
              {res.stderr && <span className="text-[#ff8a8a]">{res.stderr}</span>}
            </pre>
          )}
        </div>
      )}
    </form>
  )
}
