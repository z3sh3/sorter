import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter } from "react-router-dom"
import { Toaster } from "sonner"

import App from "./App"
import "./index.css"

// Apply the persisted theme before first paint.
const storedTheme = localStorage.getItem("sorter_theme")
if (storedTheme === "dark" || (!storedTheme && window.matchMedia("(prefers-color-scheme: dark)").matches)) {
  document.documentElement.classList.add("dark")
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
      <Toaster richColors position="top-center" />
    </BrowserRouter>
  </StrictMode>
)
