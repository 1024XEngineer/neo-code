**状态：** Draft
**组件：** Codebase / Workspace 领域层，Tools 代码库探索入口，Runtime Codebase Context 决策链路，Context Prompt 组装层，Prompt Assets，App Bootstrap
**日期：** 2026-05-01
**相关技术：** Workspace Retrieval, Git Facts, Structured Retrieval, Tool Registry, Runtime Run Loop, Git Status Verification, Filesystem Tools

## 1. 摘要
本 RFC 提议将 NeoCode 当前"由 runtime 基于用户最新一句文本做启发式 codebase retrieval 注入"的方案，重构为"模型主动调用 `git_* / codebase_*` 工具"的方案。

这次改造**不是新建一套 codebase / workspace 检索体系**。当前项目已经具备：

1. `Summary / ChangedFiles / Retrieve(path/text/symbol/glob)` 等代码库能力
2. changed snippets、安全过滤、裁剪、workspace 边界防护
3. runtime 自动触发 codebase context 注入
4. context 渲染 `Repository Context` section
5. 通用 `filesystem_*` 文件工具
6. `codebase_search_symbol` 所需的 Go-first 轻量符号匹配与 whole-word fallback 底层能力
7. checkpoint 模块中的 `Fingerprint`（工作区全量指纹扫描 + diff 对比）和 `FileChangeKind / FileChangeEntry`（通用变更分类类型），可作为非 Git workspace 下的降级基础

本次真正要做的是：

1. 把代码库探索能力从 `context` 路径归属中迁出，改成中性领域层
2. 新增正式的 `git_* / codebase_*` 工具入口，让模型主动调用
3. 删除当前 runtime 的启发式自动 retrieval 主链路
4. 保留迁移期的轻量 Git 事实注入，并通过工具按需暴露更具体的 Git / workspace 信息
5. 调整 prompt assets 中的工具使用策略，明确 `git_* / codebase_*` 优先于 `filesystem_*` 的代码库探索路径

第一版工具集确定为：

1. `git_summary`
2. `git_changed_files`
3. `git_changed_snippets`
4. `codebase_read`
5. `codebase_search_text`
6. `codebase_search_symbol`

其中需要明确：`codebase_read` 视为**新增底层能力**，用于提供 workspace 语义下的单文件受限读取；它不是现有 `Retrieve(path)` 的简单包装。

工具命名遵循以下约定：

1. `git_*` 前缀标识强依赖 Git 的能力（Git 事实查询），在非 Git 场景下返回稳定降级结果。
2. `codebase_*` 前缀标识 Git 无关的代码库探索能力，在非 Git 目录下继续可用。

## 2. 背景与问题
当前系统已经具备：

1. 轻量 Git summary 能力
   已能返回 `in_git_repo / branch / dirty / ahead / behind / changed_file_count / representative_changed_files` 等事实。

2. changed files / changed snippets 能力
   已能基于一次 `git status` 输出派生 changed files，并在受限条件下返回 snippets。

3. path / glob / text / symbol retrieval 能力
   已能返回稳定的结构化 retrieval hit，并带 `line_hint / snippet / truncated`。

4. `filesystem_*` 通用文件工具
   已有 `filesystem_read_file / filesystem_grep / filesystem_glob`。

5. runtime 自动触发 codebase context 的能力
   已会基于最新用户文本做启发式判定，并把结果投影给 `context` 渲染。

但当前仍有几个关键问题：

1. retrieval 触发逻辑过于脆弱
   当前主要依赖最新一条用户文本上的关键词、正则和引号锚点，稳定性和召回率都偏弱。

6. `context/repository` 模块路径归属不自然
   这部分能力当前挂在 `internal/context/repository` 下，但它本质上是代码库探索与 Git 事实能力，不是 prompt 渲染逻辑。
   `context` 更适合消费已经准备好的 codebase 投影结果并渲染 prompt section，而不是长期承载 Git 扫描、changed files、定向检索、安全过滤与裁剪等领域实现。

7. checkpoint 模块中存在语义上不属于它的通用能力
   `internal/checkpoint` 中有 `Fingerprint`（`ScanWorkdir` + `DiffFingerprints`，工作区文件状态扫描与比较）和 `FileChangeKind / FileChangeEntry`（`added / deleted / modified` 通用变更分类）。
   这两个能力的语义是"工作区状态感知与变更分类"，跟 checkpoint 的核心职责（工具写之前的版本快照与恢复）没有必然关系，当前放在 checkpoint 里纯属历史原因。
   如果 `git_*` 工具需要在非 Git workspace 下提供降级的变更列表和 diff 能力，就需要这些能力，但不应该为此依赖整个 checkpoint 包（包括写链路、SQLite 持久化、session 类型）。

8. runtime 在替模型猜是否需要仓库信息
   这导致误触发、漏触发，以及"一轮最多打一类 retrieval"的能力上限。

9. `filesystem_*` 与 codebase retrieval 语义并不等价
   通用文件工具能读和搜，但不提供 changed-files、Git 摘要、稳定 retrieval 结构和 workspace 级安全裁剪语义。
   同时，`filesystem_read_file` 是完整文件读取，而现有 `Retrieve(path)` 是受限 snippet 检索，两者也不等价。

10. 当前 prompt assets 仍在强化 `filesystem_*` 优先
    当前主提示词中的工具使用策略明确优先 `filesystem_read_file / filesystem_grep / filesystem_glob` 进行探索，这会削弱 `git_* / codebase_*` 工具引入后的主路径地位。

