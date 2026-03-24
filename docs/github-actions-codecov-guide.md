# GitHub Actions + Codecov 使用指南

本文面向第一次接触 CI 的同学，结合本仓库当前的 Go 项目配置，讲清楚 GitHub Actions 和 Codecov 是什么、我们为什么使用它们、如何完成首次接入、日常怎么看结果，以及出了问题应该怎么排查。

---

## 1. 先用一句话理解这两者

- GitHub Actions：GitHub 自带的自动化平台。我们把“拉代码、安装依赖、编译、测试、上传结果”写成一个 YAML 文件后，GitHub 会在 PR、push 等事件发生时自动执行。
- Codecov：专门看测试覆盖率的平台。它读取测试生成的覆盖率报告，告诉我们“哪些代码被测到了，哪些没测到”。

可以把它们理解为：

- GitHub Actions = 自动执行流水线的工人
- Codecov = 专门分析测试覆盖率的质检员

---

## 2. 我们为什么要引入它们

在没有 CI 之前，团队通常会遇到这些问题：

- 有人本地没跑测试就提 PR
- 有人本地能过，换台机器就不过
- 看不出一个改动有没有让覆盖率下降
- reviewer 需要手工确认“有没有编译、有没有测试、有没有新增未覆盖代码”

引入 GitHub Actions + Codecov 后：

- 每次 PR 创建或更新时，自动 build 和 test
- 覆盖率自动上传并显示在 PR 或 Codecov 页面里
- reviewer 不需要先相信“我本地跑过了”，而是直接看系统结果
- 可以进一步配合分支保护，要求 CI 必须通过后才能合并

---

## 3. GitHub Actions 的核心概念

如果你第一次看 `.github/workflows/*.yml`，建议先记住下面 6 个词：

### 3.1 workflow

一个 workflow 就是一份自动化流程文件。

例如本仓库的：

- `.github/workflows/ci.yml`

GitHub 会读取这里面的配置，并在指定时机执行。

### 3.2 event

event 是“什么事情发生时触发 workflow”。

常见事件：

- `pull_request`：有人创建或更新 PR
- `push`：有人把代码推到分支
- `workflow_dispatch`：手动点按钮执行

### 3.3 job

job 是一组步骤的集合。一个 workflow 可以有一个或多个 job。

例如：

- `build-test`

它表示“在一台 runner 上完成 build、test、coverage 上传”。

### 3.4 step

step 是 job 内的一步操作。

例如：

- checkout 代码
- 安装 Go
- 执行 `go build ./...`
- 执行 `go test ./...`
- 上传覆盖率到 Codecov

### 3.5 action

action 是别人或官方封装好的可复用步骤。

例如本仓库使用了：

- `actions/checkout`
- `actions/setup-go`
- `codecov/codecov-action`

### 3.6 runner

runner 是实际执行 workflow 的机器。

本仓库现在用的是：

- `ubuntu-latest`

也就是 GitHub 提供的 Linux 虚拟机。

---

## 4. Codecov 的核心概念

### 4.1 覆盖率是什么

覆盖率不是“代码质量”的全部，但它是一个非常有用的信号。

它主要回答：

- 这段代码有没有被测试执行到
- 这次 PR 新增的代码有没有测试覆盖

注意：

- 高覆盖率不等于高质量
- 低覆盖率也不一定代表代码有问题
- 但完全没有覆盖率数据时，团队会缺少一个很重要的客观信号

### 4.2 Codecov 看什么

Codecov 通常会看两类覆盖率：

- Project coverage：整个项目当前的整体覆盖率
- Patch coverage：这次 PR 新增或修改的代码覆盖率

Patch coverage 对 PR review 很有价值，因为它更贴近“这次改动是否被测试到”。

### 4.3 Codecov 需要什么输入

Codecov 自己不会跑测试，它只负责“接收和分析报告”。

所以流程是：

1. GitHub Actions 先跑测试
2. 测试生成覆盖率文件
3. Codecov Action 上传覆盖率文件
4. Codecov 解析并展示结果

对于 Go 项目，最常见的覆盖率文件是：

- `coverage.out`

---

## 5. 本仓库现在的 CI 做了什么

本仓库当前 CI 文件是：

