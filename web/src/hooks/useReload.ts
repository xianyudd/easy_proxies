import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { getReloadStatus, reloadCore } from '../api/configNodes'
import { useToast } from '../components/ui/Toast'
import type { ReloadCoreResponse } from '../types/configNode'
import type { ReloadStatus } from '../types/settings'

type ReloadState = 'idle' | 'reloading' | 'failed'

interface UseReloadOptions {
  /** Unique key segment so multiple pages don't share one poll query. */
  scope: string
  /** Poll interval while reloading (ms). */
  pollInterval?: number
  /** Toast message when reload succeeds — string or a fn of the final status. */
  successMessage?: string | ((status?: ReloadStatus) => string)
  /** Toast message when a reload is accepted — string or a fn of the start response. */
  startMessage?: string | ((res: ReloadCoreResponse) => string)
  /** Fired once when a reload finishes successfully — invalidate/refetch here. */
  onSucceeded?: () => void
}

/**
 * Shared reload state machine: tracks whether a reload is pending, starts a
 * core reload, and polls status until it succeeds or fails. Replaces the
 * per-page copies that lived in RegionReview / NodeConfig / Quality / Settings.
 */
export function useReload({ scope, pollInterval = 800, successMessage = '重载已完成', startMessage, onSucceeded }: UseReloadOptions) {
  const toast = useToast(s => s.show)
  const [needReload, setNeedReload] = useState(false)
  const [reloadState, setReloadState] = useState<ReloadState>('idle')

  const reloadStatus = useQuery({
    queryKey: ['reload-status', scope],
    queryFn: getReloadStatus,
    enabled: reloadState === 'reloading',
    refetchInterval: reloadState === 'reloading' ? pollInterval : false,
    // Don't keep a finished run's status around — a remount would otherwise
    // replay it through the effect below.
    gcTime: 0,
  })

  useEffect(() => {
    // Only act on a status that belongs to a reload we started: the query keeps
    // serving its last value after it goes idle, so an unguarded effect would
    // re-toast success (and re-run onSucceeded) on every remount.
    if (reloadState !== 'reloading') return
    const state = String(reloadStatus.data?.state || '')
    if (state === 'succeeded') {
      setReloadState('idle')
      setNeedReload(false)
      const message = typeof successMessage === 'function' ? successMessage(reloadStatus.data) : successMessage
      toast(message, 'ok')
      onSucceeded?.()
    } else if (state === 'failed') {
      setReloadState('failed')
      toast(reloadStatus.data?.error ? `重载失败：${reloadStatus.data.error}` : '重载失败', 'error')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadState, reloadStatus.data?.state, reloadStatus.data?.duration_ms, reloadStatus.data?.error])

  const startReload = useMutation({
    mutationFn: reloadCore,
    onSuccess: res => {
      const message = typeof startMessage === 'function'
        ? startMessage(res)
        : startMessage || res?.message || '重载已在后台启动'
      toast(message, 'ok')
      // Reload may finish synchronously (small configs) — settle immediately.
      if (res?.reload_status?.state === 'succeeded') {
        setReloadState('idle')
        setNeedReload(false)
        onSucceeded?.()
        return
      }
      setReloadState('reloading')
      void reloadStatus.refetch()
    },
    onError: error => {
      setReloadState('failed')
      toast(error instanceof Error ? error.message : '重载启动失败', 'error')
    },
  })

  return {
    needReload,
    setNeedReload,
    reloadState,
    isReloading: reloadState === 'reloading',
    startReload,
    status: reloadStatus.data,
    reloadStatusError: reloadStatus.isError ? reloadStatus.error : undefined,
    refetchReloadStatus: () => { void reloadStatus.refetch() },
  }
}
