// Shared display constants and formatters.

export const CATEGORIES = [
  { value: "transport", label: "交通" },
  { value: "dining", label: "餐饮" },
  { value: "accommodation", label: "住宿" },
  { value: "tech_service", label: "技术服务费" },
  { value: "other", label: "其他" },
] as const

export function categoryLabel(value: string): string {
  return CATEGORIES.find((c) => c.value === value)?.label ?? value
}

export const STATUS_LABELS: Record<string, string> = {
  processing: "提取中",
  done: "已提取",
  failed: "失败",
}

// formatMoney renders a yuan amount with currency symbol and 2 decimals.
export function formatMoney(amount: number, currency = "CNY"): string {
  const symbol = currency === "CNY" ? "¥" : currency + " "
  return symbol + amount.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
