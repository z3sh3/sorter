import { Navigate, Route, Routes } from "react-router-dom"

import { getToken } from "@/api/client"
import Layout from "@/components/Layout"
import Dashboard from "@/pages/Dashboard"
import Import from "@/pages/Import"
import Invoices from "@/pages/Invoices"
import Login from "@/pages/Login"

// RequireAuth redirects to the login page when no session token exists.
function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!getToken()) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="import" element={<Import />} />
        <Route path="invoices" element={<Invoices />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
