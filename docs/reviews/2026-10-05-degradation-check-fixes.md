# 降智检查审查修复与验证记录

验证日期：2026-10-05。修复范围为此前审查的 9 项问题，保留工作区其他已有修改，未执行任何 Git 写操作。

## 修复结果

| 问题 | 修复与回归验证 |
| --- | --- |
| 导航配置解析遗漏 | 解析 `degradationCheck` 对象和旧布尔/数字/字符串配置，并强制登录；导航测试覆盖开启与缺省配置。 |
| 探针超时未取消 HTTP 请求 | 将渠道测试的 context 传给实际 HTTP 请求，覆盖响应读取过程的取消；探针响应读取超时归类为 timeout。 |
| 空探针返回 null | 概览 channels、models、probes 返回数组；真实 SQLite + handler 测试覆盖空目标、空模型、空探针和已删除的绑定探针，页面测试验证聚合时间线。 |
| 配置深复制竞争 | 深复制在共享配置读锁内完成；测试验证更新前快照独立、调用方修改不污染配置，以及并发读取的一致性。 |
| Claude preset 参数非法 | 设置有效的思考预算、额外可见输出预算和温度；通过实际 Claude adaptor 验证 low/medium/high 请求。 |
| custom 预算被忽略 | Claude/OpenRouter 使用 reasoning.max_tokens，Gemini 使用实际 thinking_config；按供应商限制拒绝无效或不支持的自定义预算；验证实际 adaptor 转换结果。 |
| 执行次数响应复制遗漏 | 保留模型 total_executions；handler 与页面测试验证不同执行频率下的加权结果及筛选。 |
| 模型名截断破坏关联 | 模型名使用 TEXT 完整保存；删除旧模型名复合索引，保留渠道、时间、状态索引；覆盖完整名称分页、概览关联及新旧表迁移。 |
| 旧翻译被覆盖 | 经规定脚本恢复 32 处已有翻译，执行 i18n sync；七种语言既有键值与 HEAD 一致，每种语言保留本功能新增的 137 个键。 |

## 后端验证

以下最终测试全部通过，覆盖 7 个受影响包：

```powershell
go test ./controller ./model ./setting/degradation_setting ./relay/channel ./relay/channel/openai ./relay/channel/claude ./relay/channel/gemini -count=1 -timeout 180s
```

配置并发验证在 Linux Go 1.25.1 容器中通过，源码只读挂载；Windows 当前 CGO_ENABLED=0：

```powershell
docker run --rm --name newapi-degradation-review-race --mount 'type=bind,source=D:\codespace\new-api,target=/workspace,readonly' --mount 'type=bind,source=C:\Users\yhxjs\go\pkg\mod,target=/go/pkg/mod' --workdir /workspace --env GOTOOLCHAIN=local golang:1.25.1 go test -race ./setting/degradation_setting -count=1
```

relaykit 独立构建通过：

```powershell
cd D:\codespace\new-api\relaykit
$env:GOWORK='off'
go build ./...
```

已有 HTTP/2 GOAWAY 测试曾出现一次 Windows 套接字中止异常；该测试直接使用测试 HTTP client，不经过本次修改的 DoApiRequest。单独重跑、包级重跑及最终完整包测试均通过，未修改该测试。

## 前端验证

3 个文件、21 项测试通过；应用与构建工具两个 TypeScript 项目、涉及文件的 lint 和格式检查均通过：

```powershell
cd D:\codespace\new-api\web
node node_modules/vitest/vitest.mjs run src/features/degradation-check/__tests__/degradation-check-page.test.tsx src/lib/__tests__/nav-modules.test.ts src/features/profile/components/__tests__/login-session-utils.test.ts
node node_modules/@typescript/native-preview/bin/tsgo --noEmit -p tsconfig.app.json
node node_modules/@typescript/native-preview/bin/tsgo --noEmit -p tsconfig.node.json
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/lib/nav-modules.ts src/lib/__tests__/nav-modules.test.ts src/features/degradation-check/__tests__/degradation-check-page.test.tsx
node node_modules/oxfmt/bin/oxfmt --check src/lib/nav-modules.ts src/lib/__tests__/nav-modules.test.ts src/features/degradation-check/__tests__/degradation-check-page.test.tsx
node scripts/sync-i18n.mjs
```

