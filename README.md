# Sorter · 发票管理工具

单一二进制部署的发票管理系统：上传发票（图片 / PDF），通过多模态 LLM 自动提取
金额、开票日期、开票人、销售方等信息并自动分类，支持多维度统计查询。

## 功能

- **智能导入**：多模态 LLM（OpenAI 兼容接口）单次调用完成 OCR 提取 + 自动分类
  （交通 / 餐饮 / 住宿 / 技术服务费 / 其他），PDF 通过内嵌 PDFium（WebAssembly）渲染
- **多维统计**：按日 / 月 / 年 / 销售方 / 分类聚合，Dashboard 图表可视化
- **发票管理**：关键词搜索、日期范围 / 分类筛选、人工修正、失败重试、原文件预览
- **Token 认证**：无用户名密码，预置加密 token 验证，登录签发 JWT（7 天）
- **一键部署**：前端嵌入 Go 二进制，免 CGO（modernc.org/sqlite + wazero），
  交叉编译开箱即用

## 技术栈

| 层 | 技术 | 实际版本 |
| --- | --- | --- |
| 后端 | Go · Gin · golang-jwt · yaml.v3 | Go 1.27.2 · Gin 1.12.0 · jwt/v5 5.3.1 · yaml.v3 3.0.1 |
| 数据库 | modernc.org/sqlite（纯 Go，免 CGO，WAL） | 1.60.1 |
| PDF 渲染 | go-pdfium + wazero（WebAssembly，无需系统 PDFium 库） | go-pdfium 1.21.3 · wazero 1.12.0 |
| 前端框架 | React · react-router-dom · TypeScript | React 19.3 · react-router-dom 7.18 · TypeScript 7.0 |
| 构建工具 | Vite（Rolldown 内核）· @vitejs/plugin-react | Vite 8.3 · plugin-react 6.1 |
| 样式 / 组件 | Tailwind CSS v4（@tailwindcss/vite）· Radix UI 原语 + class-variance-authority（shadcn 风格）· lucide-react · sonner · tw-animate-css | tailwindcss 4.3 · Radix UI 1.x/2.x · cva 0.7 |
| 图表 | recharts（构建时单独分包） | 3.10 |
| 存储 | SQLite（WAL）+ 本地文件目录 | — |
| 工具链 | Make · pnpm（`packageManager` 锁定） | pnpm 12.6.0 |
| LLM | OpenAI 兼容多模态接口（GPT-4o / 通义千问 VL / 豆包视觉等均可配置） | — |

> 版本以 `go.mod` 与 `web/package.json` 为准，可用 `go list -m all` / `pnpm list` 核对。

## 快速开始

### 1. 构建（产出单一二进制）

前置依赖：Go 1.27+（当前构建使用 1.27.2）、Node.js 20+（当前构建使用 24.22）、
pnpm（`npm install -g pnpm`，仓库通过 `packageManager` 锁定 12.6.0）。

```bash
make build        # = 前端构建 + 嵌入 + Go 编译 → ./sorter
```

### 2. 生成访问 token 哈希

```bash
./sorter hash-token 你的访问token
# 输出: sha256:xxxx...  ← 填入 config.yaml 的 auth.token_hash
```

### 3. 配置

```bash
cp config.example.yaml config.yaml
# 编辑 config.yaml：填入 token_hash 和 LLM 的 base_url / api_key / model
```

### 4. 启动

```bash
./sorter -config config.yaml
# 监听 :8080，浏览器打开 http://localhost:8080
```

部署到服务器只需拷贝两个文件：`sorter`（二进制）+ `config.yaml`。
数据（SQLite + 上传文件）默认写入 `./data/` 目录。

也可用环境变量覆盖配置：`SORTER_ADDR`、`SORTER_TOKEN_HASH`、
`SORTER_LLM_BASE_URL`、`SORTER_LLM_API_KEY`、`SORTER_LLM_MODEL` 等。

## 开发

前端使用 **pnpm** 管理依赖（`packageManager` 已锁定 pnpm@12.6.0）。
若尚未安装：

```bash
npm install -g pnpm     # 或: corepack enable pnpm
```

```bash
# 终端 1：后端（使用占位前端页面）
make dev-backend    # go run ./cmd/sorter -config config.yaml

# 终端 2：前端热更新（Vite 8 / Rolldown 代理 /api 到 :8080）
make dev-frontend   # cd web && pnpm run dev
```

## 交叉编译（免 CGO，直接可用）

```bash
make build-linux-arm64   # Linux ARM64
make build-windows       # Windows amd64
```

## API 一览

| 方法           | 路径                                                     | 说明                                                         |
| -------------- | -------------------------------------------------------- | ------------------------------------------------------------ |
| POST           | `/api/login`                                             | token 登录，返回 JWT                                         |
| POST           | `/api/invoices`                                          | 批量上传（multipart，字段名 `files`）                        |
| GET            | `/api/invoices`                                          | 分页列表（category/seller/keyword/start_date/end_date/page） |
| GET/PUT/DELETE | `/api/invoices/:id`                                      | 详情 / 修正 / 删除                                           |
| POST           | `/api/invoices/:id/re-extract`                           | 重新提取                                                     |
| GET            | `/api/invoices/:id/file`                                 | 原始文件预览                                                 |
| GET            | `/api/stats?group_by=day\|month\|year\|seller\|category` | 聚合统计                                                     |
| GET            | `/api/stats/summary`                                     | 汇总卡片数据                                                 |

除 `/api/login` 与 `/api/health` 外均需 `Authorization: Bearer <JWT>`。
