import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { Receipt } from "lucide-react"

import { api, setToken } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export default function Login() {
  const navigate = useNavigate()
  const [token, setTokenValue] = useState("")
  const [loading, setLoading] = useState(false)

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!token.trim()) {
      toast.error("请输入访问 Token")
      return
    }
    setLoading(true)
    try {
      const res = await api.login(token.trim())
      setToken(res.token)
      toast.success("登录成功")
      navigate("/", { replace: true })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "登录失败")
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <div className="mx-auto mb-2 flex size-12 items-center justify-center rounded-full bg-primary/10">
            <Receipt className="size-6 text-primary" />
          </div>
          <CardTitle className="text-xl">Sorter 发票管理</CardTitle>
          <CardDescription>请输入预置的访问 Token 进入系统</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="token">访问 Token</Label>
              <Input
                id="token"
                type="password"
                placeholder="••••••••••••"
                autoComplete="off"
                autoFocus
                value={token}
                onChange={(e) => setTokenValue(e.target.value)}
              />
            </div>
            <Button type="submit" disabled={loading} className="w-full">
              {loading ? "验证中…" : "进入系统"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
