import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '~/lib/api'
import { fmtDuration, fmtTime } from '~/lib/hooks'
import { Command, KindBadge, LevelBadge, StatusBadge } from '~/components/ui'
import { useMe } from './__root'

export const Route = createFileRoute('/requests/$id')({ component: Detail })

function Detail() {
  const { id } = Route.useParams()
  const me = useMe().data
  const qc = useQueryClient()
  const r = useQuery({ queryKey: ['request', id], queryFn: () => api.request(id) })
  const audit = useQuery({ queryKey: ['audit', { request_id: id }], queryFn: () => api.audit({ request_id: id, limit: 100 }) })
  const cancel = useMutation({ mutationFn: () => api.cancel(id), onSuccess: () => qc.invalidateQueries({ queryKey: ['request', id] }) })
  if (r.error) return <div className="text-error">{(r.error as Error).message}</div>
  if (!r.data) return <div className="text-mute">加载中…</div>
  const d = r.data
  const fields: [string, React.ReactNode][] = [
    ['目标', <span className="code">{d.target}</span>],
    ['提交者', <>{d.requester} <KindBadge kind={d.requester_kind} /></>],
    ['匹配规则', <span className="code">{d.rule}</span>],
    ['提交时间', fmtTime(d.created_at)],
    ['审批人', d.approver || '—'],
    ['审批时间', fmtTime(d.decided_at)],
    ['执行耗时', fmtDuration(d.started_at, d.finished_at)],
    ['退出码', d.finished_at ? <span className={`code ${d.exit_code === 0 ? 'text-accent-deep' : 'text-error'}`}>{d.exit_code}</span> : '—'],
  ]
  return (
    <>
      <Link to="/requests" className="text-[13px] text-mute hover:text-ink">← 操作记录</Link>
      <div className="mt-4 mb-6 flex items-start justify-between gap-4">
        <div>
          <div className="eyebrow mb-2">{d.id}</div>
          <div className="flex items-center gap-3">
            <LevelBadge level={d.level} />
            <StatusBadge status={d.status} />
          </div>
        </div>
        {d.status === 'pending' && me?.name === d.requester && (
          <button className="btn btn-danger" onClick={() => cancel.mutate()}>撤回请求</button>
        )}
      </div>
      <div className="card mb-6 p-5">
        <Command argv={d.argv} className="text-[15px]" />
        {d.reason && <p className="mt-3 text-body"><span className="text-mute">理由：</span>{d.reason}</p>}
        {d.decision_note && <p className="mt-1 text-body"><span className="text-mute">审批备注：</span>{d.decision_note}</p>}
      </div>
      <div className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-md border border-hairline bg-hairline md:grid-cols-4">
        {fields.map(([k, v]) => (
          <div key={k} className="bg-elevated px-4 py-3">
            <div className="eyebrow mb-1">{k}</div>
            <div className="text-ink">{v}</div>
          </div>
        ))}
      </div>
      {(d.stdout || d.stderr) && (
        <div className="mb-6">
          <div className="eyebrow mb-2">输出 {d.truncated && <span className="text-warning-deep">（已截断）</span>}</div>
          <pre className="code max-h-[520px] overflow-auto rounded-md bg-ink p-4 text-[12.5px] text-[#ededed]">
            {d.stdout}
            {d.stderr && <span className="text-[#ff8a8a]">{d.stderr}</span>}
          </pre>
        </div>
      )}
      <div className="eyebrow mb-2">审计轨迹</div>
      <div className="card divide-y divide-hairline-soft">
        {audit.data?.slice().reverse().map((e) => (
          <div key={e.seq} className="flex items-center gap-4 px-4 py-2.5">
            <span className="code w-12 text-[12px] text-faint">#{e.seq}</span>
            <span className="code w-40 text-[12px] text-mute">{fmtTime(e.ts)}</span>
            <span className="code w-44 text-[13px] text-ink">{e.action}</span>
            <span className="text-body">{e.actor} <KindBadge kind={e.actor_kind} /></span>
            {e.remote && <span className="code ml-auto text-[12px] text-faint">{e.remote}</span>}
          </div>
        ))}
      </div>
    </>
  )
}
