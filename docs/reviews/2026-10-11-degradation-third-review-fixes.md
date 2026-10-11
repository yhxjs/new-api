# 降级检测第三轮审查修复

用户已授权修复三项审查发现；保留工作区现有改动，不执行任何 Git 写操作。

## 设计与执行计划

1. 设置表单只提交用户修改的字段，防止旧草稿覆盖其他管理员对未编辑字段的更新；保留保存过程中编辑、部分失败和重试行为。探针与目标 JSON 的已刷新外部冲突在任何写入前阻止提交，Reset 重新加载。保存中新编辑的字段保留其原始草稿基准，只有确认成功写入的字段才按提交值更新基准；部分失败等待新读取完成，读取失败时再次保存 JSON 先重读，持续失败保留草稿并阻止写入。先增加真实交互回归，确认失败，再修改保存逻辑并运行模块测试、类型检查及 lint。
2. 全轮任务没有保存可验证的执行目标快照，无法证明覆盖单目标请求；明确返回已有忙碌提示，允许完成后重试，保留同一单目标任务的合并。先增加巡检开始后新增目标的接口回归，确认失败，再删除基于最新配置推断覆盖的逻辑。
3. 探针 ID 与名称按原始保存值计算长度，避免空白绕过列宽约束。先增加带空白和多字节字符的边界回归，确认失败，再修改校验并运行设置与 controller 测试。

验证范围：根模块构建、controller/router/service/config 相关测试、前端设置相关测试及类型/lint/格式检查；本轮不修改模型、数据库查询、迁移或 relaykit。

## 验证记录

以下命令和结果均来自本轮实施后的实际执行。

### 回归验证

- 前端原始问题先复现 timeout 被从其他管理员保存的 45 覆盖回 30，再确认修复通过。独立复核额外发现的保存中新 JSON 草稿、部分成功但 GET 仍挂起、刷新失败恢复三类边界，也分别确认失败用例转为通过。
- 后端新增目标的 running/pending 全轮接口回归先确认错误返回“已合并”，修复后均返回原有 busy/retry 提示；running 用例还验证新目标确实不在旧轮执行日志中。
- 探针原始 ID/名称的前后空白超长输入先确认错误放行，修复后拒绝；恰好上限的 Unicode/空白输入继续通过。

### 后端

在仓库根目录执行：

```powershell
go test ./controller ./router ./model ./service ./setting/config ./setting/degradation_setting -count=1 -timeout 180s
go build ./...
gofmt -l controller/degradation.go controller/degradation_test.go controller/degradation_task_test.go setting/degradation_setting/config.go setting/degradation_setting/config_test.go
```

六个测试包和根模块构建均退出码 0；gofmt 无待格式化文件。独立 reviewer 另运行 controller 与 degradation_setting 的定向回归，两包通过。

### 前端

Bun 不在 PATH，使用已有 Node CLI 与原生 tsgo 执行等价命令，未安装或更新依赖。在 `web/` 执行：

```powershell
$env:NODE_PATH = 'C:\Users\yhxjs\AppData\Local\Temp\newapi-native-deps-20261010\node_modules'
node node_modules/vitest/vitest.mjs run src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx src/features/system-settings/models/__tests__/degradation-binding-dialog.test.tsx src/features/system-settings/models/__tests__/probe-regex-input.test.tsx
& node_modules/@typescript/native-preview-win32-x64/lib/tsgo.exe -b
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx
node node_modules/@rsbuild/core/bin/rsbuild.js build
```

最终 3 个测试文件、38 项测试全部通过，其中 persistence 24 项；类型检查、lint、最终生产构建均退出码 0。Vitest 有宿主 jsdom 既有的 ESM ExperimentalWarning，没有测试错误。

格式检查调用 `oxfmt.format()`，对两份文件正文按 `.oxfmtrc.json` 格式化，并逐字节保留原版权头；最终检查格式错误列表为空、正文与格式化结果相同、第二次格式化幂等，退出码 0。

### 翻译与差异检查

- 通过临时 `add-missing-keys.mjs` 脚本新增七语冲突提示，再执行 `node scripts/sync-i18n.mjs`；每种语言的 missing/extras/untranslated 数量均为 0。临时脚本已删除。
- 对照插入前备份，en、zh、zh-TW、fr、ja、ru、vi 均仅新增同一提示键；原有翻译与署名内容保持一致。
- `git diff --check` 退出码 0。七份本轮源文件另逐一执行 `git diff --no-index --check -- <基线文件> <当前文件>`，无空白问题诊断；此命令的退出码 1 表示两文件有差异。
- 前后端独立只读 reviewer 最终均未发现剩余阻塞问题。保留工作区此前所有改动，未执行 Git 写操作。

### 验证边界

JSON 冲突检查依据客户端已读取的服务端快照；接口仍为逐键保存，没有加入服务端版本锁或跨键事务。本轮没有改动数据库模型、SQL、迁移或 relaykit，未重跑真实三库矩阵与最低数据库版本验证。
