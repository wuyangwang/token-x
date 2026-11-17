# token-x 设计文档

AI Agent Token 用量管理器 —— 查询、删除 Codex / OpenCode 的 session，查看当天 token 用量与耗费金额。

> 本文档基于对本机实际数据的探查（Codex CLI 0.169.0、OpenCode v2.3.0），所有数据格式结论均有实证。

---

## 1. 技术栈

| 项 | 选择 | 说明 |
|---|---|---|
| 语言 | Go 1.27+（纯 Go，无 cgo） | |
| UI | mygo `package ui`（GPU 自绘原生 UI，无 webview） | `E:\work\test-demo\mygo` |
| 组件库 | my-ui（shadcn 风格设计系统，zinc 默认主题，跟随系统明暗色） | `github.com/wuyangwang/my-ui` |
| 数据读取 | 实时读取原始数据（v1 不建索引库） | |
| 数据源架构 | `DataSource` 插件接口，v1 实现 codex + opencode 两个 | |
| 界面语言 | 中文（zh-Hans） | |
| 金额币种 | USD（模型计价口径，不做汇率换算） | |
| 打包 | `go tool mygo build` → NSIS 安装器（`%LOCALAPPDATA%\Programs`，免管理员权限） | |
| 自动更新 | v1 不启用，`updates` 配置字段预留；激活步骤见 §8 | |
| 代码签名 | 无证书（SmartScreen 提示"未知发布者"，可接受） | |

工程初始化：

```sh
go run github.com/egoist/mygo/cmd/mygo@latest init -template native token-x
# 已有 token-x 目录，将模板骨架合并进现有目录后：
go tool mygo dev
```

---

## 2. 架构

```
token-x/
├── main.go                  # 窗口、页面路由
├── core/
│   ├── datasource.go        # DataSource 接口（插件接入点）
│   ├── types.go             # Session / UsageRecord / DailyStat 等领域类型
│   ├── aggregate.go         # 聚合：按天、按模型、按项目
│   └── prices.go            # 价格表：默认值 + 用户配置合并
├── providers/
│   ├── codex/               # rollout JSONL 解析、.zst、删除
│   └── opencode/            # SQLite 只读查询、CLI 删除
└── ui/
    ├── dashboard.go         # 今日用量 + 趋势图 + 模型细分
    ├── sessions.go          # session 列表（筛选/删除）
    └── settings.go          # 价格表编辑、数据路径
```

### DataSource 接口（v1 的扩展点）

```go
type DataSource interface {
    Name() string
    Available() error                      // 数据目录是否存在等
    Sessions(filter Filter) ([]Session, error)
    DeleteSession(id string) error
}

type Session struct {
    ID, Title string
    Source    string      // "codex" | "opencode"
    Project   string      // 工作目录/项目
    Model     string
    Created, Updated time.Time
    Tokens    TokenUsage  // input / cached / cache_write / output / reasoning
    Cost      CostValue   // Amount + Estimated 标记
    Parent    string      // opencode 子 agent 指向父 session，codex 恒空
}
```

聚合层在内存中把各 provider 的 Session 流按天/模型/项目汇总；"当天"按本地时区切分（原始时间戳均为 UTC）。

---

## 3. 数据源：Codex

### 存储（已实证）

- 目录：`%USERPROFILE%\.codex\sessions\<YYYY>\<MM>\<DD>\rollout-<时间戳>-<uuid>.jsonl`；支持 `CODEX_HOME` 环境变量覆盖。
- 老会话归档于 `.codex\archived_sessions\*.jsonl.zst`（zstd 魔数 `28 B5 2F FD`），**计入统计**，解压按需。
- 部分日期存在同名 `.jsonl` 与 `.zst` 双份：**按文件名中的 UUID 去重**。

### 解析

逐行 JSON，取三类事件：

| 事件 | 用途 |
|---|---|
| `session_meta`（首行） | session id、cwd、originator、时间 |
| `turn_context` | 逐 turn 的模型名（如 `gpt-6.1-sol`）；token 记账归入最近一次 preceding 的 turn_context 模型 |
| `event_msg` / `payload.type=token_count` | `total_token_usage`（会话累计）与 `last_token_usage`；**逐 turn 用量 = 累计值与前一次的差值** |
| `token_usage_record`（0.15x–0.16x 时代的文件） | `usage`（单次响应粒度）、`turn_token_usage`、`thread_token_usage`；存在时优先用它 |

