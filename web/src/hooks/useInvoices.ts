import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { api } from "@/api/client"
import type { Invoice, ListParams } from "@/api/types"

// Requests are serialized within each effect and canceled on query changes.
export function useInvoices(params: ListParams) {
  const [items, setItems] = useState<Invoice[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision((value) => value + 1), [])
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    setLoading(true)
    const load = async () => {
      try {
        const result = await api.listInvoices(params, controller.signal)
        if (controller.signal.aborted) return
        setItems(result.items)
        setTotal(result.total)
        if (result.items.some((item) => item.status === "processing")) timer = setTimeout(load, 3000)
      } catch (error) {
        if (!controller.signal.aborted) toast.error(error instanceof Error ? error.message : "加载失败")
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [params, revision])
  return { items, total, loading, refresh }
}
