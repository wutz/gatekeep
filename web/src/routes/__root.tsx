/// <reference types="vite/client" />
import { useEffect, useState, type ReactNode } from 'react'
import { HeadContent, Link, Outlet, Scripts, createRootRouteWithContext } from '@tanstack/react-router'
import { QueryClientProvider, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api, token, type Me } from '~/lib/api'
import { useLiveUpdates } from '~/lib/hooks'
import { queryClient } from '~/router'
import appCss from '~/styles.css?url'

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'gatekeep · 生产环境访问控制' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  shellComponent: Shell,
  component: Root,
})

function Shell({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function Root() {
  return (
    <QueryClientProvider client={queryClient}>
      <Gate />
    </QueryClientProvider>
  )
}

export function useMe() {
  return useQuery({ queryKey: ['me'], queryFn: () => api.me(), staleTime: Infinity, retry: false })
}

function Gate() {
  // Token lives in localStorage; track it in state so login/logout re-render.
  const [hasToken, setHasToken] = useState(() => !!token.get())
  useEffect(() => {
    setHasToken(!!token.get())
  }, [])
  if (!hasToken) return <Login onLogin={() => setHasToken(true)} />
  return <Authed onInvalid={() => setHasToken(false)} />
}

function Authed({ onInvalid }: { onInvalid: () => void }) {
  const me = useMe()
  if (me.isError) {
    return <Login error={me.error?.message} onLogin={() => me.refetch()} />
  }
  if (!me.data) return <div className="p-10 text-mute">加载中…</div>
  if (me.data.kind !== 'human') {
    token.clear()
    onInvalid()
  }
  return <App me={me.data} />
}

function Login({ error, onLogin }: { error?: string; onLogin: () => void }) {
  const qc = useQueryClient()
  const [tok, setTok] = useState('')
  const [err, setErr] = useState(error)
  const [busy, setBusy] = useState(false)
  async function submit(e: { preventDefault(): void }) {
    e.preventDefault()
    setBusy(true)
    try {
      const me = await api.me(tok.trim())
      if (me.kind !== 'human') throw new Error('该令牌属于 Agent，不能登录控制台')
      token.set(tok.trim())
      qc.setQueryData(['me'], me)
      setErr(undefined)
      onLogin()
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden px-4">
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 h-[520px] w-[900px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{ background: 'radial-gradient(closest-side, #5eead4, transparent), radial-gradient(closest-side at 70% 60%, #0070f3, transparent)' }}
      />
      <form onSubmit={submit} className="card relative w-full max-w-sm p-8">
        <Logo />
        <h1 className="mt-6 text-[24px] font-semibold tracking-[-0.8px]">登录控制台</h1>
        <p className="mt-1 text-body">使用你的 gatekeep 访问令牌登录。</p>
        <label className="eyebrow mt-6 mb-2 block">Access token</label>
        <input className="input code" type="password" autoComplete="current-password" autoFocus value={tok} onChange={(e) => setTok(e.target.value)} placeholder="gk_…" />
        {err && <div className="mt-3 text-[13px] text-error">{err}</div>}
        <button className="btn btn-primary mt-6 w-full justify-center" disabled={!tok || busy}>
          {busy ? '验证中…' : '继续'}
        </button>
      </form>
    </div>
  )
}

function Logo() {
  return (
    <Link to="/" className="flex items-center gap-2 font-semibold tracking-[-0.3px] text-ink">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden>
        <rect x="2" y="2" width="20" height="20" rx="6" fill="#171717" />
        <path d="M8 17V10a4 4 0 0 1 8 0v7" stroke="#5eead4" strokeWidth="2" strokeLinecap="round" />
        <circle cx="12" cy="14" r="1.5" fill="#fff" />
      </svg>
      gatekeep
    </Link>
  )
}

const roleLabel = { viewer: '只读', operator: '运维', admin: '管理员' } as const

function App({ me }: { me: Me }) {
  useLiveUpdates(true)
  const qc = useQueryClient()
  const pending = useQuery({ queryKey: ['requests', { status: 'pending' }], queryFn: () => api.requests({ status: 'pending' }) })
  const nav: { to: '/' | '/requests' | '/run' | '/commands' | '/rules' | '/audit' | '/targets'; label: string; badge?: number }[] = [
    { to: '/', label: '审批', badge: pending.data?.length },
    { to: '/requests', label: '操作记录' },
    { to: '/run', label: '执行命令' },
    { to: '/commands', label: '自定义命令' },
    { to: '/rules', label: '分级规则' },
    { to: '/audit', label: '审计日志' },
    { to: '/targets', label: '目标与接入' },
  ]
  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-10 border-b border-hairline bg-canvas/80 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-6">
          <Logo />
          <nav className="flex items-center gap-1">
            {nav.map((n) => (
              <Link
                key={n.to}
                to={n.to}
                activeOptions={{ exact: n.to === '/' }}
                className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-body transition-colors hover:text-ink"
                activeProps={{ className: 'bg-hairline-soft !text-ink font-medium' }}
              >
                {n.label}
                {!!n.badge && (
                  <span className="code rounded-full bg-warning px-1.5 text-[11px] leading-4 font-semibold text-white">{n.badge}</span>
                )}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <span className="text-body">
              {me.name} <span className="code ml-1 rounded-[4px] bg-accent-soft px-1.5 py-0.5 text-[11px] text-accent-deep">{roleLabel[me.role ?? 'viewer']}</span>
            </span>
            <button
              className="btn h-7 px-2 text-[13px]"
              onClick={() => {
                token.clear()
                qc.clear()
                location.href = '/'
              }}
            >
              退出
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-10">
        <Outlet />
      </main>
    </div>
  )
}