token 分类：`input_tokens`、`cached_input_tokens`（缓存读，计费折扣价）、`cache_write_input_tokens`、`output_tokens`、`reasoning_output_tokens`。

### 删除（永久）

1. 确认弹窗（标题列出 session 名与模型）。
2. rollout 文件（`.jsonl` 与同名 `.zst`，及 `archived_sessions` 中的副本）移除。
3. 清理 `session_index.jsonl` 中该 id 的行：读全文件 → 过滤 → 原子写回（写临时文件后 rename）。
4. 不触碰 `.codex\sqlite\`、`codex.db`、`thread_history_1.sqlite`（Codex Desktop 的历史记录，留痕无碍，动它风险大）。

---

## 4. 数据源：OpenCode

### 存储（已实证，v2.x 为 SQLite 单库）

- 库文件：`%USERPROFILE%\.local\share\opencode\opencode.db`（约 90 MB，WAL 模式）。**只读打开**（`file:...?mode=ro`），所有写操作只走官方 CLI。注意 WAL 未合并的写入在 `-wal` 文件里，只读打开会正确读到。
- 本机无 `storage\session\` 之类的旧版 JSON 布局；`session_part` 表为空（part 内联在消息 JSON 中）。

### 查询

- Session 列表：`SELECT * FROM session_v2 ORDER BY time_updated DESC`
  - 聚合列直接可用：`cost`、`tokens_input/output/reasoning/cache_read/cache_write`
  - `directory` / `project_id`（join `project` 表取 worktree 根路径）→ 项目筛选
  - `model` 是 JSON 字符串（`{id, providerID, variant}`）→ 按模型细分
  - `parent_id` 非空 = 子 agent session，**计入总量**，列表标注
  - `time_archived IS NOT NULL` 的会话同样计入（真实消耗）
- 明细（趋势图/按模型细分的逐日数据）：`session_message`（`type='assistant'`、`data` JSON 内 `tokens.{input,output,reasoning,cache.read,cache.write}`、`cost`、`model`、`time.completed`），用 `json_extract`，仅统计 `time.completed` 非空的（流式未完成的无用量）。
- 本机所有 cost 均为 0（免费模型），**不得假设 cost>0**。

### 删除（永久）

已验证 CLI 能力：`opencode session list / delete / export / import`。

1. 确认弹窗。
2. 执行 `opencode session delete <sessionID>`（官方命令，含级联删除子 session，正确处理 WAL 并发）。
3. 不手写 SQL 删除；辅助 JSON（`storage\agent-usage-reminder\ses_*.json`）残留无害，不清理。

---

## 5. 金额计算

| 数据源 | cost 来源 |
|---|---|
| opencode | 记录自带的 `cost` 字段优先（`session_v2.cost` / 消息级 `data.cost`） |
| codex | 无 cost 字段，一律用价格表计算 → 标注 **"估算"** |

价格表（`prices.json`，存应用配置目录）：

```json
{
  "models": {
    "gpt-6.1-sol":     { "input": 1.25,  "cachedInput": 0.125, "cacheWrite": 0,   "output": 10.0 },
    "gpt-5.5":         { "input": 1.25,  "cachedInput": 0.125, "cacheWrite": 0,   "output": 10.0 }
  },
  "fallback": { "input": 1.0, "cachedInput": 0.1, "cacheWrite": 0, "output": 5.0 }
}
```

- 单价单位：USD / 百万 token。默认值为 OpenAI 官方价的占位，**用户可在设置页逐模型修改**（例如填中转站 `ccswitch` 的实际单价，金额即变为真实开销）。
- 金额 = `input × 单价 + cachedInput × 折扣价 + cacheWrite × 单价 + output × 单价`（reasoning 已含在 output 内）。
- 未知模型用 `fallback` 价，界面标注"估算"。
- codex 走中转站，官方价≠真实账单：设置页显著提示这一事实。

---

## 6. UI 设计（my-ui 组件）

顶栏：应用名 + 数据源徽标（codex / opencode 连接状态）+ 刷新按钮 + 明暗切换。
左侧：分组导航（总览 / Sessions / 设置）。

### 6.1 总览（Dashboard）

- **今日卡片行**（4 张）：今日 Token 总量、今日金额（区分实收/估算）、今日 session 数、最贵单 session。
- **趋势图**：近 7/30 天 token 与金额折线（mygo `ui.Chart`），可切天数。
- **模型细分表**：按模型汇总 token（input/cached/output 分列）与金额，今日 + 累计两个 tab。
- 全局数据可按项目筛选（下拉，默认"全部"）。

### 6.2 Sessions

- 数据表（mygo `ui.Table`）：标题、来源、项目、模型、创建时间、tokens、金额、父子标记。
- 可排序列；筛选：来源 / 项目 / 模型 / 关键词。
- 选中 → 工具栏"删除"（支持多选）→ 确认弹窗（列出将删除的 session 及其 token/金额）→ 永久删除 → 行内 toast 反馈 → 自动刷新统计。

### 6.3 设置

- 价格表编辑表格（模型 / input 价 / cached 价 / output 价 / 恢复默认）。
- 数据目录路径显示（检测 `CODEX_HOME`、opencode 数据目录可用性）。
- 关于：版本、检查更新（v1 显示"未启用自动更新"）。

---

## 7. 风险与对策

| 风险 | 对策 |
|---|---|
| opencode.db schema 随版本变化（当前 v2.3.0） | SQL 查询集中在 `providers/opencode/` 单文件；启动时检测 `session_v2` 关键列，缺失则提示版本不兼容 |
| codex 版本演进改变 rollout 事件结构 | 解析按事件 type 分派，未知 type 忽略；token 记账同时支持 `token_count` 差值与 `token_usage_record` 两种路径 |
| opencode 正在运行时读库 | 只读模式 + WAL 下读取是安全的；写操作全走 CLI |
| codex `.zst` 逐次解压开销 | 扫描在后台 goroutine + 界面 loading 态；按 UUID 去重避免双份 |
| 中转站价格不可知 | 价格表可编辑 + "估算"标注，文档明示 |

---

## 8. 打包与（预留的）自动更新

```sh
go tool mygo build       # 产出 build/windows-amd64/：token-x.exe + token-x-setup-<ver>-amd64.exe
```

- `mygo.config.ts`：`name: "Token X"`、`identifier`、`version`、`i18n: { source: "zh-Hans" }`、图标 `resources/icon.png`（1024×1024）。
- **预留** `updates` 字段（`publicKey` 留空）；激活步骤：
  1. `mygo keygen` → 生成 `mygo-update.key`（私钥，进密码管理器/CI secrets，不入库）
  2. 配置 `updates: { publicKey, github: "you/token-x" }`
  3. `MYGO_UPDATER_PRIVATE_KEY=... go tool mygo build -upload` → 上传到 GitHub Releases（草稿）→ 发布
  4. 应用内启用 updater 插件（`mygo.Use(updater.Plugin)`）

---

## 9. 里程碑

| 阶段 | 内容 | 验收 |
|---|---|---|
| M1 | 脚手架 + DataSource 接口 + 两个 provider 的只读解析（含 `.zst`） | 命令行可列出全部 session 与用量，数字与手算抽查一致 |
| M2 | UI：Dashboard（卡片/趋势图/模型细分）+ Sessions 表格（筛选/排序/父子标记） | 界面数据与 M1 输出一致 |
| M3 | 删除流程（确认弹窗、CLI 调用、codex 文件+索引清理） | 删除后两工具本体（TUI/CLI）中 session 确实消失，统计自动刷新 |
| M4 | 设置页价格表 + 金额口径（自带 cost 优先/估算标注） | 改价后金额即时变化 |
| M5 | 打包安装器 + 图标 + 中文安装界面 | NSIS 安装包可安装、可卸载 |

v2 候选：数据导出（CSV/JSON）、月度汇总、回收站、自动更新激活、更多数据源（Claude Code 等）。
