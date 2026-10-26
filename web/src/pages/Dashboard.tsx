import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"

import { api } from "@/api/client"
import type { StatItem, Summary } from "@/api/types"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { CATEGORIES, CURRENCIES, categoryLabel, formatMoney } from "@/lib/format"

const CHART_COLORS = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
]

export default function Dashboard() {
  const [summaries, setSummaries] = useState<Summary[]>([])
  const [currency, setCurrency] = useState<string>("CNY")
  const [monthly, setMonthly] = useState<StatItem[]>([])
  const [byCategory, setByCategory] = useState<StatItem[]>([])
  const [bySeller, setBySeller] = useState<StatItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const controller = new AbortController()
    api
      .summary(controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return
        setSummaries(result.items)
        // Default to the currency that actually holds data.
        if (result.items.length > 0 && !result.items.some((item) => item.currency === currency)) {
          setCurrency(result.items[0].currency)
        }
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          toast.error("加载统计数据失败: " + (err instanceof Error ? err.message : ""))
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [currency])

  // Amounts are never converted: every chart shows exactly one currency.
  useEffect(() => {
    const controller = new AbortController()
    Promise.all([
      api.stats("month", currency, controller.signal),
      api.stats("category", currency, controller.signal),
      api.stats("seller", currency, controller.signal),
    ])
      .then(([m, c, sel]) => {
        if (controller.signal.aborted) return
        setMonthly(m.items)
        setByCategory(c.items)
        setBySeller(sel.items.slice(0, 10))
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          toast.error("加载统计数据失败: " + (err instanceof Error ? err.message : ""))
        }
      })
    return () => controller.abort()
  }, [currency])

  const summary = summaries.find((item) => item.currency === currency)

  const categoryData = useMemo(
    () => byCategory.map((c) => ({ ...c, name: categoryLabel(c.period) })),
    [byCategory]
  )

  const cards = [
    { label: "累计金额", value: formatMoney(summary?.total_amount ?? 0, currency) },
    { label: "累计发票张数", value: `${summary?.total_count ?? 0} 张` },
    { label: "本月金额", value: formatMoney(summary?.month_amount ?? 0, currency) },
    { label: "本月张数", value: `${summary?.month_count ?? 0} 张` },
  ]

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">概览</h2>
          <p className="text-sm text-muted-foreground">
            统计仅含已确认发票，币种之间不做折算
          </p>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">币种</span>
          <select
            aria-label="统计币种"
            className="h-9 rounded-md border bg-background px-2"
            value={currency}
            onChange={(event) => setCurrency(event.target.value)}
          >
            {CURRENCIES.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
        </label>
      </div>

      {(summary?.review_count ?? 0) > 0 && (
        <Card className="border-amber-300 bg-amber-50 dark:bg-amber-950/30">
          <CardContent className="flex flex-wrap items-center justify-between gap-2 pt-6 text-sm">
            <span>
              有 <strong>{summary?.review_count}</strong> 张 {currency} 发票字段不完整，未计入统计
            </span>
            <a className="font-medium underline" href="/invoices?status=needs_review">
              去复核
            </a>
          </CardContent>
        </Card>
      )}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {cards.map((c) => (
          <Card key={c.label} className="gap-2 py-4">
            <CardHeader className="pb-0">
              <CardDescription>{c.label}</CardDescription>
            </CardHeader>
            <CardContent>
              {loading ? (
                <Skeleton className="h-8 w-24" />
              ) : (
                <div className="text-2xl font-bold tracking-tight">{c.value}</div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>月度趋势</CardTitle>
            <CardDescription>按开票月份汇总金额</CardDescription>
          </CardHeader>
          <CardContent className="h-72">
            {monthly.length === 0 && !loading ? (
              <EmptyHint />
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={monthly}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="period" fontSize={12} tickLine={false} axisLine={false} />
                  <YAxis fontSize={12} tickLine={false} axisLine={false} />
                    <Tooltip formatter={(value) => [formatMoney(Number(value), currency), "金额"]} />
                  <Bar dataKey="total" fill="var(--color-chart-2)" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>分类占比</CardTitle>
            <CardDescription>按发票分类汇总金额</CardDescription>
          </CardHeader>
          <CardContent className="h-72">
            {categoryData.length === 0 && !loading ? (
              <EmptyHint />
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={categoryData}
                    dataKey="total"
                    nameKey="name"
                    innerRadius="45%"
                    outerRadius="75%"
                    paddingAngle={2}
                  >
                    {categoryData.map((_, i) => (
                      <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    formatter={(value) => [formatMoney(Number(value), currency), currency]}
                    labelFormatter={(label) => `分类：${label}`}
                  />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>销售方排行</CardTitle>
          <CardDescription>按开票方汇总金额（前 10 名，{currency}）</CardDescription>
        </CardHeader>
        <CardContent>
          {bySeller.length === 0 && !loading ? (
            <EmptyHint />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>销售方</TableHead>
                  <TableHead className="text-right">张数</TableHead>
                  <TableHead className="text-right">金额</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {bySeller.map((s, i) => (
                  <TableRow key={s.period + i}>
                    <TableCell className="max-w-0 truncate">{s.period}</TableCell>
                    <TableCell className="text-right">{s.count}</TableCell>
                    <TableCell className="text-right font-medium">{formatMoney(s.total, currency)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function EmptyHint() {
  return (
    <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
      暂无数据，先去导入发票吧
    </div>
  )
}