11. 当前的 `git status` 验证并不能替代 codebase / Git 工具
    现有 `verify/git_diff` 是**收尾验收**逻辑，用来确认任务是否真的产生改动；它不是给模型在推理过程中按需获取代码库上下文的入口。

## 3. 典型用户场景

### 场景 1：用户要求 review 当前改动
用户说"review 我这次改动"。

旧行为：runtime 猜测这是 review 语义，自动注入 changed files，有时再自动带 snippets。

新行为：模型先调用 `git_summary` 了解工作树状态，再调用 `git_changed_files` 或 `git_changed_snippets` 获取具体改动。

### 场景 2：用户要求定位某个实现
用户说"看看 `ExecuteSystemTool` 在哪"。

旧行为：runtime 尝试从文本里抽符号锚点，自动做一次 symbol retrieval。

新行为：模型直接调用 `codebase_search_symbol`，再决定是否继续 `codebase_read`。

### 场景 3：用户要求解释某个文件
用户说"解释 `internal/runtime/run.go`"。

旧行为：runtime 从用户文本里抓路径并预注入 path retrieval。

新行为：模型直接调用 `codebase_read`，只在真的需要时读取文件。

### 场景 4：用户要求判断当前分支状态
用户说"我这个分支现在能不能直接提交/推送"。

旧行为：runtime 不一定会自动提供 branch / ahead / behind / dirty 信息。

新行为：模型通过 `git_summary` 主动获取 Git 事实，再决定是否继续看 changed files。

## 4. 设计目标
本方案要求同时满足：

1. codebase / workspace 检索是否发生，由模型决定，而不是由 runtime 猜测。
2. 模型可调用的代码库能力统一进入 `internal/tools`。
3. codebase / workspace 核心实现迁出 `context` 路径归属，形成中性领域层。
4. 保留现有成熟实现，不重复发明底层代码库检索逻辑，`codebase_read` 作为唯一承认新增的底层能力单独补齐。
5. `filesystem_*` 继续保留，但在模型层明确 `git_* / codebase_*` 优先。
6. 第一版不引入 embedding、LSP；Tree-sitter 作为跨语言符号索引层纳入第一版，与现有 Go-first 实现形成分层检索，而非替换底层逻辑。
7. 结果输出保持结构化优先，降低模型消费不稳定性。
8. 迁移期保留最小 Git Summary 注入，避免一次性移除所有 Git 基础事实导致主链路行为突变。
9. 在非 Git workspace 下，代码探索能力继续可用；Git 相关能力返回稳定降级结果，而不是把整组工具视为不可用。

## 5. 非目标
本 RFC 不处理：

1. 不重写现有代码库检索核心算法与安全策略。
2. 不引入 codebase 向量索引或 embedding retrieval。
3. 不引入 LSP 或跨文件语义分析；Tree-sitter 作为轻量语法解析层纳入。
4. 不保留当前启发式自动 retrieval 作为主入口。
5. 不移除现有 `filesystem_*` 通用工具。
6. `codebase_search_symbol` 本轮扩展到多语言符号检索，但只通过 Tree-sitter 做语法级定义提取，不做跨文件类型解析或语义级引用追踪。
7. 不把 final verify 阶段的 `git_diff` verifier 改造成推理期 retrieval 入口。
8. 不在本轮把迁移后的 codebase 模块继续设计成新的隐式 prompt 注入系统。

## 6. 核心设计

### 6.1 明确"已有能力"与"本次新增交付物"
本次不新增的能力：

1. Git summary 底层能力
2. changed files / changed snippets 底层能力
3. retrieval(path/glob/text/symbol) 底层能力
4. workspace 安全过滤、大小限制、二进制过滤、symlink/path escape 防护
5. `codebase_search_symbol` 的 Go-first 轻量匹配与 whole-word fallback 底层检索能力

这些能力已经存在，本次继续沿用。

本次新增或改造的交付物：

1. 中性 codebase / workspace 领域模块
2. `git_* / codebase_*` 工具入口
3. `codebase_read` 底层能力
4. **Tree-sitter 跨语言符号索引器**（`internal/repository/treesitter`）：grammar 加载、AST 解析、符号倒排索引、增量更新
5. `codebase_search_symbol` 分层检索架构：Go AST 快速路径 + Tree-sitter 通用路径 + whole-word fallback
6. runtime 的 git/codebase-first 工具暴露
7. prompt assets 中的 git/codebase-first 工具使用策略
8. 删除自动 codebase retrieval 主链路
9. 删除 `context` 对 codebase retrieval 注入的长期依赖

这样设计的原因是：当前问题不在底层 codebase 能力整体缺失，而在入口、边界和编排方式不对；只有 `codebase_read` 是本轮需要明确承认的新增底层能力。

### 6.2 从 `internal/context/repository` 迁出为中性领域层
将当前 `internal/context/repository` 迁移为新的中性模块，建议命名为 `internal/repository`。

同时，从 `internal/checkpoint` 中提取两个语义上不属于 checkpoint 的通用能力，一并迁入 `internal/repository`：

