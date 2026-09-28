import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api, type Status } from '~/lib/api'
import { fmtRelative, fmtTime } from '~/lib/hooks'
import { Command, Empty, KindBadge, LevelBadge, PageHeader, StatusBadge } from '~/components/ui'

export const Route = createFileRoute('/requests/')({ component: Requests })

const filters: { v: '' | Status; label: string }[] = [
  { v: '', label: '全部' },
  { v: 'pending', label: '待审批' },
  { v: 'succeeded', label: '成功' },
  { v: 'failed', label: '失败' },
  { v: 'rejected', label: '驳回' },
  { v: 'denied', label: '拒绝' },
]

function Requests() {
  const [status, setStatus] = useState<'' | Status>('')
  const [requester, setRequester] = useState('')
  const q = useQuery({
    queryKey: ['requests', { status, requester, limit: 200 }],
    queryFn: () => api.requests({ status, requester, limit: 200 }),
  })
  return (
    <>
      <PageHeader eyebrow="Requests" title="操作记录" desc="所有经由 gatekeep 提交的命令，含 Agent 与人类。" />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        {filters.map((f) => (
          <button
            key={f.v}
            onClick={() => setStatus(f.v)}
            className={`h-8 rounded-full border px-3 text-[13px] ${status === f.v ? 'border-ink bg-ink text-white' : 'border-hairline bg-elevated text-body hover:text-ink'}`}
          >
            {f.label}
          </button>
        ))}
        <input className="input ml-auto h-8 max-w-[220px]" placeholder="按提交者过滤" value={requester} onChange={(e) => setRequester(e.target.value.trim())} />
      </div>
      {q.data && q.data.length === 0 ? (
        <Empty title="暂无记录" />
      ) : (
        <div className="card overflow-hidden">
          <table className="w-full text-left">
            <thead className="border-b border-hairline bg-canvas">
              <tr className="eyebrow">
                <th className="px-4 py-2.5 font-medium">命令</th>
                <th className="px-4 py-2.5 font-medium">级别</th>
                <th className="px-4 py-2.5 font-medium">状态</th>
                <th className="px-4 py-2.5 font-medium">提交者</th>
                <th className="px-4 py-2.5 font-medium">审批人</th>
                <th className="px-4 py-2.5 text-right font-medium">时间</th>
              </tr>
            </thead>
            <tbody>
              {q.data?.map((r) => (
                <tr key={r.id} className="border-b border-hairline-soft last:border-0 hover:bg-canvas">
                  <td className="max-w-[420px] px-4 py-3">
                    <Link to="/requests/$id" params={{ id: r.id }} className="block">
                      <div className="eyebrow mb-0.5 normal-case">{r.target}</div>
                      <Command argv={r.argv} className="line-clamp-1" />
                    </Link>
                  </td>
                  <td className="px-4 py-3"><LevelBadge level={r.level} /></td>
                  <td className="px-4 py-3"><StatusBadge status={r.status} /></td>
                  <td className="px-4 py-3 whitespace-nowrap">{r.requester} <KindBadge kind={r.requester_kind} /></td>
                  <td className="px-4 py-3 text-body">{r.approver && r.approver !== r.requester ? r.approver : <span className="text-faint">—</span>}</td>
                  <td className="px-4 py-3 text-right whitespace-nowrap text-mute" title={fmtTime(r.created_at)}>{fmtRelative(r.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
