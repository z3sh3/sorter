# AGENTS.md · Sorter 技术架构与开发约定

本文档是本仓库的**技术选型基线**。任何代码更新（含重构、依赖升级、新功能）
都必须遵循此处已锁定的选型与架构约束；如需偏离，必须先说明理由并获得确认，
不得静默替换技术栈。

## 1. 项目定位

单二进制部署的发票管理系统：上传图片 / 单页 PDF，由多模态 LLM 一次调用完成
OCR 字段提取与自动分类，人工复核后进入统计；前端静态资源嵌入 Go 二进制。

核心不变式：

- 单实例、单二进制、无 CGO，可交叉编译。
- 生产环境只部署两个文件：`sorter` 与 `config.yaml`。
- 金额以最小货币单位的整数存储，统计严格按币种隔离，永不跨币种换算或求和。
- 抽取结果先复核再计入统计；写操作带版本号做乐观并发控制。

## 2. 技术选型（已锁定）

版本以 `go.mod` 与 `web/package.json` 为准；升级需单独提交并说明影响。

| 层次        | 选型                                                                                                 | 当前版本                         | 强制约束                                                                                    |
| ----------- | ---------------------------------------------------------------------------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------- |
| 后端语言    | Go                                                                                                   | `go 1.27.2`（本地 1.27.x）       | 只用标准库 + 下表依赖，不引入框架级替代品                                                   |
| HTTP 框架   | Gin                                                                                                  | 1.12.0                           | 路由、中间件、参数绑定统一走 Gin                                                            |
| 认证        | golang-jwt/jwt v5                                                                                    | 5.3.1                            | 预置 token 的 SHA-256 哈希校验 + JWT 会话，不引入 session 存储                              |
| 配置        | gopkg.in/yaml.v3                                                                                     | 3.0.1                            | 严格模式解析（未知字段报错）+ 环境变量覆盖                                                  |
| 数据库      | modernc.org/sqlite                                                                                   | 1.60.1                           | 纯 Go、无 CGO、WAL、`SetMaxOpenConns(1)`，手写 SQL                                          |
| PDF 渲染    | go-pdfium + wazero（WASM）                                                                           | go-pdfium 1.21.3 / wazero 1.12.0 | 只用 WebAssembly PDFium，不依赖系统库                                                       |
| 图像解码    | 标准库 image + golang.org/x/image                                                                    | x/image 0.47.0                   | WebP 等格式通过 x/image 注册解码器                                                          |
| 前端框架    | React + TypeScript                                                                                   | React 19.3.1 / TypeScript 7.0.2  | 函数组件 + Hooks，不引入类组件或额外状态库                                                  |
| 路由        | react-router-dom                                                                                     | 7.18.4                           | 数据路由未启用，沿用 `<BrowserRouter>` + `<Routes>`                                         |
| 构建        | Vite（Rolldown）+ @vitejs/plugin-react                                                               | Vite 8.3.4 / plugin-react 6.1.2  | 仅用 Vite，不引入 Webpack / Rspack / Turbopack                                              |
| 包管理      | pnpm                                                                                                 | 12.6.0（`packageManager` 固定）  | 只用 pnpm，禁用 npm / yarn / bun 生成锁文件                                                 |
| 样式        | Tailwind CSS v4 + @tailwindcss/vite                                                                  | 4.3.3                            | 只用 Tailwind 原子类与 `index.css` 中的设计令牌                                             |
| UI 组件     | shadcn/ui（源码内置于 `web/src/components/ui/`，底层为 Radix primitives + class-variance-authority） | Radix 1.x/2.x、cva 0.7.1         | 业务代码只使用 shadcn 封装组件；不直接使用 `@radix-ui/*` 原始 primitive，不引入第三方组件库 |
| 图标 / 提示 | lucide-react / sonner                                                                                | 1.60.0 / 2.0.8                   | 图标只用 lucide；轻提示只用 sonner 的 `toast`                                               |
| 动画辅助    | tw-animate-css                                                                                       | 1.4.0                            | 通过 `@import` 引入，不额外配置动画库                                                       |
| 图表        | recharts                                                                                             | 3.10.1                           | 仅在 Dashboard 使用，构建时拆为独立 chunk                                                   |
| 类名工具    | clsx + tailwind-merge                                                                                | 2.1.1 / 3.7.0                    | 统一通过 `cn()` 合并类名                                                                    |
| 存储        | SQLite（WAL）+ 本地文件目录                                                                          | —                                | 不引入对象存储 / Redis / 消息队列                                                           |
| 部署        | 前端 `embed` 进 Go 二进制                                                                            | —                                | 不引入 Nginx 之外的独立前端服务、不做 SSR                                                   |
| LLM         | OpenAI 兼容的多模态 HTTP 接口                                                                        | —                                | 只用 `net/http` 直连，不引入官方/第三方 SDK                                                 |
| 流水线      | Make + GitHub Actions                                                                                | —                                | 构建与检查入口固定为 `Makefile`                                                             |

