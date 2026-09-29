import { useEffect, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, type Check } from '~/lib/api'
import { Command, LevelBadge, PageHeader, StatusBadge } from '~/components/ui'
import { useMe } from './__root'

export const Route = createFileRoute('/run')({ component: Run })

const outcomeText: Record<Check['outcome'], { t: string; cls: string }> = {
  allow: { t: '你可以直接执行', cls: 'text-accent-deep' },
  need_approval: { t: '需要另一位人类审批', cls: 'text-warning-deep' },
  deny: { t: '你的角色不允许提交此操作', cls: 'text-error' },
}

function Run() {
  const me = useMe().data
  const targets = useQuery({ queryKey: ['targets'], queryFn: api.targets })
  const [target, setTarget] = useState('')
  const [command, setCommand] = useState('')
  const [reason, setReason] = useState('')
  const [check, setCheck] = useState<Check | null>(null)
  useEffect(() => {
    if (!target && targets.data?.length) setTarget(targets.data[0].name)
  }, [targets.data, target])
  // Live classification while typing.
  useEffect(() => {
    if (!command.trim()) return setCheck(null)
    const t = setTimeout(() => api.check(command, target).then(setCheck).catch(() => setCheck(null)), 250)
    return () => clearTimeout(t)
  }, [command, target])
  const submit = useMutation({ mutationFn: () => api.submit({ target, command, reason }) })
  const res = submit.data
  return (
    <>
      <PageHeader
        eyebrow="Run"
        title="执行命令"
        desc={`以 ${me?.name}（${me?.role}）身份执行。命令不经过 shell，管道与重定向会被判定为危险操作。`}
      />
      <form
        className="card p-5"
        onSubmit={(e) => {
          e.preventDefault()
          submit.mutate()
        }}
      >
        <div className="grid gap-4 md:grid-cols-[200px_1fr]">
          <div>
            <label className="eyebrow mb-2 block">目标</label>
            <select className="input" value={target} onChange={(e) => setTarget(e.target.value)}>
              {targets.data?.map((t) => (
                <option key={t.name} value={t.name}>{t.name}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="eyebrow mb-2 block">命令</label>
            <input className="input code" placeholder="kubectl get pods -A" value={command} onChange={(e) => setCommand(e.target.value)} />
          </div>
        </div>
        <label className="eyebrow mt-4 mb-2 block">理由（变更操作必填，审批人可见）</label>
        <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="例如：节点 gpu-105 kubelet 卡死，需要重启" />
        <div className="mt-4 flex items-center gap-3">
          {check && (
            <>
              <LevelBadge level={check.level} />
              <span className="code text-[12px] text-mute">rule: {check.rule}{check.custom && ' (自定义)'}</span>
              <span className={`text-[13px] ${outcomeText[check.outcome].cls}`}>{outcomeText[check.outcome].t}</span>
            </>
          )}
          <button className="btn btn-primary ml-auto" disabled={!command.trim() || !target || submit.isPending || check?.outcome === 'deny'}>
            {check?.outcome === 'need_approval' ? '提交审批' : '执行'}
          </button>
        </div>
        {submit.error && <div className="mt-3 text-[13px] text-error">{(submit.error as Error).message}</div>}
      </form>
      {res && (
        <div className="card mt-6 overflow-hidden">
          <div className="flex items-center gap-3 border-b border-hairline px-5 py-3">
            <StatusBadge status={res.status} />
            <Command argv={res.argv} className="text-[13px]" />
            <Link to="/requests/$id" params={{ id: res.id }} className="code ml-auto text-[12px] text-mute hover:text-ink">{res.id} →</Link>
          </div>
          {res.status === 'pending' ? (
            <div className="px-5 py-4 text-body">已提交，等待另一位人类审批。</div>
          ) : (
            <pre className="code max-h-[480px] overflow-auto bg-ink p-4 text-[12.5px] text-[#ededed]">
              {res.stdout}
              {res.stderr && <span className="text-[#ff8a8a]">{res.stderr}</span>}
            </pre>
          )}
        </div>
      )}
    </>
  )
}
