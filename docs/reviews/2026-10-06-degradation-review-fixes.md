# 降智检查最新审查修复记录

日期：2026-10-06。本轮修复最近审查的四项问题，并覆盖复核发现的同一渠道读取权限边界下的概览详情入口。保留此前工作区改动，未执行 Git 写操作。

## 修复结果

| 问题 | 修改与回归 |
| --- | --- |
| 导航关闭检查发生在控制器之后 | 概览路由先执行 UserAuth，再独立检查导航开关。测试断言普通用户返回 403 且处理器未执行、匿名返回 401、管理员保留导航豁免。 |
| 巡检接口绕过渠道权限 | 两个运行接口要求 ChannelOperate，日志要求 ChannelRead；概览详细视图也要求管理员角色和 ChannelRead。撤销权限后不能创建任务、读取完整日志或从已有概览缓存读取敏感详情。授权管理员、root、普通用户和两种拓扑配置均有回归覆盖。 |
| 配置替换继承旧切片字段 | 切片在新值中解码，成功后整体替换。省略字段清零、目标重排或更换不会继承旧模型与绑定、无效 JSON 保留旧值；UpdateConfigFromMap 和 LoadFromDB 均覆盖。Struct 更新语义未改变。 |
| 日志请求失败被显示为空记录 | API 将业务失败作为异常；弹窗显示错误和重试，刷新失败保留旧记录并提示，只在查询成功且无记录时显示空态。网络和业务失败、恢复、刷新保留及成功空态均覆盖。新增提示补齐七种语言。 |

新增回归先在原实现上失败，再在修复后通过。独立只读复核确认概览权限补充修复后没有阻塞问题。

## 后端验证

Go 1.25.1，Windows amd64。以下命令通过：

```powershell
go test ./middleware ./router ./controller ./service ./setting/... ./model -count=1 -timeout 5m
go test ./common ./relay/channel/openai ./relay/channel/aws -count=1 -timeout 5m
go test ./relay/channel -count=1 -timeout 120s
go build ./...
```

概览补充修复后再次验证实际受影响范围并重新构建：

```powershell
go test ./controller ./middleware ./router ./service/authz -count=1 -timeout 5m
go build ./...
git diff --check
```

relaykit 独立构建及转换回归通过：

```powershell
cd relaykit
$env:GOWORK='off'
go build ./...
go test ./relayconvert/internal/oai_chat -count=1 -timeout 120s
```

首轮较宽范围运行中，已有两个 HTTP/2 GOAWAY 测试出现 Windows wsarecv 连接中止；定向重跑与随后整个 relay/channel 包重跑均通过，未修改这些测试。

## 前端验证

11 个文件、62 项测试通过；两个 TypeScript 项目及本轮四个 TS/TSX 文件的 lint、格式检查通过：

```powershell
cd web
node node_modules/vitest/vitest.mjs run src/features/degradation-check/__tests__ src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx src/features/system-settings/models/__tests__/probe-regex-input.test.tsx src/lib/__tests__/nav-modules.test.ts src/features/profile/components/__tests__/login-session-utils.test.ts
node node_modules/@typescript/native-preview/bin/tsgo --noEmit -p tsconfig.app.json
node node_modules/@typescript/native-preview/bin/tsgo --noEmit -p tsconfig.node.json
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/degradation-check/api.ts src/features/degradation-check/components/log-details-dialog.tsx src/features/degradation-check/__tests__/logs-api.test.ts src/features/degradation-check/__tests__/logs-state.test.tsx
node node_modules/oxfmt/bin/oxfmt --check src/features/degradation-check/api.ts src/features/degradation-check/components/log-details-dialog.tsx src/features/degradation-check/__tests__/logs-api.test.ts src/features/degradation-check/__tests__/logs-state.test.tsx
node scripts/sync-i18n.mjs
```

七个语言的同步报告均为零缺失，新增错误提示及复用文案均存在。与 HEAD 对比，每种语言的 5622 个原有翻译值保持不变；当前 142 个新增键中，本轮只新增刷新日志失败提示，其他键为此前改动。翻译通过技能要求的临时脚本写入，该脚本已删除。

生产构建使用 Bun 1.2.20 / Rsbuild 2.1.6 容器通过，耗时 2 分 28.9 秒；源码只读挂载，产物单独写入临时目录，未修改依赖、锁文件或工作区 dist：

