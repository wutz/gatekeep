import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { token } from './api'

// Subscribes to gatekeepd server-sent events and invalidates cached queries,
// so approval queues and request details update live.
export function useLiveUpdates(enabled: boolean) {
  const qc = useQueryClient()
  useEffect(() => {
    const tok = token.get()
    if (!enabled || !tok) return
    const es = new EventSource(`/api/v1/events?token=${encodeURIComponent(tok)}`)
    es.onmessage = () => {
      qc.invalidateQueries({ queryKey: ['requests'] })
      qc.invalidateQueries({ queryKey: ['request'] })
      qc.invalidateQueries({ queryKey: ['audit'] })
      qc.invalidateQueries({ queryKey: ['rules'] })
    }
    return () => es.close()
  }, [enabled, qc])
}

export function fmtTime(ms?: number) {
  if (!ms) return '—'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

export function fmtRelative(ms?: number) {
  if (!ms) return '—'
  const d = (Date.now() - ms) / 1000
  if (d < 60) return `${Math.max(0, Math.floor(d))} 秒前`
  if (d < 3600) return `${Math.floor(d / 60)} 分钟前`
  if (d < 86400) return `${Math.floor(d / 3600)} 小时前`
  return fmtTime(ms)
}

export function fmtDuration(start?: number, end?: number) {
  if (!start || !end) return '—'
  const ms = end - start
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}
