import { useState } from "react"
import { NavLink, Outlet, useNavigate } from "react-router-dom"
import { FileText, LayoutDashboard, LogOut, Moon, Sun, Upload } from "lucide-react"

import { clearToken } from "@/api/client"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

const navItems = [
  { to: "/", label: "概览", icon: LayoutDashboard, end: true },
  { to: "/import", label: "导入发票", icon: Upload, end: false },
  { to: "/invoices", label: "发票列表", icon: FileText, end: false },
]

export default function Layout() {
  const navigate = useNavigate()
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"))

  const toggleTheme = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle("dark", next)
    localStorage.setItem("sorter_theme", next ? "dark" : "light")
  }

  const logout = () => {
    clearToken()
    navigate("/login", { replace: true })
  }

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4">
          <div className="flex items-center gap-6">
            <span className="text-lg font-bold tracking-tight">🧾 Sorter</span>
            <nav className="flex items-center gap-1">
              {navItems.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-accent text-accent-foreground"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                    )
                  }
                >
                  <item.icon className="size-4" />
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </div>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="icon" onClick={toggleTheme} title="切换主题">
              {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
            </Button>
            <Button variant="ghost" size="icon" onClick={logout} title="退出登录">
              <LogOut className="size-4" />
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
