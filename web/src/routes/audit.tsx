import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api } from '~/lib/api'
import { fmtTime } from '~/lib/hooks'
import { Empty, KindBadge, PageHeader } from '~/components/ui'

export const Route = createFileRoute('/audit')({ component: Audit })

const actionTone = (a: string) =>
  a.includes('fail') || a.includes('forbidden') || a.includes('reject')
    ? 'text-error'
    : a.includes('approve')
      ? 'text-accent-deep'
      : a.startsWith('server.')
        ? 'text-mute'
        : 'text-ink'

function Audit() {
  const [actor, setActor] = useState('')
  const [action, setAction] = useState('')
  const verify = useQuery({ queryKey: ['audit', 'verify'], queryFn: api.verify, staleTime: 30_000 })
  const q = useInfiniteQuery({
    queryKey: ['audit', { actor, action }],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => api.audit({ actor, action, before: pageParam || undefined, limit: 100 }),
    getNextPageParam: (last) => (last.length === 100 ? last[last.length - 1].seq : undefined),
  })
  const rows = q.data?.pages.flat() ?? []
  const v = verify.data
  return (
    <>
      <PageHeader
        eyebrow="Audit"
        title="审计日志"
        desc="只追加的哈希链日志：每条记录包含上一条的哈希，任何篡改或删除都会被校验发现。"
        right={
          <button className="btn" onClick={() => verify.refetch()}>
            <span className={`h-2 w-2 rounded-full ${v ? (v.ok ? 'bg-accent' : 'bg-error') : 'bg-faint'}`} />
            {v ? (v.ok ? `完整性校验通过 · ${v.count} 条` : `链在 #${v.broken_at} 断裂：${v.reason}`) : '校验中…'}
          </button>
        }
      />
      <div className="mb-4 flex gap-2">
        <input className="input h-8 max-w-[220px]" placeholder="操作者" value={actor} onChange={(e) => setActor(e.target.value.trim())} />
        <select className="input h-8 max-w-[240px]" value={action} onChange={(e) => setAction(e.target.value)}>
          <option value="">全部动作</option>
          {['request.create', 'request.approve', 'request.reject', 'request.execute', 'request.finish', 'request.cancel',
            'request.expire', 'request.decide_forbidden', 'request.forbidden_target', 'access.forbidden', 'auth.fail',
            'server.start', 'server.stop'].map((a) => (
            <option key={a}>{a}</option>
          ))}
        </select>
      </div>
      {rows.length === 0 && !q.isLoading ? (
        <Empty title="没有匹配的审计记录" />
      ) : (
        <div className="card overflow-hidden">
          <table className="w-full text-left">
            <thead className="border-b border-hairline bg-canvas">
              <tr className="eyebrow">
                <th className="px-4 py-2.5 font-medium">#</th>
                <th className="px-4 py-2.5 font-medium">时间</th>
                <th className="px-4 py-2.5 font-medium">操作者</th>
                <th className="px-4 py-2.5 font-medium">动作</th>
                <th className="px-4 py-2.5 font-medium">详情</th>
                <th className="px-4 py-2.5 font-medium">来源</th>
                <th className="px-4 py-2.5 font-medium">哈希</th>
              </tr>
            </thead>
            <tbody className="align-top">
              {rows.map((e) => (
                <tr key={e.seq} className="border-b border-hairline-soft last:border-0">
                  <td className="code px-4 py-2.5 text-[12px] text-faint">{e.seq}</td>
                  <td className="code px-4 py-2.5 text-[12px] whitespace-nowrap text-mute">{fmtTime(e.ts)}</td>
                  <td className="px-4 py-2.5 whitespace-nowrap">{e.actor} <KindBadge kind={e.actor_kind} /></td>
                  <td className={`code px-4 py-2.5 text-[12.5px] whitespace-nowrap ${actionTone(e.action)}`}>{e.action}</td>
                  <td className="px-4 py-2.5">
                    {e.request_id && (
                      <Link to="/requests/$id" params={{ id: e.request_id }} className="code mr-2 text-[12px] text-link hover:underline">
                        {e.request_id}
                      </Link>
                    )}
                    {e.target && <span className="code mr-2 text-[12px] text-body">@{e.target}</span>}
                    <Detail d={e.detail} />
                  </td>
                  <td className="code px-4 py-2.5 text-[12px] text-faint">{e.remote || '—'}</td>
                  <td className="code px-4 py-2.5 text-[12px] text-faint" title={`hash ${e.hash}\nprev ${e.prev_hash}`}>{e.hash.slice(0, 8)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {q.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <button className="btn" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>加载更多</button>
        </div>
      )}
    </>
  )
}

function Detail({ d }: { d?: Record<string, unknown> }) {
  if (!d || Object.keys(d).length === 0) return null
  const parts = Object.entries(d)
    .filter(([, v]) => v !== '' && v !== null && v !== false)
    .map(([k, v]) => `${k}=${Array.isArray(v) ? v.join(' ') : typeof v === 'object' ? JSON.stringify(v) : String(v)}`)
  return <span className="code text-[12px] break-all text-mute">{parts.join('  ')}</span>
}
