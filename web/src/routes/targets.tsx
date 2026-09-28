import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api } from '~/lib/api'
import { PageHeader } from '~/components/ui'

export const Route = createFileRoute('/targets')({ component: Targets })

function Targets() {
  const targets = useQuery({ queryKey: ['targets'], queryFn: api.targets })
  const origin = typeof location === 'undefined' ? 'http://gatekeep:8740' : location.origin
  const mcpJson = JSON.stringify(
    { mcpServers: { gatekeep: { type: 'http', url: `${origin}/mcp`, headers: { Authorization: 'Bearer ${GATEKEEP_TOKEN}' } } } },
    null,
    2,
  )
  return (
    <>
      <PageHeader eyebrow="Targets" title="目标与接入" desc="gatekeep 可以代为执行命令的主机，以及 Agent 的接入方式。" />
      <div className="mb-10 grid gap-3 md:grid-cols-3">
        {targets.data?.map((t) => (
          <div key={t.name} className="card p-4">
            <div className="flex items-center justify-between">
              <span className="font-medium text-ink">{t.name}</span>
              <span className="code text-[11px] text-mute">{t.transport}</span>
            </div>
            <div className="code mt-1 text-[12px] text-body">{t.host ? `${t.user ? t.user + '@' : ''}${t.host}` : 'gatekeep host'}</div>
            {t.description && <div className="mt-1 text-[13px] text-mute">{t.description}</div>}
            <div className="mt-3 flex flex-wrap gap-1">
              {t.labels?.map((l) => (
                <span key={l} className="code rounded-[4px] bg-hairline-soft px-1.5 py-0.5 text-[11px] text-body">{l}</span>
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="eyebrow mb-3">Agent 接入</div>
      <div className="grid gap-4 md:grid-cols-2">
        <Snippet
          title="MCP（推荐）"
          desc="Claude Code / Codex / Cursor 等支持 MCP 的 Agent 直接获得 run、check、wait_request 等工具。"
          code={`claude mcp add --transport http gatekeep ${origin}/mcp \\\n  --header "Authorization: Bearer $GATEKEEP_TOKEN"\n\n# 或写入 .mcp.json\n${mcpJson}`}
        />
        <Snippet
          title="gk CLI"
          desc="在命令前加 gk 即可，退出码与原命令一致；需要审批时返回 75 并给出请求 id。"
          code={`export GATEKEEP_URL=${origin}\nexport GATEKEEP_TOKEN=gk_...\nexport GATEKEEP_TARGET=k8s-mgmt-1\n\ngk kubectl get pods -A\ngk -t gpu-105 journalctl -u kubelet -n 200\ngk -r "重启卡死的 kubelet" --wait 600 systemctl restart kubelet`}
        />
      </div>
    </>
  )
}

function Snippet({ title, desc, code }: { title: string; desc: string; code: string }) {
  return (
    <div className="card overflow-hidden">
      <div className="p-4">
        <div className="font-medium text-ink">{title}</div>
        <div className="mt-1 text-[13px] text-body">{desc}</div>
      </div>
      <pre className="code overflow-auto border-t border-hairline bg-canvas p-4 text-[12px] text-ink">{code}</pre>
    </div>
  )
}
