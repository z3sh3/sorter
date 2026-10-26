import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { FileUp, Loader2, RefreshCw, UploadCloud, X } from "lucide-react"

import { api, ApiError } from "@/api/client"
import type { UploadResult } from "@/api/types"
import { CategoryBadge, StatusBadge } from "@/components/CategoryBadge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { formatMoney } from "@/lib/format"
import type { Invoice } from "@/api/types"

// TrackedItem combines the upload result with the live invoice record.
interface TrackedItem {
  result: UploadResult
  invoice?: Invoice
  done: boolean
}

const ACCEPTED = ".jpg,.jpeg,.png,.webp,.pdf"

export default function Import() {
  const [files, setFiles] = useState<File[]>([])
  const [dragging, setDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [tracked, setTracked] = useState<TrackedItem[]>([])
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = useCallback((list: FileList | File[]) => {
    const incoming = Array.from(list)
    setFiles((prev) => {
      const seen = new Set(prev.map((f) => f.name + f.size))
      return [...prev, ...incoming.filter((f) => !seen.has(f.name + f.size))]
    })
  }, [])

  // Poll sequentially: a new round only starts after the previous one finished,
  // and the request is canceled when the pending set changes or the page unmounts.
  const pendingKey = tracked
    .flatMap((t) => (!t.done && t.result.invoice_id ? [t.result.invoice_id] : []))
    .join(",")
  useEffect(() => {
    const ids = pendingKey ? pendingKey.split(",").map(Number) : []
    if (ids.length === 0) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      const finished = new Map<number, Invoice>()
      for (const id of ids) {
        if (controller.signal.aborted) return
        try {
          const inv = await api.getInvoice(id, controller.signal)
          if (inv.status !== "processing") finished.set(id, inv)
        } catch (err) {
          if (controller.signal.aborted) return
          // Missing or unauthorized records will never become available by polling.
          if (err instanceof ApiError && (err.status === 404 || err.status === 401)) return
        }
      }
      // Apply one state update per round instead of one render per invoice.
      if (!controller.signal.aborted && finished.size > 0) {
        setTracked((prev) =>
          prev.map((t) => {
            const inv = t.result.invoice_id ? finished.get(t.result.invoice_id) : undefined
            return inv ? { ...t, invoice: inv, done: true } : t
          })
        )
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 2000)
    }
    void poll()
    return () => {
      controller.abort()
      clearTimeout(timer)
    }
  }, [pendingKey])

  const doUpload = async () => {
    if (files.length === 0) return
    setUploading(true)
    try {
      const { results } = await api.upload(files)
      const items: TrackedItem[] = results.map((r) => ({
        result: r,
        done: r.status !== "queued",
      }))
      setTracked((prev) => [...items, ...prev])
      const queued = results.filter((r) => r.status === "queued").length
      const dup = results.filter((r) => r.status === "duplicate").length
      const failed = results.filter((r) => r.status === "error").length
      if (queued > 0) toast.success(`已提交 ${queued} 个文件，正在后台提取`)
      if (dup > 0) toast.warning(`${dup} 个文件重复，已跳过`)
      if (failed > 0) toast.error(`${failed} 个文件上传失败`)
      setFiles([])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "上传失败")
    } finally {
      setUploading(false)
    }
  }

  const retry = async (invoiceId: number, version: number) => {
    try {
      await api.reExtract(invoiceId, version)
      setTracked((prev) =>
        prev.map((t) =>
          t.result.invoice_id === invoiceId
            ? { ...t, done: false, invoice: undefined }
            : t
        )
      )
      toast.success("已重新提交提取")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "操作失败")
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle>导入发票</CardTitle>
          <CardDescription>
            支持 JPG / PNG / WebP 图片和 PDF 电子发票，可一次选择多个文件
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div
            role="button"
            tabIndex={0}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => e.key === "Enter" && inputRef.current?.click()}
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              addFiles(e.dataTransfer.files)
            }}
            className={cn(
              "flex min-h-40 cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 text-center transition-colors",
              dragging ? "border-primary bg-primary/5" : "border-border hover:border-primary/50"
            )}
          >
            <UploadCloud className="size-10 text-muted-foreground" />
            <div className="text-sm font-medium">点击选择或拖拽文件到此处</div>
            <div className="text-xs text-muted-foreground">单个文件不超过 32MB</div>
            <input
              ref={inputRef}
              type="file"
              multiple
              accept={ACCEPTED}
              className="hidden"
              onChange={(e) => {
                if (e.target.files) addFiles(e.target.files)
                e.target.value = ""
              }}
            />
          </div>

          {files.length > 0 && (
            <div className="flex flex-col gap-2">
              {files.map((f) => (
                <div
                  key={f.name + f.size}
                  className="flex items-center justify-between rounded-md border px-3 py-2 text-sm"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <FileUp className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{f.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {(f.size / 1024).toFixed(0)} KB
                    </span>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    onClick={() => setFiles((prev) => prev.filter((x) => x !== f))}
                  >
                    <X className="size-4" />
                  </Button>
                </div>
              ))}
              <Button onClick={doUpload} disabled={uploading} className="self-end">
                {uploading && <Loader2 className="size-4 animate-spin" />}
                开始导入（{files.length} 个文件）
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {tracked.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>处理进度</CardTitle>
            <CardDescription>提取结果会实时更新，完成后可前往发票列表查看</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {tracked.map((t, i) => (
              <TrackedRow key={i} item={t} onRetry={retry} />
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function TrackedRow({ item, onRetry }: { item: TrackedItem; onRetry: (id: number, version: number) => void }) {
  const { result, invoice, done } = item

  if (result.status === "duplicate") {
    return (
      <RowShell name={result.file_name}>
        <StatusBadge status="failed" />
        <span className="text-sm text-muted-foreground">文件重复，已跳过</span>
      </RowShell>
    )
  }
  if (result.status === "error") {
    return (
      <RowShell name={result.file_name}>
        <StatusBadge status="failed" />
        <span className="text-sm text-destructive">{result.error}</span>
      </RowShell>
    )
  }

  const processing = !done
  const failed = done && invoice?.status === "failed"
  const needsReview = done && invoice?.status === "needs_review"

  return (
    <RowShell name={result.file_name}>
      {processing ? (
        <>
          <Loader2 className="size-4 animate-spin text-amber-500" />
          <span className="text-sm text-muted-foreground">正在提取…</span>
        </>
      ) : failed ? (
        <>
          <StatusBadge status="failed" />
          <span className="flex-1 truncate text-sm text-destructive" title={invoice?.error_message}>
            {invoice?.error_message}
          </span>
          <Button variant="outline" size="sm" onClick={() => onRetry(result.invoice_id!, invoice.version)}>
            <RefreshCw className="size-3.5" /> 重试
          </Button>
        </>
      ) : needsReview ? (
        <>
          <StatusBadge status="needs_review" />
          <CategoryBadge value={invoice.category} />
          <span className="flex-1 truncate text-sm text-amber-700" title={invoice.review_reason}>
            {invoice.review_reason || "字段不完整，请在发票列表中核对"}
          </span>
        </>
      ) : invoice ? (
        <>
          <StatusBadge status={invoice.status} />
          <CategoryBadge value={invoice.category} />
          <span className="text-sm">
            {invoice.invoice_date || "—"} · {invoice.seller_name || "未知销售方"} ·{" "}
            <span className="font-medium">{formatMoney(invoice.amount, invoice.currency)}</span>
          </span>
        </>
      ) : null}
    </RowShell>
  )
}

function RowShell({ name, children }: { name: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-md border px-3 py-2">
      <span className="max-w-48 truncate text-sm font-medium" title={name}>
        {name}
      </span>
      <div className="flex flex-1 items-center gap-2 justify-end">{children}</div>
    </div>
  )
}
