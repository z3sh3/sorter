import { Badge } from "@/components/ui/badge"
import { categoryLabel } from "@/lib/format"
import { cn } from "@/lib/utils"

// categoryColors maps each category to a distinctive badge palette.
const categoryColors: Record<string, string> = {
  transport: "border-transparent bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300",
  dining: "border-transparent bg-orange-100 text-orange-800 dark:bg-orange-900/40 dark:text-orange-300",
  accommodation: "border-transparent bg-purple-100 text-purple-800 dark:bg-purple-900/40 dark:text-purple-300",
  tech_service: "border-transparent bg-teal-100 text-teal-800 dark:bg-teal-900/40 dark:text-teal-300",
  other: "border-transparent bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400",
}

export function CategoryBadge({ value }: { value: string }) {
  return (
    <Badge className={cn(categoryColors[value] ?? categoryColors.other)}>
      {categoryLabel(value)}
    </Badge>
  )
}

export function StatusBadge({ status, error }: { status: string; error?: string }) {
  if (status === "done") {
    return (
      <Badge className="border-transparent bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300">
        已提取
      </Badge>
    )
  }
  if (status === "failed") {
    return (
      <Badge variant="destructive" title={error}>
        失败
      </Badge>
    )
  }
  return (
    <Badge className="border-transparent bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
      提取中…
    </Badge>
  )
}
