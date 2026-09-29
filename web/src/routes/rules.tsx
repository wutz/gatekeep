import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type CustomRule } from '~/lib/api'
import { fmtRelative } from '~/lib/hooks'
import { Empty, LevelBadge, PageHeader } from '~/components/ui'
import { RuleForm, emptyRule } from '~/components/rule-form'

export const Route = createFileRoute('/rules')({ component: RulesPage })

function RulesPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['rules'], queryFn: api.rules })
  const [editing, setEditing] = useState<CustomRule | null>(null)
  const [showBuiltin, setShowBuiltin] = useState(false)
  const del = useMutation({
    mutationFn: (id: number) => api.deleteRule(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rules'] }),
  })
  const toggle = useMutation({
    mutationFn: (r: CustomRule) => api.saveRule({ ...r, enabled: !r.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rules'] }),
  })
  const d = q.data
  return (
    <>
      <PageHeader
        eyebrow="Rules"
        title="命令分级规则"
        desc="自定义规则在锁定的内置规则之后、其余内置规则之前生效，可按目标主机单独调整某条命令的级别。所有变更写入审计日志并立即生效。"
        right={d?.can_edit && !editing && <button className="btn btn-primary" onClick={() => setEditing({ ...emptyRule })}>新建规则</button>}
      />
      {d && !d.can_edit && <div className="mb-4 text-[13px] text-mute">只有管理员可以修改规则，你可以查看。</div>}
      {editing && (
        <div className="mb-6">
          <RuleForm key={editing.id ?? 'new'} initial={editing} onDone={() => setEditing(null)} />
        </div>
      )}
      <div className="eyebrow mb-3">自定义规则 · 按优先级顺序匹配</div>
      {d && d.custom.length === 0 ? (
        <Empty title="暂无自定义规则" hint="当前全部使用 policy.yaml 中的内置分级。" />
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-left">
            <thead className="border-b border-hairline bg-canvas">
              <tr className="eyebrow">
                <th className="px-4 py-2.5 font-medium">优先级</th>
                <th className="px-4 py-2.5 font-medium">规则</th>
                <th className="px-4 py-2.5 font-medium">匹配</th>
                <th className="px-4 py-2.5 font-medium">级别</th>
                <th className="px-4 py-2.5 font-medium">目标</th>
                <th className="px-4 py-2.5 font-medium">更新</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {d?.custom.map((r) => (
                <tr key={r.id} className={`border-b border-hairline-soft last:border-0 ${r.enabled ? '' : 'opacity-50'}`}>
                  <td className="code px-4 py-3 text-mute">{r.priority}</td>
                  <td className="px-4 py-3">
                    <div className="font-medium text-ink">{r.name}</div>
                    {r.note && <div className="text-[12px] text-mute">{r.note}</div>}
                  </td>
                  <td className="code px-4 py-3 text-[12px] text-ink">
                    {r.program} {r.args && <span className="text-body">~ {r.args}</span>}
                    {r.not_args && <div className="text-mute">!~ {r.not_args}</div>}
                  </td>
                  <td className="px-4 py-3"><LevelBadge level={r.level} /></td>
                  <td className="code px-4 py-3 text-[12px] text-body">{r.target || '全部'}</td>
                  <td className="px-4 py-3 whitespace-nowrap text-[12px] text-mute">{r.updated_by} · {fmtRelative(r.updated_at)}</td>
                  <td className="px-4 py-3 text-right whitespace-nowrap">
                    {d.can_edit && (
                      <span className="inline-flex gap-1">
                        <button className="btn h-7 px-2 text-[12px]" onClick={() => toggle.mutate(r)}>{r.enabled ? '停用' : '启用'}</button>
                        <button className="btn h-7 px-2 text-[12px]" onClick={() => setEditing(r)}>编辑</button>
                        <button
                          className="btn h-7 px-2 text-[12px] text-error"
                          onClick={() => confirm(`删除规则 ${r.name}？`) && del.mutate(r.id!)}
                        >
                          删除
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <button className="eyebrow mt-8 mb-3 hover:text-ink" onClick={() => setShowBuiltin((v) => !v)}>
        {showBuiltin ? '▾' : '▸'} 内置规则（policy.yaml，{d?.builtin.length ?? 0} 条，未匹配时默认 L{d?.default_level}）
      </button>
      {showBuiltin && (
        <div className="card overflow-x-auto">
          <table className="w-full text-left text-[12px]">
            <tbody>
              {d?.builtin.map((r) => (
                <tr key={r.name} className="border-b border-hairline-soft last:border-0">
                  <td className="px-4 py-2 font-medium text-ink">
                    {r.name}
                    {r.locked && <span className="code ml-2 rounded-[4px] bg-error-soft px-1 text-[11px] text-error">锁定</span>}
                  </td>
                  <td className="code max-w-[520px] truncate px-4 py-2 text-body" title={r.args}>{r.program} {r.args && `~ ${r.args}`}</td>
                  <td className="px-4 py-2"><LevelBadge level={r.level} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