1. `Fingerprint`（`ScanWorkdir` + `DiffFingerprints` + 相关类型）
   这是工作区文件状态扫描与比较的纯函数能力，当前放在 checkpoint 里只是因为最早只有 checkpoint 在用它做 bash write gate 的兜底检测。其语义属于"工作区状态感知"，应在新的中性领域层中作为基础能力提供。
   迁移后，`internal/checkpoint` 和 `internal/runtime` 中对 `checkpoint.ScanWorkdir` / `checkpoint.DiffFingerprints` 的引用改为依赖 `internal/repository`。

2. `FileChangeKind` / `FileChangeEntry`（`added / deleted / modified` 变更分类类型）
   这两个类型是 checkpoint 和 repository 两个领域共享的基础语义。提取到 `internal/repository` 后避免两个包各定义一份，`git_changed_files` 的变更分类可以直接复用。
   注意：`PerEditSnapshotStore` 上挂载的 `Diff()` / `ChangedFiles()` 方法不迁移——这两个方法紧密依赖 per-edit 的 `.bin/.meta` 版本链存储格式，时间基准是"两个 per-edit checkpoint 之间"，不是"工作树当前状态"，语义不适合作为通用能力暴露。

它负责：

1. `Summary`
2. `ChangedFiles`
3. `Inspect`
4. `Retrieve`
5. changed snippet 生成
6. 安全过滤与结果裁剪
7. `codebase_read` 所需的受限单文件读取能力
8. **符号检索路由**：Go AST 快速路径 + Tree-sitter 通用路径的统一调度与结果聚合
9. 非 Git workspace 下的稳定降级

它不负责：

1. prompt section 渲染
2. runtime 自动触发策略
3. tool schema 暴露
4. 模型选择何时检索

这样设计的原因是：我们要复用实现，但不能继续保留原有不自然的 `context` 路径归属。

更具体地说，判断 codebase 能力应该拆出 `context` 的依据是：

1. `context` 的天然职责是 prompt 组装与渲染，而不是执行 Git 扫描、文件检索或 workspace 级安全过滤。
2. 当前 repository 实现已经包含 `Summary / Inspect / ChangedFiles / Retrieve` 等完整领域能力，它们本质上是"代码库 / Git 事实服务"，不是"prompt section 生成器"。
3. 当前 `context` 真正合理保留的部分，只有对最小 `RepositorySummary` 和 `Repository Context` 投影结果的消费与渲染；这说明能力主体与目录归属已经不匹配。
4. 当模型将来通过 `git_* / codebase_*` 工具主动访问代码库信息时，如果底层实现仍挂在 `context` 路径下，会继续强化"代码库检索是 prompt 隐式注入附属物"的错误心智，而不是一个独立的领域能力与正式工具入口。

因此，这次迁移不是单纯为了目录整洁，而是为了把"代码库 / Git 事实获取"和"上下文渲染"重新拉回正确边界。

迁移策略采用分阶段方式：

1. 先建立新的中性模块并迁移实现
2. tools/runtime/context 逐步改依赖
3. 最后清理旧路径和兼容壳

这样设计的原因是：避免把"入口改造"膨胀成一次性的大规模搬迁。

### 6.3 新工具集采用"多工具专职"
新增六个专职工具，按 Git 依赖分为两组：

**Git 事实工具（`git_*`）：**

1. `git_summary`
   返回 `in_git_repo`、`branch`、`dirty`、`ahead`、`behind`、`changed_file_count`、`representative_changed_files`
   在非 Git workspace 下返回稳定降级结果：`in_git_repo: false`，其余 Git 字段为空或零值，但工具调用本身不视为失败。

2. `git_changed_files`
   默认只返回变更列表，不带 snippet。返回 `status`、`path`、`old_path`、`returned_count`、`total_count`、`truncated`

3. `git_changed_snippets`
   独立返回 changed snippets，复用现有 diff/head snippet 策略与敏感内容过滤

**代码库探索工具（`codebase_*`）：**

4. `codebase_read`
   返回 workspace 语义下的单文件受限内容读取结果，作为第一版唯一明确新增的底层能力；其边界、过滤与工作区防护应与现有 repository snippet 安全策略保持一致

5. `codebase_search_text`
   返回 `path`、`line_hint`、`match_count`、`truncated`。不返回匹配行的文本内容，确保模型必须调用 `codebase_read` 才能看到原始内容。

6. `codebase_search_symbol`
   采用**分层符号检索**架构，按语言类型路由（详见第 10 节）。同时执行**硬约束**：索引工具只返回位置与声明签名，不返回代码实现内容。

   - **Go 语言**：保留现有 Go-first AST 快速路径，利用 `go/ast` + `go/types` 提取符号并推断接口实现关系；未命中时退回 whole-word 文本搜索。
   - **其他语言**：通过 Tree-sitter 提取语法级符号定义（函数、类、方法、变量等），查询内存索引；未命中时退回 whole-word 文本搜索。
   - **未知/不支持语言**：直接走 whole-word 文本搜索，不阻塞、不报错。

   `codebase_search_symbol` 返回字段严格限定为：`path`、`line_hint`、`kind`、`signature`（声明头）。`signature` 只包含函数名/类名与参数/返回类型，不包含函数体或类实现。

   具体实现机制（索引初始化、增量更新、失败处理）见 6.9。

这样设计的原因是：代码库查询天然分类型，拆成专职工具比单工具多模式更稳定；同时按 Git 依赖分组命名，让工具名直接表达其能力边界。硬约束确保模型无法基于索引结果中的代码片段绕过 `codebase_read` 直接推理，迫使它基于真实文件内容进行验证。