生产构建通过，Rsbuild 2.1.6 / Bun 1.2.20，耗时 39.6 秒。宿主机缺少 Windows Rspack native binding，使用现有 Linux 依赖在 Bun 容器中完成构建，未修改依赖或锁文件：

```powershell
$buildDir = Join-Path $env:TEMP 'new-api-degradation-review-20261005/frontend-build'
New-Item -ItemType Directory -Path $buildDir -Force | Out-Null
docker run --rm --name newapi-degradation-review-frontend --mount 'type=bind,source=D:\codespace\new-api,target=/workspace,readonly' --mount "type=bind,source=$buildDir,target=/workspace/web/dist" --workdir /workspace/web oven/bun:1.2.20 bun run build
```

## 真实数据库验证

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 完整模型名读写、聚合、分页、清理，新表和旧窄表重复迁移，发布版数据库升级通过。 |
| MySQL | 5.7.44 | 同上；数据库使用 utf8mb4 / utf8mb4_unicode_ci。 |
| PostgreSQL | 9.6.24 | 同上；超出 btree 索引条目常见长度限制的模型名也能完整保存与查询。 |

使用隔离 MySQL/PostgreSQL 容器及临时 SQLite 文件。以最新发布版官方镜像 `calciumion/new-api:v1.0.0-rc.41` 实际启动创建三种数据库，镜像摘要为 `sha256:3293fc3d13bbf243ae720d9c4e0b8049e8ed01f5c130d3bdcdad8a1a2c7e57ed`。随后停止发布版应用，在真实发布版 users/options 表中初始化代表性数据，并执行当前巡检日志迁移。

测试同时覆盖无巡检表的发布版数据库与此前未发布的 varchar(128) 巡检表；每种情况连续运行生产使用的巡检迁移两次。验证原表仍存在、原用户/配置及巡检数据保留、用户名和日志主键唯一约束保留、渠道/时间/状态索引存在、长模型名能够完整过滤和关联。巡检日志存储在主数据库 DB，独立消费日志数据库 LOG_DB 的 schema 迁移路径未改变。

数据库测试仅可指向新建的隔离测试库：迁移测试会重建 degradation_check_logs 表。

MySQL：

```powershell
$env:DEGRADATION_DIALECT_DB='mysql'
$env:DEGRADATION_DIALECT_DSN='root:degradation-test@tcp(127.0.0.1:13316)/degradation_review?charset=utf8mb4&parseTime=true&loc=Local'
$env:DEGRADATION_RELEASE_BASELINE='1'
go test ./model -run '^TestDegradationCheckLog(Migration|DialectVerification)$' -count=1 -v
```

PostgreSQL：

```powershell
$env:DEGRADATION_DIALECT_DB='postgres'
$env:DEGRADATION_DIALECT_DSN='host=127.0.0.1 port=15436 user=postgres password=degradation-test dbname=degradation_review sslmode=disable'
$env:DEGRADATION_RELEASE_BASELINE='1'
go test ./model -run '^TestDegradationCheckLog(Migration|DialectVerification)$' -count=1 -v
```

SQLite：

```powershell
$env:DEGRADATION_DIALECT_DB='sqlite'
$env:DEGRADATION_DIALECT_DSN=Join-Path $env:TEMP 'new-api-degradation-review-20261005/new-api.db'
$env:DEGRADATION_RELEASE_BASELINE='1'
go test ./model -run '^TestDegradationCheckLog(Migration|DialectVerification)$' -count=1 -v
```

## 范围与限制