## 3. 明确禁止的替代方案

以下方案与现有架构冲突，除非获得显式批准，否则不得引入：

- 后端：Echo / Fiber / chi / Beego 等 Web 框架；GORM / ent / sqlx 等 ORM 或
  SQL 构建器；mattn/go-sqlite3 或其他 CGO 数据库驱动；系统 PDFium 或 CGO 版
  poppler；Redis / PostgreSQL / MongoDB 等外部存储；消息队列。
- 配置：viper / envconfig 等配置框架。
- 前端：Next.js / Remix / Astro 等 SSR 框架；Redux / Zustand / MobX / Jotai 等
  状态库；TanStack Query / SWR 等请求缓存层；axios 或其他 HTTP 客户端；
  MUI / Ant Design / Chakra 等组件库；在页面或业务组件中直接 import
  `@radix-ui/react-*` 原始 primitive（必须经 `components/ui/` 的 shadcn 封装）；
  CSS-in-JS / styled-components / CSS Modules / Sass；Webpack / Rspack /
  Turbopack；npm / yarn / bun。
- 通用：任何会让产物需要 CGO、需要额外运行进程、或破坏"单二进制 + 内嵌前端"
  特性的依赖。新增运行时依赖前必须先征得同意，并同步更新 README 的
  Tech stack 表。

## 4. 目录结构与职责

```text
cmd/sorter/          # 进程入口：配置加载、hash-token 子命令、启动/优雅退出
internal/config/     # YAML 加载、默认值、环境变量覆盖、严格校验
internal/invoice/    # 领域模型：Invoice、Money、状态/分类常量、校验规则
internal/store/      # SQLite 持久化：SQL、事务、迁移（embed migrations/*.sql）
internal/jobs/       # 持久化队列 + worker 池：认领、重试、失败落库
internal/extractor/  # LLM 调用、提示词、PDF→PNG（pdfium WASM）
internal/app/        # 应用服务：导入/更新/复核/删除、文件存储与清理
internal/auth/       # token 校验与 JWT 签发/验证
internal/server/     # Gin 路由、中间件、DTO、HTTP 错误映射、SPA 静态服务
internal/server/dist # Vite 构建产物的 embed 目录（.gitkeep 保留，产物不入库）
web/src/api/         # 后端契约类型 + 唯一 HTTP 客户端
web/src/hooks/       # 数据获取与轮询（AbortController 取消）
web/src/pages/       # Dashboard / Import / Invoices / Login
web/src/components/  # 布局与业务组件；ui/ 为 shadcn 风格基础组件
web/src/lib/         # cn()、金额/分类/币种格式化等纯函数
```

依赖方向：`server → app/jobs/extractor/store/auth/config` 与
`app/jobs/extractor → invoice`；领域包不反向依赖 HTTP、SQL 或 UI。

## 5. 后端约定

- **分层**：handler 只做解析、鉴权、DTO 映射与错误转换；业务规则在 `app` /
  `jobs` / `invoice`；SQL 只出现在 `store`。跨层依赖用接口（如
  `app.Repository`、`jobs.Extractor`），便于用 fake 做单测。
- **金额**：`invoice.Money` 是 int64 最小单位（分），解析用 `big.Rat` 精确计算，
  最多两位小数，支持负数（红票）。禁止用 float 参与计算或存储。
- **并发控制**：所有写操作携带 `version`；`store` 用带版本条件的 UPDATE，
  冲突返回 `invoice.ErrConflict`。抽取任务通过 `extraction_token` 独占认领。
- **错误**：领域错误用哨兵值（`ErrNotFound`、`ErrConflict`、`ErrDuplicate`、
  `ErrInvalidFile`、`ErrValidation`）+ `%w` 包装；handler 用 `errors.Is` 映射
  HTTP 状态，未知错误记录日志后返回通用 500 文案。
- **迁移**：只新增 `internal/store/migrations/NNN_描述.sql`（编号递增），
  已应用的迁移不得修改；迁移文件随 `go:embed` 打包，启动时按文件名顺序执行。
- **队列**：SQLite 即队列，`jobs.Queue` 用可合并的 `wake` channel 通知；
  服务启动时 `RecoverPending` 回收中断任务，关闭时释放未完成任务。
- **文件**：上传按请求分配独立路径（不复用共享路径），保存时计算 SHA-256 去重；
  数据库写入失败必须删除已保存文件。
- **日志**：标准库 `log`，英文、小写开头、包含关键上下文（method/route/err）。
- **测试**：表驱动优先，外部依赖用同包 fake；`go test -race` 必须通过。

## 6. API 契约

- 统一前缀 `/api`；除 `/api/health`、`/api/login` 外全部要求
  `Authorization: Bearer <JWT>`。
- JSON 字段使用 `snake_case`，与 `web/src/api/types.ts` 一一对应；改后端 DTO
  必须同步改前端类型。
- 错误体固定为 `{"code": "...", "error": "客户端可读信息"}`；
  状态码：400 参数错误、401 未认证、404 不存在、409 版本冲突、
  429 登录限流、500 内部错误。
