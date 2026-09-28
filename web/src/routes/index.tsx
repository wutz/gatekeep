import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, canApprove, type GkRequest, type Me } from '~/lib/api'
import { fmtRelative, fmtTime } from '~/lib/hooks'
import { Command, Empty, KindBadge, LevelBadge, PageHeader } from '~/components/ui'
import { useMe } from './__root'

export const Route = createFileRoute('/')({ component: Approvals })

function Approvals() {
  const me = useMe().data
  const q = useQuery({ queryKey: ['requests', { status: 'pending' }], queryFn: () => api.requests({ status: 'pending' }) })
  const list = q.data ?? []
  return (
    <>
      <PageHeader
        eyebrow="Approvals"
        title="待审批操作"
        desc="Agent 的一切变更操作、以及超出人类角色直接权限的操作都会在这里等待另一位人类批准。"
      />
      {q.isLoading ? (
        <div className="text-mute">加载中…</div>
      ) : list.length === 0 ? (
        <Empty title="没有待审批的操作" hint="新的请求会实时出现在这里。" />
      ) : (
        <div className="flex flex-col gap-3">
          {list.map((r) => (
            <ApprovalCard key={r.id} r={r} me={me} />
          ))}
        </div>
      )}
    </>
  )
}

function ApprovalCard({ r, me }: { r: GkRequest; me?: Me }) {
  const qc = useQueryClient()
  const [note, setNote] = useState('')
  const [confirmText, setConfirmText] = useState('')
  const allowed = canApprove(me, r)
  const critical = r.level >= 3
  const decide = useMutation({
    mutationFn: (approve: boolean) => (approve ? api.approve(r.id, note) : api.reject(r.id, note)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['requests'] }),
  })
  // L3 requires typing the target name to approve, to prevent click-through.
  const confirmOk = !critical || confirmText === r.target
  return (
    <div className={`card overflow-hidden ${critical ? 'border-error/40' : ''}`}>
      <div className="flex items-start justify-between gap-4 p-5">
        <div className="min-w-0 flex-1">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <LevelBadge level={r.level} />
            <span className="code text-[12px] text-mute">rule: {r.rule}</span>
            <span className="text-faint">·</span>
            <span className="text-[13px] text-body">
              {r.requester} <KindBadge kind={r.requester_kind} />
            </span>
            <span className="text-faint">·</span>
            <span className="text-[13px] text-mute" title={fmtTime(r.created_at)}>
              {fmtRelative(r.created_at)}
            </span>
          </div>
          <div className="rounded-sm border border-hairline bg-canvas px-3 py-2">
            <div className="eyebrow mb-1 normal-case">{r.target}</div>
            <Command argv={r.argv} />
          </div>
          <div className="mt-3 text-body">
            <span className="text-mute">理由：</span>
            {r.reason || <span className="text-faint">（未填写）</span>}
          </div>
        </div>
        <Link to="/requests/$id" params={{ id: r.id }} className="code shrink-0 text-[12px] text-mute hover:text-ink">
          {r.id}
        </Link>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-t border-hairline bg-canvas px-5 py-3">
        {allowed ? (
          <>
            <input className="input h-8 max-w-xs flex-1" placeholder="审批备注（可选）" value={note} onChange={(e) => setNote(e.target.value)} />
            {critical && (
              <input
                className="input code h-8 max-w-[200px]"
                placeholder={`输入 ${r.target} 以确认`}
                value={confirmText}
                onChange={(e) => setConfirmText(e.target.value)}
              />
            )}
            <div className="ml-auto flex gap-2">
              <button className="btn btn-danger" disabled={decide.isPending} onClick={() => decide.mutate(false)}>
                驳回
              </button>
              <button className="btn btn-accent" disabled={decide.isPending || !confirmOk} onClick={() => decide.mutate(true)}>
                批准并执行
              </button>
            </div>
          </>
        ) : (
          <span className="text-[13px] text-mute">
            {me?.name === r.requester ? '不能审批自己提交的请求。' : `你的角色（${me?.role}）无权审批 L${r.level} 操作。`}
          </span>
        )}
        {decide.error && <span className="w-full text-[13px] text-error">{(decide.error as Error).message}</span>}
      </div>
    </div>
  )
}