- `.github/workflows/ci.yml`

主要流程如下：

1. 在 PR 创建、更新、重新打开、标记为 Ready for review 时自动触发
2. 如果 PR 仍然是 draft，则整个 job 跳过
3. 拉取仓库代码
4. 根据 `go.mod` 安装 Go 并启用缓存
5. 执行 `go build ./...`
6. 执行 `go test ./... -covermode=atomic -coverprofile=coverage.out`
7. 把 `coverage.out` 上传到 Codecov

简化后的执行顺序可以理解为：

```text
PR
  -> GitHub Actions 触发
  -> Draft PR 时跳过 job
  -> Checkout 代码
  -> Setup Go
  -> Build
  -> Test + 生成 coverage.out
  -> 上传到 Codecov
  -> PR 上看到 CI / Coverage 结果
```

---

## 6. 首次接入操作手册

这一节是第一次配置时最重要的部分。

### 6.1 第一步：确认仓库里已经有 workflow 文件

当前仓库应存在：

- `.github/workflows/ci.yml`

如果这个文件已经在默认分支中，GitHub Actions 就具备运行入口了。

### 6.2 第二步：启用 GitHub Actions

通常仓库第一次加 workflow 后，GitHub 会自动识别。

你可以这样确认：

1. 打开仓库首页
2. 点击顶部 `Actions`
3. 如果能看到 workflow 列表或运行记录，说明 Actions 已生效

### 6.3 第三步：注册并接入 Codecov

建议由仓库管理员完成。

步骤：