- 供应商请求验证使用项目实际 adaptor/converter，没有发送付费的真实供应商请求。
- Gemini 3 Pro 的 medium 限制仅应用于 `gemini-3-pro`，不影响支持 medium 的 3.1 Pro；已补充实际转换回归用例，并对照 [Google 官方思考文档](https://ai.google.dev/gemini-api/docs/thinking) 检查。
- 已被旧逻辑截断的历史模型名无法从现有日志还原；迁移保留现有数据，后续完整模型名不再截断。
- 数据库验证覆盖本次巡检日志迁移，不等同于整仓库所有未提交变更的升级验证。

## 本轮审查问题修复

本轮补齐了六项审查问题：

- AWS Bedrock SDK 的请求准备阶段允许 `(nil, nil)` 响应，继续交给 `DoResponse` 执行 `InvokeModel`；HTTP API-key 路径仍要求真实 HTTP 响应，并新增 SDK 传输回归测试。
- 手动单目标任务不会再被任意全轮任务吞掉。全轮任务只有在目标存在、渠道启用、模型未暂停且至少有一个有效探针时才算覆盖该目标。
- 调度器按空 payload 的自动任务记录计算间隔；已完成的单目标手动任务不会推迟下一次自动全轮巡检，活动手动任务仍参与去重。
- 概览按执行次数区分无数据模型，健康徽章、通过率、探针时间线和状态筛选不再把零执行模型当作 100% Healthy。
- 正则探针改为逐行编辑和保存，逗号保留在表达式中；同时修复 RHF 嵌套路径导致新探针、标量和模型绑定提交旧值的问题。
- 概览接口遇到业务失败会进入错误态；首次失败显示重试提示，刷新失败保留上次成功结果并标记数据可能过期。

新增回归验证：前端相关 6 个测试文件共 32 项通过；应用和 Node TypeScript 检查、目标 oxlint 与格式检查通过。后端 controller、model、AWS adaptor 和 degradation setting 相关测试通过；服务包全量测试中的既有 `channel_affinity_usage_cache` 测试受测试间共享状态影响而失败，单独运行该测试通过，未修改其代码。

自动调度查询使用 SQLite 3.50.4、MySQL 5.7.44 和 PostgreSQL 9.6.24 的真实实例分别验证通过；测试容器已清理。全程未执行 Git 写操作。

调度查询验证命令为：

```powershell
$env:DEGRADATION_DIALECT_DB='sqlite'; $env:DEGRADATION_DIALECT_DSN=':memory:'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
$env:DEGRADATION_DIALECT_DB='mysql'; $env:DEGRADATION_DIALECT_DSN='root:degradation-test@tcp(127.0.0.1:13316)/degradation_review?charset=utf8mb4&parseTime=true&loc=Local'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
$env:DEGRADATION_DIALECT_DB='postgres'; $env:DEGRADATION_DIALECT_DSN='host=127.0.0.1 port=15436 user=postgres password=degradation-test dbname=degradation_review sslmode=disable'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
```
- 本次创建的测试容器和网络已清理；未执行提交、暂存、推送、合并、切换分支等 Git 写操作。

## 后续七项审查问题修复与最终验证

本次继续完成七项修复，保持现有工作区变更，没有执行 Git 写操作：

1. 调度器只为 degradation_check 使用自动全轮任务查询；channel_test 和 model_update 仍按最近一次任务完成时间计算间隔，避免手动任务完成后马上重复调度。
2. 巡检日志批量落库或过期清理失败会返回任务错误，不再上报成功、推进全轮完成时间或刷新概览缓存。
3. 渠道测试拒绝负数和超过单请求额度边界的 usage，覆盖嵌套缓存、输入和输出明细；计费转换捕获额度饱和并写入管理员审计。额外修复饱和原值为 Inf/NaN 时无法序列化 JSON 的问题：审计原值以文本保留，有限数值保持数字。
4. 巡检路由先要求登录；管理员和超级管理员在导航被隐藏时仍可直接访问，普通用户继续遵守导航开关。
5. 保存时限制探针、关键词、排除词、提示词、目标、模型和绑定的数量或长度；执行前限制每轮最多 10000 项任务，覆盖“全部模型”从渠道配置展开的路径。
6. 移除正则表达式的全局无界缓存，保留有界输入窗口和 RE2 匹配行为。
7. “全部模型”目标展示渠道当前模型，允许暂停、即时测试和配置探针/推理；保存绑定保持动态模型选择，不固化模型列表，并覆盖加载、失败重试和空状态。

### 后端

以下五个包的全量测试通过：

```powershell
go test ./common ./controller ./service ./model ./setting/degradation_setting -count=1 -timeout 5m
```

新增回归覆盖任务落库失败、清理失败、成功状态、时间戳与缓存更新、实际本地 HTTP 探针、展开任务数超限、usage 边界、消费日志饱和审计及非有限原值的 JSON 序列化。配置测试同时验证集合/字符串最大合法大小和超限拒绝。

既有 channel_affinity_usage_cache 测试在 Windows 上使用时间戳生成键，可能因时钟分辨率产生碰撞。本次将夹具改为测试专属键，并在前后清理对应统计缓存；没有修改生产统计逻辑。完整 service 包及以下重复运行均通过：

```powershell
go test ./service -run '^TestObserveChannelAffinityUsageCacheByRelayFormat' -count=2
```

### 前端

实际执行 Vitest 的 11 个相关测试文件、54 项测试全部通过；应用与构建工具 TypeScript 项目、涉及文件 oxlint、oxfmt 和 git diff --check 通过。宿主机缺少 Bun，因此使用已安装工具的 Node 入口执行检查：

```powershell
cd D:\codespace\new-api\web
node node_modules/vitest/vitest.mjs run src/features/degradation-check/__tests__ src/features/system-settings/models/__tests__ src/lib/__tests__/nav-modules.test.ts src/features/model-status/__tests__/route-access.test.ts
node node_modules/@typescript/native-preview/bin/tsgo -b
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/routes/degradation-check.tsx src/features/degradation-check src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/degradation-binding-dialog.tsx src/features/system-settings/models/__tests__ src/lib/nav-modules.ts src/lib/__tests__/nav-modules.test.ts
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/usage-logs/types.ts
node node_modules/oxfmt/bin/oxfmt --check src/routes/degradation-check.tsx src/features/degradation-check/__tests__/route-access.test.ts src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx src/features/usage-logs/types.ts
git diff --check
```

生产构建在 Bun 1.2.20 / Rsbuild 2.1.6 容器中通过，耗时 40.5 秒。源码只读挂载，产物写入临时目录，没有修改依赖或锁文件；最终新增的日志类型声明另经 TypeScript 验证，运行时代码未变。

```powershell
$reviewBuildDir = Join-Path $env:TEMP 'new-api-degradation-review-20261005/fixes-frontend-build'
New-Item -ItemType Directory -Path $reviewBuildDir -Force | Out-Null
docker run --rm --name newapi-review-fixes-frontend --mount 'type=bind,source=D:\codespace\new-api,target=/workspace,readonly' --mount "type=bind,source=$reviewBuildDir,target=/workspace/web/dist" --workdir /workspace/web oven/bun:1.2.20 bun run build
```

### 真实数据库

调度查询在 SQLite 3.50.4、MySQL 5.7.44、PostgreSQL 9.6.24 的真实隔离实例均通过。验证巡检已完成手动任务不推迟自动巡检、活动手动任务参与去重，以及普通任务查询保留手动完成记录。本轮没有修改 schema 或迁移；此前迁移验证结果见上文。

```powershell
cd D:\codespace\new-api
$env:DEGRADATION_DIALECT_DB='sqlite'; $env:DEGRADATION_DIALECT_DSN=':memory:'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
$env:DEGRADATION_DIALECT_DB='mysql'; $env:DEGRADATION_DIALECT_DSN='root:degradation-test@tcp(127.0.0.1:13317)/degradation_review?charset=utf8mb4&parseTime=true&loc=Local'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
$env:DEGRADATION_DIALECT_DB='postgres'; $env:DEGRADATION_DIALECT_DSN='host=127.0.0.1 port=15437 user=postgres password=degradation-test dbname=degradation_review sslmode=disable'; go test ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -v
```

本轮新建的 MySQL/PostgreSQL 验证容器已清理。前端构建产物保留在临时目录；供应商探针回归使用本地测试上游，不要求付费供应商请求。
