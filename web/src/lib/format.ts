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

// CURRENCIES mirrors the currencies accepted by the backend.
export const CURRENCIES = [
  "CNY",
  "USD",
  "EUR",
  "GBP",
  "HKD",
  "JPY",
  "CAD",
  "AUD",
  "SGD",
  "CHF",
  "KRW",
  "NZD",
] as const

export type Currency = (typeof CURRENCIES)[number]

export const STATUS_LABELS: Record<string, string> = {
  processing: "提取中",
  done: "已提取",
  needs_review: "待复核",
  failed: "失败",
}

const CURRENCY_SYMBOLS: Record<string, string> = {
  CNY: "¥",
  USD: "US$",
  EUR: "€",
  GBP: "£",
  HKD: "HK$",
  JPY: "JP¥",
  KRW: "₩",
}

// formatMoney renders an amount with its currency marker and two decimals.
// Currencies are never converted; the marker makes the unit explicit.
export function formatMoney(amount: number, currency = "CNY"): string {
  const symbol = CURRENCY_SYMBOLS[currency] ?? currency + " "
  const digits = currency === "JPY" || currency === "KRW" ? 0 : 2
  const value = amount.toLocaleString("zh-CN", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
  return symbol + value
}