1. 打开 [Codecov](https://codecov.io/)
2. 使用 GitHub 账号登录
3. 安装或授权 Codecov GitHub App
4. 选择要接入的仓库

如果组织安装 Codecov GitHub App 时选择了“Only Select Repositories”，记得把本仓库勾上。

### 6.4 第四步：获取 Codecov Token

在 Codecov 中进入对应仓库后，找到仓库配置页的 General 区域，可以看到 Repository upload token。

官方说明里提到，Repository upload token 可以在仓库配置页查看。

### 6.5 第五步：把 Token 配到 GitHub Secrets

在 GitHub 仓库里：

1. 打开 `Settings`
2. 打开 `Secrets and variables`
3. 选择 `Actions`
4. 点击 `New repository secret`
5. 名称填写：`CODECOV_TOKEN`
6. 值填写：刚才从 Codecov 复制出来的 token

注意：

- 只粘贴 token 值本身
- 不要写成 `CODECOV_TOKEN=xxxx`

### 6.6 第六步：发一个 PR 验证配置

推荐做法：

1. 新建一个小分支
2. 做一个很小的改动
3. 提交并创建 PR
4. 观察 `Actions` 页面是否开始跑 `CI`
5. 观察 PR 页面是否出现 build/test 状态
6. 观察 Codecov 页面是否出现新的覆盖率上传记录

---

## 7. 如何阅读当前 `ci.yml`

下面按块解释本仓库当前配置。

说明：

- 本节所有 YAML 片段都以仓库当前已经提交的 `.github/workflows/ci.yml` 为准。
- 如果你在其他教程里看到 `actions/checkout@v4` 或 `actions/setup-go@v5`，那通常只是当时常见的示例版本；阅读本仓库文档时，应优先以仓库实际 CI 配置为准。
- 当前仓库使用 `actions/checkout@v5` 与 `actions/setup-go@v6`，文档会与这份实际配置保持同步。

### 7.1 触发条件

```yaml
on:
  pull_request:
    types:
      - opened
      - synchronize
      - reopened
      - ready_for_review
```

含义：

- 创建 PR 时跑
- PR 有新提交时跑
- 关闭后重新打开 PR 时跑
- 草稿 PR 转为正式评审时跑

为什么这样配：

- PR 阶段保证改动被检查
- 当前工作流先聚焦 PR 质量门禁，保持简单稳定

### 7.2 Draft PR 跳过

```yaml
jobs:
  build-test:
    if: github.event.pull_request.draft == false
```

含义：

- 如果当前 PR 还是草稿状态，`build-test` job 不执行

好处：

- 减少 draft PR 反复更新时的 CI 消耗
- 等作者准备好进入评审，再正式跑完整检查

### 7.3 并发控制

```yaml
concurrency:
  group: ci-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true
```

含义：

- 同一个 PR 如果连续 push 多次，旧的 CI 会被自动取消
- 只保留最新一次运行

好处：

- 节约 GitHub Actions 分钟数
- 避免 reviewer 看一堆过期结果

### 7.4 权限控制

```yaml
permissions:
  contents: read
```

含义：

- workflow 只需要读取仓库内容，不需要写权限

这是一种更安全、更易维护的默认做法。

### 7.5 Checkout

```yaml
- name: Checkout repository
  uses: actions/checkout@v5
```

作用：

- 把仓库代码拉到 runner 上

没有这一步，后续 build/test 都没有源码可执行。

### 7.6 Setup Go

```yaml
- name: Setup Go
  uses: actions/setup-go@v6
  with:
    go-version-file: go.mod
    cache: true
```

作用：

- 根据 `go.mod` 选择 Go 版本
- 开启 Go 依赖缓存

为什么推荐这样做：

- 避免把 Go 版本写死两份
- 与仓库当前依赖状态绑定，维护成本更低

版本说明：

- 当前仓库的 CI 已经使用 `actions/setup-go@v6`
- 同样，前面的 checkout 步骤使用的是 `actions/checkout@v5`
- 如果后续团队决定升级这些 Action 版本，应优先修改 `.github/workflows/ci.yml`，再同步更新本指南

### 7.7 Build

```yaml
- name: Build
  run: go build ./...
```

作用：

- 编译仓库中所有 Go 包

它主要防止：

- 编译错误
- import 问题
- 某些包只在测试外路径上才会暴露的问题

### 7.8 Test with coverage

```yaml
- name: Test with coverage
  run: go test ./... -covermode=atomic -coverprofile=coverage.out
```

作用：

- 跑所有测试
- 生成覆盖率文件 `coverage.out`

参数说明：

- `./...`：递归测试所有包
- `-coverprofile=coverage.out`：把覆盖率结果写到文件
- `-covermode=atomic`：Go 官方常见覆盖率模式之一，适合并发场景，结果更稳健

### 7.9 Upload coverage to Codecov

```yaml
- name: Upload coverage to Codecov
  uses: codecov/codecov-action@v5
  with:
    files: ./coverage.out
    flags: unittests
    fail_ci_if_error: true
  env:
    CODECOV_TOKEN: ${{ secrets.CODECOV_TOKEN }}
```

作用：

- 把当前生成的 `coverage.out` 上传到 Codecov

几个关键字段：

- `files`: 告诉 Codecov 上传哪个覆盖率文件
- `flags`: 给这次上传打标签，方便以后做多套测试拆分
- `fail_ci_if_error: true`: 如果上传失败，CI 也失败，避免“测试过了但覆盖率丢了”
- `CODECOV_TOKEN`: 通过 `env` 传给上传步骤，用于让 Codecov 识别当前仓库

---

## 8. 当前配置对 fork PR 的影响

当前 workflow 的上传步骤直接依赖：

- `CODECOV_TOKEN`

这意味着：

- 在本仓库内部正常提 PR 时，通常没有问题
- 如果将来是 fork 仓库向主仓提 PR，GitHub Actions 默认可能拿不到主仓 secrets
- 一旦拿不到 `CODECOV_TOKEN`，Codecov 上传步骤就可能失败

这不是当前配置“错了”，而是它更适合：

- 仓库内部协作
- 先把主流程跑通

如果未来团队希望更稳妥地支持 fork PR，可以再做增强，例如：

- 对 fork PR 跳过上传步骤
- 或者在公开仓库结合 Codecov 的 tokenless upload 能力重新设计上传策略

---

## 9. 组员日常怎么使用

对大多数开发同学来说，日常只需要会下面这套流程。

### 9.1 提交代码前

建议本地先跑：

```bash
go build ./...
go test ./...
```

理由：

- CI 不是替代本地检查
- CI 更像“最后一道统一验证”

### 9.2 创建 PR 后

组员需要做的事情：

1. 打开 PR
2. 看 GitHub 页面里的检查状态是否开始运行
3. 等 `CI` 结果出来
4. 若失败，点进日志定位问题
5. 若通过，再进入代码评审

### 9.3 Reviewer 看什么

reviewer 最少看三件事：

1. CI 是否通过
2. 是否有测试
3. Codecov 显示的 patch coverage 是否明显异常下降

### 9.4 合并前

建议团队约定：

- CI 不通过，不合并
- 覆盖率上传失败，要么修好，要么说明原因
- 对高风险改动，关注 patch coverage，而不仅是整体 project coverage

---

## 10. 如何查看运行结果

### 10.1 在 GitHub 里看

位置：

- 仓库首页 -> `Actions`

你会看到：

- 哪次运行成功
- 哪次运行失败
- 每一步花了多久
- 每一步的详细日志

### 10.2 在 PR 页面看

PR 页面通常会显示检查状态，比如：

- 正在运行
- 成功
- 失败

点击对应检查名，可以直接跳到具体日志。

### 10.3 在 Codecov 里看

在 Codecov 仓库页面里，通常可以看到：

- 当前整体覆盖率
- 与基线相比是升是降
- 每次提交或 PR 的覆盖率变化
- 哪些文件覆盖率高，哪些低

如果启用了 GitHub Checks 或状态检查，还可能在 PR 中直接看到 coverage 相关信息。

---

## 11. 最常见的失败场景与排查方法

这一节非常适合组员收藏。

### 11.1 `go build ./...` 失败

常见原因：

- 代码编译错误
- 缺失 import
- 条件编译或平台差异

排查方式：

1. 打开 Actions 日志
2. 找 `Build` 步骤
3. 看失败的包名和报错行
4. 在本地复现 `go build ./...`

### 11.2 `go test ./...` 失败

常见原因：

- 单元测试本身失败
- 与本地环境不一致
- 依赖了本地文件、环境变量、网络

排查方式：

1. 打开 `Test with coverage` 步骤
2. 查看是哪个 package 失败
3. 本地执行同样命令复现
4. 尽量把测试改成不依赖外部环境

### 11.3 Codecov 上传失败

常见原因：

- 没配置 `CODECOV_TOKEN`
- token 填错
- 覆盖率文件路径不对
- 覆盖率文件没生成

排查顺序：

1. 看 `coverage.out` 是否在测试步骤生成
2. 看 workflow 里 `files` 路径是否正确
3. 看 GitHub Secrets 是否存在 `CODECOV_TOKEN`
4. 去 Codecov 仓库配置页重新复制 token

### 11.4 fork PR 为什么没上传覆盖率

因为 fork PR 默认不能访问仓库 secrets。

这不是配置错了，而是 GitHub 的安全机制。

如果将来你们是公开仓库，并且组织愿意在 Codecov 中开启 public repo tokenless upload，再来调整策略。

### 11.5 为什么本地能过，CI 不能过

常见原因：

- 本地缓存影响
- 本地 Go 版本和 CI 不一致
- 本地有未提交文件
- 本地环境变量不同

建议：

- 尽量让本地与 CI 使用同一 Go 版本
- 避免测试依赖机器特有环境
- 优先相信“干净环境”下的 CI 结果

---

## 12. 如何给团队建立统一约定

建议团队落地下面这些规则：

### 12.1 开发约定

- 提 PR 前至少本地跑一次 `go test ./...`
- 新增逻辑尽量补测试
- 如果 CI 失败，提 PR 的人优先修复

### 12.2 评审约定

- reviewer 默认检查 CI 状态
- 对新增逻辑关注 patch coverage
- 对覆盖率下降明显的 PR，要求说明原因

### 12.3 主分支约定

- 开启 branch protection
- 要求 `CI` 必须通过
- 禁止绕过检查直接合并

---

## 13. 推荐的后续增强

当前这套方案已经足够做稳定的基础 CI，但后续还可以继续增强。

### 13.1 增加格式化或静态检查

例如：

- `go fmt ./...`
- `go vet ./...`
- `golangci-lint run`

如果团队后续希望把“能编译、能测试”进一步升级成“基础质量门禁”，最推荐优先增加 `golangci-lint`。它是 Go 社区非常常见的综合静态分析工具，覆盖的问题范围通常比 `go vet` 更广，也更适合在 PR 阶段做统一检查。

可以在 GitHub Actions 中增加类似步骤：

```yaml
- name: Lint
  uses: golangci/golangci-lint-action@v6
  with:
    version: latest
```

### 13.2 增加 PR 注释或状态门禁

例如通过 Codecov 配置：

- 限制 patch coverage 不得低于某个阈值
- 限制 project coverage 不得异常下降

### 13.3 增加矩阵测试

例如：

- 同时测试多个 Go 版本
- 不同操作系统 runner

但在项目早期，优先推荐保持简单稳定，不要一上来就配得过重。

---

## 14. 常见问答

### Q1：GitHub Actions 会自动修代码吗

不会。

它只会按你写的步骤执行命令，并返回结果。

### Q2：Codecov 会帮我生成测试吗

不会。

它只会分析你已经生成的覆盖率报告。

### Q3：覆盖率高就代表代码没问题吗

不代表。

覆盖率只能说明“执行到了多少代码”，不能自动证明断言质量和业务正确性。

### Q4：为什么我们还要本地跑测试

因为本地提前发现问题比等 CI 更快，CI 是统一验证，不是替代本地自检。

### Q5：是不是必须先学会 YAML 才能维护 CI

不用一开始就很精通。

大多数日常维护只需要会看：

- 触发条件
- 执行步骤
- 哪一步失败了

---

## 15. 一份给组员的最短操作版

如果组员只想看最短版本，可以直接看这一段。

### 开发者

1. 本地先跑 `go build ./...` 和 `go test ./...`
2. 提交代码并创建 PR
3. 等 GitHub Actions 自动跑完
4. 如果 `CI` 失败，打开日志修复
5. 如果通过，再发起 review

### Reviewer

1. 看 PR 上的 `CI` 是否通过
2. 看是否有对应测试
3. 看 Codecov 是否提示覆盖率明显下降
4. 没问题再合并

### 管理员

1. 确保 `.github/workflows/ci.yml` 在默认分支
2. 确保 Codecov GitHub App 已接入仓库
3. 确保 `CODECOV_TOKEN` 已配置
4. 建议启用 branch protection 和 required status checks

---

## 16. 官方参考资料

以下都是本指南整理时参考的官方文档，建议收藏。

- GitHub Actions 概念总览  
  https://docs.github.com/en/actions/get-started/understand-github-actions

- GitHub Workflow 基本说明  
  https://docs.github.com/en/actions/concepts/workflows-and-actions/workflows

- GitHub 官方 Go CI 教程  
  https://docs.github.com/en/actions/tutorials/build-and-test-code/go

- GitHub Secrets 官方说明  
  https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets

- GitHub 分支保护规则说明  
  https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches

- GitHub 必要状态检查排错  
  https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks

- Codecov GitHub Action 官方仓库  
  https://github.com/codecov/codecov-action

- Codecov GitHub 入门与上传覆盖率  
  https://docs.codecov.com/docs/github-2-getting-a-codecov-account-and-uploading-coverage

- Codecov Token 官方说明  
  https://docs.codecov.com/docs/codecov-tokens

- Codecov 添加 Token 官方说明  
  https://docs.codecov.com/docs/adding-the-codecov-token

- Codecov 状态检查说明  
  https://docs.codecov.com/docs/commit-status

- Codecov GitHub Checks 说明  
  https://docs.codecov.com/docs/github-checks

---

## 17. 给本仓库的结论

对本仓库来说，当前接入路线非常清晰：

1. 使用 GitHub Actions 统一执行 build 和 test
2. 使用 Go 原生覆盖率输出 `coverage.out`
3. 使用官方 `codecov/codecov-action` 上传覆盖率
4. 使用 `CODECOV_TOKEN` 作为最稳妥的认证方式
5. 后续再按团队需要追加 branch protection、Codecov status checks 和更多质量门禁

如果你是第一次接触 CI，不需要一口气把所有高级功能都学完。先把下面这条主线吃透就够了：

```text
代码改动 -> 发 PR -> GitHub Actions 自动 build/test -> 生成 coverage.out -> 上传到 Codecov -> reviewer 看结果 -> 合并
```

只要这条链路跑顺了，团队协作效率和质量可见性就会明显提升。