### 6.4 保留 `filesystem_*`，但默认优先 `git_* / codebase_*`
保留现有：

1. `filesystem_read_file`
2. `filesystem_grep`
3. `filesystem_glob`
4. 其他通用文件工具

同时在模型提示词中明确：

1. Git 摘要、当前改动优先使用 `git_*`；代码定位、符号/文本检索优先使用 `codebase_*`
2. `filesystem_*` 仅用于通用文件操作或 `git_* / codebase_*` 无法覆盖的特殊场景
3. `filesystem_read_file` 仍保留完整文件读取语义，但代码库探索与 Git/changed-files 相关任务应优先走 `git_* / codebase_*`

这样设计的原因是：`git_* / codebase_*` 提供代码库语义，`filesystem_*` 提供通用文件能力，两者都保留，但默认路径必须清晰。

需要明确的是：这项策略不应只停留在 runtime 概念层，而必须同步落到 prompt assets 中当前的工具使用模板。

### 6.5 结果形态采用结构化优先
所有 `git_* / codebase_*` 工具返回稳定的结构化文本，字段名固定，不走纯 grep/diff 原始风格。

建议统一风格（以 `codebase_search_symbol` 为例）：

```text
returned_count: 2
total_count: 5
truncated: true

- path: "internal/runtime/run.go"
  line_hint: 42
  kind: function
  signature: "func ExecuteSystemTool(ctx context.Context, toolName string) (Result, error)"
```

`codebase_search_text` 统一风格：

```text
returned_count: 3
total_count: 12
truncated: true

- path: "internal/runtime/run.go"
  line_hint: 42
  match_count: 2
```

**硬约束原则**：`codebase_search_symbol` 和 `codebase_search_text` 均不返回代码实现内容（无 `snippet` 字段）。模型只能通过 `codebase_read` 获取原始代码内容。

这样设计的原因是：工具结果首先要让模型稳定消费，而不是优先追求人类肉眼 grep 风格；同时通过字段约束确保模型无法绕过 `codebase_read` 直接基于索引结果进行实现级推理。

### 6.6 删除 runtime 自动 retrieval 决策链路
删除 runtime 当前这类主职责：

1. 从 latest user text 提取路径锚点
2. 从 latest user text 提取符号锚点
3. 从 latest user text 提取引号文本锚点
4. 自动构建 codebase retrieval section 注入 context

runtime 保留的职责：

1. 暴露 `git_* / codebase_*` tool specs
2. 执行工具调用
3. 回灌 tool result
4. 在构建请求时保留迁移期的最小 Git Summary 注入
5. 删除当前自动 codebase retrieval 判定链路

这样设计的原因是：当模型已有正式工具入口后，runtime 再保留自动猜测主链路只会形成双轨系统。

### 6.7 `context` 退出 codebase retrieval 主链路
`context` 不再负责：

1. 渲染自动 `Repository Context` retrieval section
2. 长期消费 runtime 预先构造的 changed-files / retrieval 注入结果

`context` 在迁移期继续保留的职责：

1. 组装 prompt
2. 消费最小 `RepositorySummary` 投影，并继续映射到 `System State` 中的 Git 基础状态

这样设计的原因是：代码库详细信息改由工具回流给模型，而不是通过系统 prompt 默认注入；同时迁移期保留最小 Git Summary 可以降低行为突变风险。

### 6.9 Tree-sitter 索引初始化与维护
本节明确 Tree-sitter 在核心设计中的运行方式。

**初始化时机**：
- 在 `app` bootstrap 阶段，当 workspace 路径确定后，触发一次全量扫描。
- 扫描范围：workspace 根目录下所有文本文件，尊重 `.gitignore` 和 workspace 边界防护规则。
- 按扩展名映射到 Tree-sitter language grammar；无对应 grammar 的文件跳过，不报错。

**索引数据结构**：
- 内存中的 `map[string][]SymbolLocation`，key 为符号名（case-sensitive），value 为有序的位置列表。
- `SymbolLocation` 包含：`path`、`line_hint`、`kind`（function/class/method/variable/type）、`signature`（可选，如函数签名片段）。
- 同一份倒排索引同时容纳 Go AST 提取的符号和 Tree-sitter 提取的符号，上层查询无感知。

**增量更新触发条件**：
1. `git_changed_files` 检测到文件变更时，通知 Tree-sitter 索引器重新解析对应文件。
2. `codebase_search_symbol` 执行前，先检查索引中该文件的上次解析时间戳，若文件 mtime 更新则触发重解析。
3. 工具调用链（如 `codebase_read` 后文件被外部修改）不强制实时同步，允许短暂不一致，依赖下次查询前的惰性检查。

**失败处理**：
- grammar 加载失败：该语言所有文件降级为 whole-word 文本搜索，记录 warning 日志。
- 单文件解析失败（如语法错误严重到 Tree-sitter 无法恢复）：跳过该文件，不影响其他文件索引。
- 索引未初始化完成时收到 `codebase_search_symbol` 调用：阻塞等待初始化完成（通常 <1s 对于中小仓库），或返回 whole-word 搜索结果并附带提示。

**资源边界**：
- 索引驻留内存，不写入磁盘，进程退出即释放。
- grammar `.so`/`.dll` 插件按需加载，未使用的语言不占用内存。
- 单文件解析有 token/节点数上限，超大文件（如生成代码）超出上限时截断解析，避免 OOM。

