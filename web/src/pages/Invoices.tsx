import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import {
  ChevronLeft,
  ChevronRight,
  Eye,
  FileImage,
  Pencil,
  RefreshCw,
  RotateCcw,
  Search,
  Trash2,
} from "lucide-react"

import { api } from "@/api/client"
import type { Invoice, ListParams } from "@/api/types"
import { CategoryBadge, StatusBadge } from "@/components/CategoryBadge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { CATEGORIES, formatMoney } from "@/lib/format"

const PAGE_SIZE = 15

export default function Invoices() {
  const [items, setItems] = useState<Invoice[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)

  // Filters
  const [keyword, setKeyword] = useState("")
  const [category, setCategory] = useState("")
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState("")

  // Dialog state
  const [detail, setDetail] = useState<Invoice | null>(null)
  const [editing, setEditing] = useState<Invoice | null>(null)
  const [deleting, setDeleting] = useState<Invoice | null>(null)

  const load = useCallback(async (p: ListParams) => {
    setLoading(true)
    try {
      const res = await api.listInvoices({ page_size: PAGE_SIZE, ...p })
      setItems(res.items)
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [])

  const currentParams = useMemo<ListParams>(
    () => ({
      page,
      category,
      keyword: keyword.trim(),
      start_date: startDate,
      end_date: endDate,
    }),
    [page, category, keyword, startDate, endDate]
  )

  useEffect(() => {
    load(currentParams)
  }, [currentParams, load])

  // Auto-refresh while any row is still being processed.
  useEffect(() => {
    if (!items.some((i) => i.status === "processing")) return
    const timer = setInterval(() => load(currentParams), 3000)
    return () => clearInterval(timer)
  }, [items, currentParams, load])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const applyFilters = () => {
    setPage(1)
    load({ ...currentParams, page: 1 })
  }

  const resetFilters = () => {
    setKeyword("")
    setCategory("")
    setStartDate("")
    setEndDate("")
    setPage(1)
  }

  const doDelete = async () => {
    if (!deleting) return
    try {
      await api.deleteInvoice(deleting.id)
      toast.success("已删除")
      setDeleting(null)
      load(currentParams)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败")
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Filter bar */}
      <Card>
        <CardContent className="flex flex-wrap items-end gap-3 pt-6">
          <div className="flex flex-col gap-1.5">
            <Label>关键词</Label>
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
              <Input
                className="w-56 pl-8"
                placeholder="号码 / 销售方 / 购买方 / 备注"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && applyFilters()}
              />
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>分类</Label>
            <Select value={category} onValueChange={(v) => { setCategory(v === "__all__" ? "" : v); setPage(1) }}>
              <SelectTrigger className="w-36">
                <SelectValue placeholder="全部分类" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">全部分类</SelectItem>
                {CATEGORIES.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>开始日期</Label>
            <Input type="date" className="w-40" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>结束日期</Label>
            <Input type="date" className="w-40" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
          </div>
          <Button onClick={applyFilters}>
            <Search className="size-4" /> 查询
          </Button>
          <Button variant="outline" onClick={resetFilters}>
            <RotateCcw className="size-4" /> 重置
          </Button>
          <Button variant="ghost" onClick={() => load(currentParams)} title="刷新">
            <RefreshCw className="size-4" />
          </Button>
        </CardContent>
      </Card>

      {/* Table */}
      <Card>
        <CardContent className="pt-6">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>开票日期</TableHead>
                <TableHead>发票号码</TableHead>
                <TableHead>销售方</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((inv) => (
                <TableRow key={inv.id}>
                  <TableCell>{inv.invoice_date || "—"}</TableCell>
                  <TableCell className="max-w-40 truncate" title={inv.invoice_number}>
                    {inv.invoice_number || "—"}
                  </TableCell>
                  <TableCell className="max-w-52 truncate" title={inv.seller_name}>
                    {inv.seller_name || "—"}
                  </TableCell>
                  <TableCell className="text-right font-medium">
                    {inv.status === "done" ? formatMoney(inv.amount, inv.currency) : "—"}
                  </TableCell>
                  <TableCell>
                    <CategoryBadge value={inv.category} />
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={inv.status} error={inv.error_message} />
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button variant="ghost" size="icon" title="详情" onClick={() => setDetail(inv)}>
                        <Eye className="size-4" />
                      </Button>
                      <Button variant="ghost" size="icon" title="编辑" onClick={() => setEditing(inv)}>
                        <Pencil className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        title="删除"
                        className="text-destructive hover:text-destructive"
                        onClick={() => setDeleting(inv)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                    {loading ? "加载中…" : "暂无发票，去「导入发票」上传吧"}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>

          {/* Pagination */}
          <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
            <span>
              共 {total} 条 · 第 {page}/{totalPages} 页
            </span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="size-4" /> 上一页
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
              >
                下一页 <ChevronRight className="size-4" />
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <DetailDialog invoice={detail} onClose={() => setDetail(null)} onChanged={() => load(currentParams)} />
      <EditDialog
        invoice={editing}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null)
          load(currentParams)
        }}
      />

      {/* Delete confirmation */}
      <Dialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              将删除发票「{deleting?.file_name}」及其原始文件，此操作不可恢复。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              取消
            </Button>
            <Button variant="destructive" onClick={doDelete}>
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ---------- Detail dialog with original file preview ----------

function DetailDialog({
  invoice,
  onClose,
  onChanged,
}: {
  invoice: Invoice | null
  onClose: () => void
  onChanged: () => void
}) {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const isPdf = invoice?.file_name.toLowerCase().endsWith(".pdf") ?? false

  useEffect(() => {
    if (!invoice) {
      setPreviewUrl(null)
      return
    }
    let revoked: string | null = null
    api
      .fetchFileBlob(invoice.id)
      .then((blob) => {
        revoked = URL.createObjectURL(blob)
        setPreviewUrl(revoked)
      })
      .catch(() => setPreviewUrl(null))
    return () => {
      if (revoked) URL.revokeObjectURL(revoked)
    }
  }, [invoice])

  if (!invoice) return null

  const reExtract = async () => {
    try {
      await api.reExtract(invoice.id)
      toast.success("已重新提交提取")
      onChanged()
      onClose()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "操作失败")
    }
  }

  const fields: [string, React.ReactNode][] = [
    ["开票日期", invoice.invoice_date || "—"],
    ["发票号码", invoice.invoice_number || "—"],
    ["金额", invoice.status === "done" ? formatMoney(invoice.amount, invoice.currency) : "—"],
    ["销售方", invoice.seller_name || "—"],
    ["购买方", invoice.buyer_name || "—"],
    ["开票人", invoice.drawer || "—"],
    ["分类", <CategoryBadge key="cat" value={invoice.category} />],
    ["状态", <StatusBadge key="st" status={invoice.status} error={invoice.error_message} />],
    ["备注", invoice.notes || "—"],
    ["导入时间", new Date(invoice.created_at).toLocaleString("zh-CN")],
  ]

  return (
    <Dialog open={!!invoice} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>发票详情 · {invoice.file_name}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="flex min-h-72 items-center justify-center overflow-hidden rounded-md border bg-muted/30">
            {previewUrl ? (
              isPdf ? (
                <iframe src={previewUrl} title="发票预览" className="h-96 w-full" />
              ) : (
                <img src={previewUrl} alt="发票" className="max-h-96 w-full object-contain" />
              )
            ) : (
              <div className="flex flex-col items-center gap-2 text-muted-foreground">
                <FileImage className="size-8" />
                <span className="text-sm">预览加载中…</span>
              </div>
            )}
          </div>
          <div className="flex flex-col gap-2">
            {fields.map(([label, value]) => (
              <div key={label} className="flex items-start justify-between gap-4 border-b pb-1.5 text-sm">
                <span className="shrink-0 text-muted-foreground">{label}</span>
                <span className="break-all text-right">{value}</span>
              </div>
            ))}
            {invoice.status === "failed" && invoice.error_message && (
              <div className="rounded-md bg-destructive/10 p-2 text-xs text-destructive">
                {invoice.error_message}
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={reExtract}>
            <RefreshCw className="size-4" /> 重新提取
          </Button>
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------- Edit dialog ----------

function EditDialog({
  invoice,
  onClose,
  onSaved,
}: {
  invoice: Invoice | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form, setForm] = useState({
    invoice_number: "",
    invoice_date: "",
    amount: "",
    seller_name: "",
    buyer_name: "",
    drawer: "",
    category: "other",
    notes: "",
  })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (invoice) {
      setForm({
        invoice_number: invoice.invoice_number,
        invoice_date: invoice.invoice_date,
        amount: invoice.amount ? invoice.amount.toFixed(2) : "",
        seller_name: invoice.seller_name,
        buyer_name: invoice.buyer_name,
        drawer: invoice.drawer,
        category: invoice.category,
        notes: invoice.notes,
      })
    }
  }, [invoice])

  if (!invoice) return null

  const set = (key: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }))

  const save = async () => {
    const amount = form.amount.trim() === "" ? 0 : Number(form.amount)
    if (Number.isNaN(amount) || amount < 0) {
      toast.error("金额必须是非负数字")
      return
    }
    setSaving(true)
    try {
      await api.updateInvoice(invoice.id, {
        invoice_number: form.invoice_number.trim(),
        invoice_date: form.invoice_date.trim(),
        amount,
        seller_name: form.seller_name.trim(),
        buyer_name: form.buyer_name.trim(),
        drawer: form.drawer.trim(),
        category: form.category,
        notes: form.notes,
      })
      toast.success("已保存")
      onSaved()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={!!invoice} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>编辑发票 · {invoice.file_name}</DialogTitle>
          <DialogDescription>修正提取有误的字段后保存</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-3">
          <Field label="开票日期">
            <Input type="date" value={form.invoice_date} onChange={set("invoice_date")} />
          </Field>
          <Field label="发票号码">
            <Input value={form.invoice_number} onChange={set("invoice_number")} />
          </Field>
          <Field label="金额（元）">
            <Input type="number" step="0.01" min="0" value={form.amount} onChange={set("amount")} />
          </Field>
          <Field label="分类">
            <Select value={form.category} onValueChange={(v) => setForm((f) => ({ ...f, category: v }))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CATEGORIES.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="销售方">
            <Input value={form.seller_name} onChange={set("seller_name")} />
          </Field>
          <Field label="购买方">
            <Input value={form.buyer_name} onChange={set("buyer_name")} />
          </Field>
          <Field label="开票人">
            <Input value={form.drawer} onChange={set("drawer")} />
          </Field>
          <div className="col-span-2">
            <Field label="备注">
              <Textarea value={form.notes} onChange={set("notes")} rows={2} />
            </Field>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={saving}>
            {saving ? "保存中…" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  )
}