```powershell
$degradationBuildOutput = 'C:\Users\yhxjs\AppData\Local\Temp\new-api-degradation-build-25c295e9273e4bd29ad583355f90905f'
docker run --rm --mount 'type=bind,source=D:\codespace\new-api,target=/repo,readonly' --mount "type=bind,source=$degradationBuildOutput,target=/repo/web/dist" -w /repo/web oven/bun:1.2.20 bun run build
```

本轮未修改数据库 schema、迁移、ORM 或数据库驱动。授权集成回归使用真实 SQLite；此前三种数据库的验证记录仍见 2026-10-05-degradation-check-fixes.md，本轮未重新运行数据库矩阵。

## 第二轮：推理配置、活跃统计与日志键盘操作

同日根据后续审查的“全部修复”授权，完成以下四项局部修复；上文验证记录属于第一轮，本节单独记录本次执行结果。

| 问题 | 修复与回归 |
| --- | --- |
| 新增指定模型目标默认强制关闭思考 | 新目标不再写入 `reasoning_effort: off`，绑定弹窗新增“使用模型默认设置”；未配置时省略推理字段，既有显式 off 保留，允许切回默认。测试覆盖 o3、Gemini Pro 新目标保存、空绑定继承与显式关闭恢复默认。 |
| 显式推理等级被模型后缀覆盖 | 探针构造时对已识别的 OpenAI 推理模型剥除冲突后缀；普通中转逻辑保持原有行为。真实 adaptor 回归覆盖 gpt-5.1-high + low、gpt-5.1-low + high、o3-high + medium，以及空配置继承和 custom-high 原名保留。 |
| 禁用渠道仍参与活跃统计 | 概览 enabled 同时要求渠道启用和模型绑定启用，禁用项不计入活跃数及全局统计，模型历史仍可见。覆盖手动禁用、自动禁用、模型暂停及渠道重新启用。 |
| 日志详情缺少键盘入口 | 日志摘要使用按钮，提供 aria-expanded 和 aria-controls；详情位于按钮外，可选择文字，点击不会收起。回归覆盖 Tab 聚焦、Enter 展开、Space 收起、焦点及诊断内容。 |

新增回归已先确认原实现失败，再确认修复后通过。独立只读复核对照本轮修改前快照，检查这四项及相关调用链，未发现阻塞项。

### 本轮验证

Go 1.25.1 / Windows amd64；宿主机没有 Bun，前端使用已安装依赖的 Node 入口。以下命令全部通过：

```powershell
go test ./controller -run '^TestDegradation(OpenAIExplicitReasoningOverridesModelSuffix|OverviewExcludesInactiveChannelsAndRetainsHistory)$' -count=1 -timeout 120s
go test ./controller ./middleware ./router ./service/authz -count=1 -timeout 5m
go build ./...
git diff --check

cd web
node node_modules/vitest/vitest.mjs run src/features/degradation-check/__tests__ src/features/system-settings/models/__tests__ src/lib/__tests__/nav-modules.test.ts --reporter=dot
node node_modules/@typescript/native-preview/bin/tsgo -b
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/degradation-binding-dialog.tsx src/features/degradation-check/components/log-details-dialog.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx src/features/system-settings/models/__tests__/degradation-binding-dialog.test.tsx src/features/degradation-check/__tests__/logs-state.test.tsx
node node_modules/oxfmt/bin/oxfmt --check src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/degradation-binding-dialog.tsx src/features/degradation-check/components/log-details-dialog.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx src/features/system-settings/models/__tests__/degradation-binding-dialog.test.tsx src/features/degradation-check/__tests__/logs-state.test.tsx
node scripts/sync-i18n.mjs
```

前端相关测试共 13 个文件、63 项通过。修正日志测试中的非空断言 lint 错误后，单独重跑该文件的 6 项测试、类型检查、lint 与该文件格式检查，全部通过。

“Use model default”通过临时 `add-missing-keys.mjs` 写入七种语言，随后执行同步脚本并删除临时脚本。同步报告各语言缺失、额外和未翻译计数均为 0；与本轮快照对比，每种语言仅新增该键，原有 5764 个翻译值全部保留。

本轮仅调整控制器逻辑、前端交互和回归测试；未改数据库查询、schema、迁移、ORM、驱动或 relaykit。本节未重新运行三数据库矩阵、relaykit 独立构建或前端生产构建；前一轮对应记录不作为本轮新验证结果。未调用真实上游模型服务。保留工作区既有改动，未执行 Git 写操作。