### 6.8 明确与现有 `git_diff` verifier 的区别
保留现有 final verify 阶段的 `git_diff` verifier，不做语义复用。

其职责继续是：

1. 在收尾阶段执行 `git status`
2. 验证 edit/fix/refactor 任务是否真的产生改动
3. 作为交付证据和验收信号

新 `git_* / codebase_*` 工具的职责是：

1. 在推理阶段按需提供代码库上下文
2. 供模型决定下一步读什么、搜什么、看哪些改动
3. 为探索和分析服务，而不是为验收服务

这样设计的原因是：两者都可能使用 Git 状态，但处于不同阶段，解决不同问题，不能混为同一能力。

## 7. 与现有模块的关系

### 7.1 codebase / workspace 领域层
迁移后的中性 `internal/repository` 负责：

1. Git 摘要
2. changed-files
3. changed-snippets
4. path/text/symbol retrieval
5. `codebase_read` 所需的受限文件读取
6. 安全过滤与结果裁剪
7. 工作区指纹扫描与比较（`ScanWorkdir` / `DiffFingerprints`，从 `internal/checkpoint` 迁入）
8. 通用变更分类类型（`FileChangeKind` / `FileChangeEntry`，从 `internal/checkpoint` 迁入）

它不直接暴露给模型，只作为工具层依赖。迁移后依赖方向：

```
internal/repository/treesitter  ← 纯领域层，依赖 tree-sitter C 库
         ↑
internal/repository  ←  聚合 Go AST + Tree-sitter，不依赖项目内其他包
     ↑
     ├── internal/checkpoint  （fingerprint 改为依赖 repository）
     ├── internal/tools/git_* / codebase_* （工具直接依赖 repository）
     ├── internal/runtime     （bash write gate 通过 repository 使用 fingerprint）
     └── internal/context     （消费 repository 的投影结果）
```

`internal/repository` 内部对符号检索的路由逻辑（详见 6.9 索引数据结构）：
- 识别到 `.go` 文件 → 调用现有 Go AST 提取器
- 识别到 Tree-sitter 支持的语言 → 调用 `internal/repository/treesitter` 提取定义节点
- 未知语言或解析失败 → 直接回退到 whole-word 文本搜索

### 7.2 tools
`internal/tools` 成为 codebase / Git 能力唯一模型入口。

它负责：

1. 定义 `git_* / codebase_*` schema
2. 执行参数校验
3. 调用 `internal/repository`
4. 返回结构化结果

### 7.3 runtime
`runtime` 负责：

1. 暴露 `git_* / codebase_*` 到 tool specs
2. 删除当前自动 codebase retrieval 判定链路
3. 执行工具调用并回灌结果
4. 在迁移期保留最小 Git Summary 注入

它不再负责：

1. 猜测用户是否需要 codebase retrieval
2. 自动构造 codebase retrieval prompt section
3. 在用户文本上做路径/符号/引号锚点抽取来驱动代码库检索

### 7.4 context
`context` 继续只负责 prompt 组装，不再承载 codebase 检索能力或其自动注入链路。

迁移期内：

1. `context` 继续消费最小 `RepositorySummary`
2. `context` 不再消费自动 changed-files / retrieval 注入结果

这里需要明确：`context` 仍然可以保留 codebase / Git 信息的**投影类型与渲染结果**，但不应继续作为 codebase 领域实现的长期归属。
换言之，`context` 可以知道"要把哪些代码库事实展示给模型"，但不应该负责"这些代码库事实如何被发现、过滤、裁剪和读取"。

### 7.5 prompt assets
`prompt assets` 负责：

1. 在工具使用模板中明确 `git_* / codebase_*` 优先于 `filesystem_*`
2. 区分 Git 事实查询、代码库探索能力与通用文件操作能力
3. 让模型在探索阶段优先选择 `git_summary / git_changed_files / git_changed_snippets / codebase_read / codebase_search_text / codebase_search_symbol`
4. 在工具使用模板中写入硬约束策略：`codebase_search_symbol` 和 `codebase_search_text` **不返回代码实现内容**（无 `snippet`），仅返回位置与声明签名；任何对代码逻辑、实现细节的分析**必须**基于 `codebase_read` 的返回结果

### 7.6 app / bootstrap
`app` 负责：

1. 注入新的 codebase service 到 `git_* / codebase_*` 工具
2. 保持现有 `filesystem_*` 注册
3. 删除旧 repository context 注入装配链路
4. 注册 `git_* / codebase_*` 工具并纳入统一的 tool registry / permission / compact 管理链路

## 8. 测试场景
需要覆盖以下场景：

