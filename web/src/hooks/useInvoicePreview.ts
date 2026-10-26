import { useEffect, useState } from "react"
import { api } from "@/api/client"

export function useInvoicePreview(id?: number) {
  const [preview, setPreview] = useState<{ id?: number; url: string | null; error: string | null }>({ url: null, error: null })
  useEffect(() => {
    if (!id) return
    const controller = new AbortController()
    let url: string | undefined
    setPreview({ id, url: null, error: null })
    void api.fetchFileBlob(id, controller.signal).then((blob) => {
      if (controller.signal.aborted) return
      url = URL.createObjectURL(blob)
      setPreview({ id, url, error: null })
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setPreview({ id, url: null, error: error instanceof Error ? error.message : "预览失败" })
    })
    return () => { controller.abort(); if (url) URL.revokeObjectURL(url) }
  }, [id])
  return preview.id === id ? preview : { url: null, error: null }
}
