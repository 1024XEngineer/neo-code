# HTTP URL 唤醒使用指南（用户故事版）

本指南面向最终用户，目标是让你在文档、网页、IM 聊天里点击一个链接，就能拉起 NeoCode 并接管会话。

---

## 你会得到什么

- 点击 `http://neocode:18921/run?...` 可直接发起一次新任务。
- 点击 `http://neocode:18921/review?...` 可直接发起文件审查任务。
- 首次点击会自动创建会话并执行。
- 后续点击带 `session_id` 的链接会直接进入同一会话，不重复发送提示词。

---

## 一分钟理解工作流

1. 浏览器打开链接（`run` 或 `review`）。
2. 本机 daemon 收到请求并转发到 Gateway。
3. Gateway 返回 `session_id`。
4. daemon 拉起 `neocode --session <session_id>`。
5. 首次点击时，TUI 启动后自动走标准 Submit 链路执行任务。
6. 复点（带 `session_id`）时，只接管会话，不重复执行。

---

## 0. 首次准备（只做一次）

### 0.1 安装并注册 daemon 自启动

```bash
neocode daemon install
```

说明：

- 会配置用户态自启动。
- 会 best-effort 写入 hosts 别名（`127.0.0.1 neocode`）。

### 0.2 启动 daemon（开发期可手动）

```bash
neocode daemon serve
```

默认监听：`127.0.0.1:18921`

### 0.3 查看状态

```bash
neocode daemon status
```

---

## 1. 用户故事 A：首次点击 run 链接并开始执行

场景：你在需求文档里放一个“让 NeoCode 直接开始做事”的链接。

### 1.1 生成可点击链接（推荐）

```bash
neocode daemon encode run --prompt "实现一个最小 HTTP 服务" --workdir "C:\project"
```

示例输出：

```text
http://neocode:18921/run?prompt=%E5%AE%9E%E7%8E%B0%E4%B8%80%E4%B8%AA%E6%9C%80%E5%B0%8F+HTTP+%E6%9C%8D%E5%8A%A1&workdir=C%3A%5Cproject
```

### 1.2 点击链接后的预期

- 弹出一个成功页，页面会显示：
  - `session_id`
  - `reusable_url`（可复用链接）
- 自动拉起 TUI。
- TUI 会显示完整思考/流式过程（与手动输入一致）。

---

## 2. 用户故事 B：首次点击 review 链接发起文件审查

场景：你在代码评审文档里放一个“审查这个文件”的链接。

### 2.1 生成 review 链接

```bash
neocode daemon encode review --path "internal/gateway/bootstrap.go" --workdir "C:\project"
```

示例输出：

```text
http://neocode:18921/review?path=internal%2Fgateway%2Fbootstrap.go&workdir=C%3A%5Cproject
```

### 2.2 review 首次点击的执行语义

- 系统会自动组装输入：`请审查文件 internal/gateway/bootstrap.go`
- 然后按标准 Submit 链路执行（不是旁路执行）。

### 2.3 review 参数规则（首次）

- `path` 必填。
- `workdir` 必填（除非你传了 `session_id`）。
- `path` 必须是安全相对路径（不能绝对路径、不能 `..` 越界）。

---

## 3. 用户故事 C：复用同一会话（不重复发送提示词）

场景：你第一次点击后结果不错，想让团队后续都续接同一个会话。

### 3.1 使用成功页里的 `reusable_url`

成功页会给出带 `session_id` 的链接，形如：

```text
http://neocode:18921/run?prompt=...&workdir=...&session_id=session_xxx
```

### 3.2 带 `session_id` 的行为

- 只接管会话（打开同一 session）。
- 不会再次自动发送 `prompt/path`。
- 适合“继续对话”和“多人协同续接”。

---

## 4. 推荐实践（避免踩坑）

1. 所有要放进文档/IM 的链接都先用 `neocode daemon encode ...` 生成。
2. `review` 首次点击一定显式带 `workdir`，避免审错仓库。
3. 首次执行后，把成功页里的 `reusable_url` 保存到文档，后续统一点它。
4. Windows 路径、中文、空格都不要手写拼接，交给 `encode` 命令处理。

---

## 5. 常见问题排查

### Q1：点击后提示 `forbidden host`

原因：Host 不在白名单。  
允许的 Host：`neocode`、`localhost`、`127.0.0.1`。

### Q2：提示 `missing required query: prompt`

原因：`run` 首次点击没带 `prompt`。  
处理：补上 `prompt`，或直接使用 `daemon encode run`。

### Q3：提示 `missing required query: workdir or session_id`

原因：`review` 首次点击没带 `workdir` 且没带 `session_id`。  
处理：补 `workdir`，或使用已有 `session_id` 的复用链接。

### Q4：提示 `wake session not found`

原因：链接里的 `session_id` 在本机不存在。  
处理：重新走一次首次点击，获取新的 `session_id`。

### Q5：Linux 下没有自动弹终端

当前 Linux 终端自动拉起能力受限。  
可手动执行：

```bash
neocode --session <session_id>
```

---

## 6. 命令速查

```bash
# 启动 daemon
neocode daemon serve

# 安装自启动
neocode daemon install

# 卸载自启动
neocode daemon uninstall

# 查看状态
neocode daemon status

# 生成 run 链接
neocode daemon encode run --prompt "..." --workdir "..."

# 生成 review 链接
neocode daemon encode review --path "..." --workdir "..."
```