1. `git_summary` 在 Git 仓库和非 Git 目录下都能返回稳定结果。
2. `git_summary` 正确返回 `branch / ahead / behind / dirty / changed_file_count / representative_changed_files`。
3. `git_changed_files` 能正确覆盖 `added / modified / deleted / renamed / copied / untracked / conflicted`。
4. `git_changed_files` 默认不返回 snippets。
5. `git_changed_snippets` 能正确返回 diff/head snippet，并遵守单文件与总预算截断。
6. `.env`、`.npmrc`、`.aws/credentials`、`secrets.*`、二进制、大文件等在 `git_* / codebase_*` 下继续被过滤。
7. `codebase_read` 正确拦截 path traversal 和 symlink escape，并遵守受限读取边界。
8. `codebase_search_text` 返回稳定排序、稳定 `line_hint` 和正确 `truncated`。
9. `codebase_search_symbol` 正确执行 Go-first 匹配和 whole-word fallback。
10. runtime 不再基于用户文本自动构造 codebase retrieval section。
11. 迁移期 `RepositorySummary -> System State` 映射保持不变。
12. prompt assets 中工具使用策略改为 `git_* / codebase_*` 优先，避免继续默认引导模型优先走 `filesystem_read_file / filesystem_grep / filesystem_glob`。
13. 现有 `git_diff` verifier 保持不变，继续只服务 final verify 阶段。
14. `git_* / codebase_*` 与 `filesystem_*` 同时存在时，tool specs 正常暴露，普通工具执行链路不回归。
15. provider/tool loop 集成测试中，模型可以通过 `git_summary -> git_changed_files -> codebase_read / codebase_search_*` 完成典型 review/debug 流程。
16. 在非 Git workspace 下，`codebase_read / codebase_search_text / codebase_search_symbol` 仍可正常工作，`git_summary` 返回稳定降级结果。
17. `git_changed_files` 在非 Git workspace 下可通过 `Fingerprint`（`ScanWorkdir` + `DiffFingerprints`）提供降级的变更列表，`FileChangeKind / FileChangeEntry` 与 Git 场景保持统一语义。
18. `Fingerprint` 迁入 `internal/repository` 后，原有 checkpoint 的 bash write gate 功能不回归。
19. `PerEditSnapshotStore` 上的 `Diff()` / `ChangedFiles()` 方法仍留在 `internal/checkpoint`，不随 Fingerprint 迁出；`checkpoint` 自身的 per-edit 版本链功能不受影响。
20. Tree-sitter 正确加载 Python、Java、TypeScript、Rust 等 grammar，并提取函数、类、方法、变量定义到符号索引。
21. Tree-sitter 索引增量更新：文件内容变更后仅重新解析该文件，未变更文件的索引条目保持稳定。
22. Tree-sitter 符号索引未命中时（如无匹配符号名），自动回退到 whole-word 文本搜索，工具调用本身不失败。
23. 未知或 Tree-sitter 不支持的语言文件（如 `.lua`、`.erl`），`codebase_search_symbol` 直接走 whole-word 文本搜索，不报错、不阻塞。
24. 混合语言仓库中，`.go` 文件优先走 Go AST 路径，其他文件走 Tree-sitter 路径，两者共享同一个 `SearchSymbol` 入口，返回结果格式统一。
25. Tree-sitter 符号索引驻留内存，进程重启后重建；第一版不要求磁盘持久化。
26. `codebase_search_symbol` 返回结果中不包含 `snippet` 字段，只包含 `path`、`line_hint`、`kind`、`signature`；`codebase_search_text` 返回结果中不包含 `snippet` 字段，只包含 `path`、`line_hint`、`match_count`。
27. 模型在 tool loop 中基于 `codebase_search_symbol` 定位后，必须调用 `codebase_read` 才能获取代码实现内容；prompt 中的工具使用策略明确禁止模型基于索引结果直接进行实现级推理。
28. `codebase_read` 的返回结果在 context compact 时优先保留，`codebase_search_*` 的返回结果在 compact 时可被摘要或裁剪。

## 9. 假设与默认决策
1. 当前 codebase 核心能力已足够支撑第一版，除 `codebase_read` 外不重写底层逻辑。
2. 第一版不保留当前启发式自动 retrieval 作为主路径。
3. `codebase_search_symbol` 采用分层架构（Go AST → Tree-sitter → whole-word fallback），具体见 6.3 与第 10 节。
4. `git_* / codebase_*` 输出采用结构化优先文本格式。
5. `filesystem_*` 继续保留，但提示词中明确 `git_* / codebase_*` 优先。
6. `internal/context/repository` 不继续作为长期归属，应迁移为中性模块并由 tools 使用。
7. `verify/git_diff` 继续保留在 final verify 阶段，不并入推理期 Git / codebase 工具链。
8. 迁移期保留最小 Git Summary 注入，不再保留 changed-files / retrieval 自动注入。
9. prompt assets 的工具使用模板必须同步调整，否则 `git_* / codebase_*` 无法形成稳定主路径。
10. 如后续出现性能瓶颈，再单独讨论 repo map、索引或缓存，而不是在本 RFC 中预先设计。

## 10. 跨语言符号索引方案（Tree-sitter）

### 10.1 为什么引入 Tree-sitter
当前 `codebase_search_symbol` 是 Go-first 实现：先通过 Go AST 匹配符号定义，未命中时回退到 whole-word 文本搜索。这导致两个问题：

1. **跨语言盲区**：面对 Python、Java、TypeScript 等仓库时，符号检索退化为纯文本搜索，精度和结构化程度大幅下降。
2. **Token 效率低**：whole-word 搜索容易返回碎片化、不完整的代码片段，浪费模型上下文窗口。

**为什么不逐个语言引入外部解析库？**

每门语言都有官方或成熟的解析工具（Python 有 `ast`，Java 有 JavaParser，TypeScript 有 tsc API）。但逐个引入会带来三个问题：