- 现有路由（新增路由需遵循同一命名风格）：

| 方法   | 路径                             | 说明                            |
| ------ | -------------------------------- | ------------------------------- |
| GET    | `/api/health`                    | 数据库连通性探针                |
| POST   | `/api/login`                     | 预置 token 换取 JWT             |
| POST   | `/api/invoices`                  | 多文件上传（multipart `files`） |
| GET    | `/api/invoices`                  | 分页/关键词/日期/分类/状态筛选  |
| GET    | `/api/invoices/:id`              | 详情                            |
| PUT    | `/api/invoices/:id`              | 人工修改（带 `version`）        |
| DELETE | `/api/invoices/:id?version=`     | 删除（带 `version`）            |
| POST   | `/api/invoices/:id/re-extract`   | 重新抽取（带 `version`）        |
| GET    | `/api/invoices/:id/file`         | 原始文件预览（带鉴权）          |
| GET    | `/api/stats?group_by=&currency=` | 单币种聚合                      |
| GET    | `/api/stats/summary`             | 各币种汇总                      |

- 限额（领域常量集中定义于 `internal/invoice`）：单文件 32 MiB、单请求
  128 MiB / 20 个文件；仅接受 JPG / JPEG / PNG / WebP / 单页 PDF，并校验内容与
  尺寸。

## 7. 前端约定

- 所有请求经过 `web/src/api/client.ts`：统一拼接 `/api`、注入 Bearer token、
  401 清理 token 并跳转 `/login`、抛出 `ApiError`。页面/hook 不得直接 `fetch`。
- 服务端状态用 `useEffect` + `useState` + `AbortController` 管理；轮询只在存在
  `processing` 记录时开启。不引入请求缓存库。
- 路径别名 `@/` 指向 `web/src`；组件用命名导出，页面用默认导出。
- UI 统一使用 **shadcn/ui 组件**：只从 `web/src/components/ui/` 引入（如
  `Button`、`Card`、`Dialog`、`Input`、`Select`、`Table`、`Badge` 等），不在页面或
  业务组件里直接使用 `@radix-ui/*` 原始 primitive；只有 `components/ui/` 内部的
  shadcn 封装可以依赖 Radix、`cva` 与 `Slot`。
- 新增 UI 组件按 shadcn 约定落地为自有源码（`data-slot` 属性、`cn()` 合并类名、
  cva 定义变体、Radix primitive 负责可访问性），而不是新增第三方 UI 依赖。
- 设计令牌只写在 `web/src/index.css`（Tailwind v4 `@theme` / CSS 变量），
  不在组件里硬编码颜色。
- 文案面向用户用中文（`format.ts` 中的枚举标签）；代码注释与日志用英文。
- 图表统一用 recharts 的 `ResponsiveContainer`；金额展示一律用
  `formatMoney(amount, currency)`，不手写货币符号或小数位。
- 深色模式通过 `document.documentElement.classList` 的 `dark` 类切换，
  主题选择持久化在 `localStorage`。

## 8. 构建、检查与发布

```bash
make build        # 前端 pnpm build → 复制到 internal/server/dist → go build ./cmd/sorter
make test         # go test ./...
make check        # go test ./... + go vet ./... + pnpm exec tsc --noEmit
make dev-backend  # go run ./cmd/sorter -config config.yaml
make dev-frontend # cd web && pnpm run dev（/api 代理到 :8080）
make build-linux-arm64 / make build-windows   # 交叉编译，仍需无 CGO
```

提交前必须通过 `make check`；CI 还会执行 `go test -race ./...`、
`go vet ./...`、干净 `go build`、前端类型检查与生产构建。

**不要自行启动服务器、数据库或浏览器做验证**（本仓库约定）；通过单元测试、
类型检查与构建结果证明正确性，需要人工验证时先征得确认。

## 9. 变更规则（强制）

1. 严格遵守第 2 节选型与第 3 节禁令；不得替换框架、构建链、数据库驱动或
   前端样式方案。
2. 新增任何第三方依赖前先说明用途、体积/维护成本与替代方案，获得确认后再加，
   并同步更新 `README.md` 的 Tech stack 表。
3. 保持"单二进制、无 CGO、前端内嵌"特性；不得让构建或运行依赖额外服务。
4. 数据库结构变更只通过新增迁移文件；不得改动既有迁移或绕过 `store` 直连 SQL。
5. API 变更必须同步 `internal/server` DTO、`web/src/api/types.ts` 与
   `web/src/api/client.ts`，并保持错误体格式一致。
6. 涉及金额、币种、复核状态、版本冲突、任务认领的改动必须补回归测试
   （参考 README 的 Covered regression areas）。
7. 代码风格与现有文件保持一致：Go 采用小接口 + 构造函数注入，前端采用函数
   组件 + Hooks；UI 只用 shadcn 封装组件，不直接使用 Radix 原始 primitive；
   注释与日志英文，用户可见文案中文。