1. **运行时灾难**：这些库大多依赖各自的运行时（Python 解释器、JVM、Node.js）。NeoCode 是一个单二进制 CLI，如果分析 Java 项目需要装 JVM，分析 TS 项目需要装 Node，就违背了"开箱即用"。
2. **API 完全不统一**：Python `ast`、JavaParser、tsc AST 的数据结构各不相同，NeoCode 需要为每种语言写完全不同的符号提取逻辑，维护成本极高。
3. **无增量更新**：这些官方解析器多为编译器设计，不支持"只改一行就更新 AST"，大文件反复全量解析性能差。

Tree-sitter 的价值在于**统一接口 + 无运行时依赖**：所有语言输出同一种树结构，同一种 `tags.scm` 查询语言，grammar 编译成 C 代码后直接嵌入进程，不需要外部运行时。

**为什么只有 Go 保留手写路径？**

Go 走手写不是因为 Tree-sitter 对 Go 支持差（tree-sitter-go 是官方维护的成熟 grammar，完全可用），而是因为两个工程现实：

1. **已有沉没成本**：当前 `codebase_search_symbol` 的 Go AST 实现已经工作正常、零 bug、零依赖。替换它要重新开发、测试、验证行为一致性，收益有限。
2. **Go 生态能做语义分析**：`go/ast` + `go/types` 可以推断接口实现关系（如"哪个 struct 实现了 `io.Reader`"）、包级作用域、构建标签过滤。这些属于语义层，Tree-sitter 只做语法层，无法替代。

其他语言没有这种特权——我们不会内嵌 JVM 做 Java 类型推断，也不会内嵌 Node 做 TS 类型检查。所以 Go 保留手写，其他语言统一走 Tree-sitter。

### 10.2 分层检索架构
`codebase_search_symbol` 采用四层 fallback 架构：

```
Layer 1: Go AST 快速路径（Go 项目专用，保留现有实现）
    ↓ 非 Go 文件或 AST 未命中
Layer 2: Tree-sitter 符号索引（通用语法级解析，30+ 语言）
    ↓ 语言无 grammar 或解析失败
Layer 3: 文件名/目录启发（通过路径、扩展名、import 语句做初步过滤）
    ↓ 仍无结果
Layer 4: whole-word 文本搜索（最终兜底）
```

**设计原则**：
- **不替换现有 Go-first 路径**：Go AST 能解析接口实现关系等 Go 特有语义，保留其作为专用快速路径。
- **Tree-sitter 是其他语言的统一入口**：一种实现覆盖 Python、Java、TypeScript、Rust 等主流语言。
- **未知语言不阻塞**：没有 grammar 的语言直接回退，工具调用不失败、不报错。

### 10.3 Tree-sitter 索引生命周期

Tree-sitter 索引的生命周期分为三阶段，具体实现机制（初始化时机、索引数据结构、增量更新触发条件、失败处理、资源边界）见 6.9。

- **初始化**：扫描 workspace 文件，按扩展名加载 grammar，执行 `tags.scm` query 提取定义节点，构建内存倒排索引。
- **增量更新**：通过 `Fingerprint` 或文件变更通知，仅重新解析变更文件，复用未变更子树。
- **查询**：先精确匹配符号名，未命中时子串匹配，最终返回 `path / line_hint / kind / signature` 结构化结果。

### 10.4 与现有模块的关系

**新增/调整模块**（与 7.1 统筹阅读）：

`internal/repository/treesitter`（Tree-sitter 索引器）：
- 负责 grammar 加载、文件解析、`tags.scm` query 执行。
- 维护内存中的符号倒排索引。
- 提供增量更新接口（`UpdateIndexForFile(path)` / `RemoveFile(path)`）。
- 不依赖项目内其他包，纯领域层。

`internal/repository`（调整）：
- 聚合 Go AST 符号提取（现有）和 Tree-sitter 符号提取（新增）。
- 对上层统一暴露 `SearchSymbol(name string) ([]SymbolHit, error)`，内部按语言路由。
- 负责 symbol 结果的安全过滤、裁剪和结构化输出。

### 10.5 与 Go-first 实现的衔接

| 场景 | 处理路径 |
|------|---------|
| `.go` 文件 | 优先走现有 Go AST 路径，解析包级符号、接口实现关系；未命中时走 Tree-sitter 或文本搜索 |
| `.py` / `.java` / `.ts` / `.rs` 等 | 走 Tree-sitter 路径，加载对应 grammar，提取符号定义 |
| 无 grammar 的扩展名 | 跳过 Tree-sitter，直接走 whole-word 文本搜索 |
| 混合语言仓库（如 Go + Python） | Go 文件走 Go AST，Python 文件走 Tree-sitter，共享同一个 `SearchSymbol` 入口 |

### 10.6 非目标

1. **不做跨文件类型解析**：Tree-sitter 只有语法解析，不做类型推断或跨文件引用解析（如"这个函数被谁调用"）。
2. **不做语义级调用图**：不构建完整的 call graph 或 dependency graph，只提取定义位置。
3. **不持久化索引到磁盘**：第一版索引驻留内存，进程重启后重建。后续如需要可扩展为磁盘缓存。
4. **不做 Embedding / 向量检索**：Tree-sitter 解决"知道名字找定义"的问题，"不知道名字找语义相似"的问题留给后续方案。
5. **不替代 LSP**：LSP 仍是可选增强层，Tree-sitter 是不依赖 LSP 的基础层。

### 10.7 后续可选增强（不纳入第一版）

1. **repo map / symbol map**：基于 Tree-sitter 提取的符号构建压缩仓库地图，按 PageRank 排序重要性，用于大仓库的全局上下文摘要。
2. **embedding 语义层**：在 Tree-sitter 分块基础上做 embedding，解决"不知道名字，只知道意图"的探索问题。
3. **IDE 诊断集成**：把 diagnostics、dirty buffers 等作为独立事实源。
4. **外部代码图服务**：对接 Sourcegraph 等企业级搜索，作为本地能力的上层增强。

这些增强不改变 Tree-sitter 作为基础符号层的定位。

## 11. 一句话结论
本方案不是给 NeoCode 新写一套 codebase / workspace 检索体系，而是在保留现有成熟实现的前提下，把其主语义收紧为 codebase / workspace 探索能力，并将其从 `context` 路径归属中迁出，改造成正式的 `git_* / codebase_*` 工具入口；模型自己决定何时读取 workspace 内容、Git 状态和当前改动，其中 `codebase_read` 作为新增底层能力补齐，runtime 启发式注入链路被删除，prompt assets 同步切换为 `git_* / codebase_*` 优先的代码库探索策略。同时，`codebase_search_symbol` 引入 Tree-sitter 作为跨语言符号索引层：Go 语言保留现有 AST 快速路径，其他语言通过 Tree-sitter grammar 统一提取符号定义，共享同一份倒排索引，未知语言回退到 whole-word 文本搜索，从而在无需 LSP 的前提下实现多语言仓库的符号级检索能力。检索工具执行硬约束：索引只返回位置与声明签名，不返回代码实现内容，模型必须调用 `codebase_read` 才能获取原始代码进行验证，从机制上消除基于索引结果绕过验证产生幻觉的可能。

## 12. 执行计划

以下是将本方案落地的分阶段执行计划。

### 12.1 阶段一：中性领域层迁移（`internal/repository`）

1. 新建 `internal/repository` 包，迁入 Git summary、changed-files、retrieval、`codebase_read`、Fingerprint、`FileChangeKind` 等能力。
2. `internal/checkpoint` 改为依赖 `internal/repository` 使用迁入的 `Fingerprint`。
3. `internal/runtime` 中 bash write gate 的指纹检测改为依赖 `internal/repository`。
4. `internal/context` 改为消费 `internal/repository` 的投影结果，不再直接执行检索。
5. `internal/context/repository` 保留薄封装作为迁移期兼容壳，阶段五再清理。

### 12.2 阶段二：Tree-sitter 索引层（`internal/repository/treesitter`）

1. 引入 `go-tree-sitter`（第一版使用纯 Go binding，避免自行维护 CGO）。
2. Grammar 采用**预编译动态库**分发（`.so`/`.dll`/`.dylib`），按 OS/Arch 组织目录，用户侧零编译。
3. 定义核心类型 `Indexer`、`SymbolLocation`，实现 `BuildIndex`、`UpdateFile`、`RemoveFile`、`SearchSymbol`。
4. 在 `internal/repository/treesitter/queries/` 下维护各语言 `tags.scm`。
5. 利用已有 `Fingerprint` 实现文件级增量更新。
6. 失败降级：grammar 加载失败 → 文本搜索；单文件解析失败 → 跳过；索引未就绪 → 阻塞或降级。

### 12.3 阶段三：符号检索路由（`internal/repository` 聚合层）

1. `Repository.SearchSymbol` 内部路由：`.go` 文件优先走现有 Go AST 路径，其他语言走 Tree-sitter，未知语言回退文本搜索。
2. 结果合并后统一应用安全过滤与裁剪。
3. `codebase_search_text` 删除 `snippet` 返回字段，仅返回 `path`、`line_hint`、`match_count`。
4. `codebase_search_symbol` 返回字段限定为 `path`、`line_hint`、`kind`、`signature`，`signature` 上限 **512 字符**。

### 12.4 阶段四：工具入口与 Prompt 策略（`internal/tools` + `prompt assets`）

1. 在 `internal/tools` 定义 `git_* / codebase_*` schema 并注册到 tool registry。
2. Runtime 删除自动 retrieval 决策链路，保留迁移期最小 Git Summary 注入。
3. Prompt assets 中明确 `git_* / codebase_*` 优先，并写入硬约束策略：索引工具不返回代码实现内容，任何实现级分析必须基于 `codebase_read`。
4. Compact 策略调整：`codebase_read` 结果高优先级保留，`codebase_search_*` 可被裁剪。

### 12.5 阶段五：测试与清理

1. 单元测试：treesitter 索引、repository 路由、tools schema。
2. 集成测试：provider/tool loop 典型 review 流程、混合语言仓库、非 Git workspace 降级。
3. 清理旧路径：删除 `internal/context/repository` 兼容壳、删除 runtime 废弃代码、更新 prompt assets。

### 12.6 关键决策

| 决策 | 结论 |
|------|------|
| `signature` 长度上限 | **512 字符**，覆盖 99.9% 函数签名，避免泛型/长参数列表截断 |
| Grammar 分发 | **预编译动态库**，用户零编译，首次启动快 |
| Tree-sitter 绑定 | **第一版用 `go-tree-sitter`**，后续按需评估直接 CGO |
| Go AST 路径 | **第一版保留现有实现不动**，通过 `Repository` 层简单路由聚合，3+ 语言时评估是否抽象统一接口 |
