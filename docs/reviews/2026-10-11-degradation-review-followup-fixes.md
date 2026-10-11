# 降智检查第二轮审查修复记录

日期：2026-10-11。根据用户“全部修复”授权，修复以下三项发现，并保留工作区既有改动。

## 修复与回归

| 问题 | 最终行为与验证 |
| --- | --- |
| Codex / Responses-only 模型巡检使用 Chat Completions | 在模型映射和推理参数应用后选择 Responses，复用现有请求转换器与 JSON / 缓冲 SSE 响应处理器。Codex 使用流式请求；保留提示词、推理参数、实际请求计费快照和消费日志流式标记。默认继承推理时清除 GPT-5 / O 系列不支持的 temperature。真实本地 HTTP 服务覆盖 Codex、OpenAI、映射别名、默认继承、上游失败和取消。 |
| 已删除的探针 ID 导致全选误判 | 对显式绑定过滤失效 ID，全选按当前探针成员判断。仅含失效 ID 时阻止保存，重新勾选一个探针后保留显式子集。新增两项真实交互回归。 |
| 倒计时依赖进程内时间戳，重启后不准确 | 复用调度器的持久化任务查询，以整轮任务的完成时间计算倒计时；无历史、过期和 pending / running 返回 0，单目标手动任务不推迟整轮。查询失败标记 AggregationError。仅在完成状态成功落库后刷新概览缓存。回归覆盖八个调度情景、查询失败和缓存刷新顺序。 |

关联协议修复：Responses 原生 `usage.output_tokens_details.reasoning_tokens` 映射到既有 token 明细，兼容旧 `completion_tokens_details`，原生显式零优先。解码方法仅加在 Responses 响应 DTO 上，避免匿名嵌入 Usage 的其他响应受到方法提升影响。

独立只读审查发现缓冲 SSE 处理器漏掉官方顶层 `type: "error"`。新增“匹配文本 ok → 顶层 error → EOF”回归，确认修复前错误地记为 Passed，再增加错误分支并保留上游 message / code / param。修复后测试通过，复核无待修问题。

协议依据：[OpenAI Reasoning 文档](https://developers.openai.com/api/docs/guides/reasoning)、[官方 Python SDK usage 定义](https://github.com/openai/openai-python/blob/main/src/openai/types/responses/response_usage.py)、[Responses streaming Error 事件](https://developers.openai.com/api/reference/resources/responses/streaming-events/)。

## 后端验证

环境：Go 1.25.1，Windows amd64。以下命令均退出 0：

```powershell
$env:GOMAXPROCS = '2'
go test -p 1 ./controller ./model ./service ./router ./relay/... ./common ./pkg/billingexpr -count=1 -timeout 5m
go build -p 1 ./...
```

补充顶层 SSE 错误修复后，重跑受影响包完整测试并通过：

```powershell
go test -p 1 ./controller ./relay/channel/openai -count=1 -timeout 5m
```

relaykit 在自身目录关闭 workspace，完整测试和独立构建均退出 0：

```powershell
cd relaykit
$env:GOWORK = 'off'
$env:GOMAXPROCS = '2'
go test -p 1 ./... -count=1 -timeout 5m
go build -p 1 ./...
```

本轮涉及的 Go 文件通过 gofmt 检查；git diff --check 通过。

## 三数据库验证

倒计时与调度查询使用真实数据库验证，结果全部通过。本轮没有修改生产 schema、迁移、数据库驱动或查询实现。

| 数据库 | 实际版本 | 结果 |
| --- | --- | --- |
| SQLite | 3.50.4 | 八个概览调度情景及 scheduled query 通过 |
| MySQL | 8.0.40 | 八个概览调度情景及 scheduled query 通过 |
| PostgreSQL | 17.5 | 八个概览调度情景及 scheduled query 通过 |

每种数据库分别设置以下环境变量，并在根模块执行相同测试命令：

```powershell
# SQLite
$env:DEGRADATION_DIALECT_DB = 'sqlite'
$env:DEGRADATION_DIALECT_DSN = ':memory:'

# MySQL：本轮隔离实例，端口 33991
$env:DEGRADATION_DIALECT_DB = 'mysql'
$env:DEGRADATION_DIALECT_DSN = 'root@tcp(127.0.0.1:33991)/review_degradation?charset=utf8mb4&parseTime=true&loc=UTC'

# PostgreSQL：本轮隔离实例，端口 55991
$env:DEGRADATION_DIALECT_DB = 'postgres'
$env:DEGRADATION_DIALECT_DSN = 'host=127.0.0.1 port=55991 user=review_admin dbname=review_degradation sslmode=disable'

# 对每组环境变量单独执行
$env:GOMAXPROCS = '2'
go test -p 1 ./controller -run '^TestDegradationOverviewUsesPersistedSchedule$' -count=1 -timeout 90s -v
go test -p 1 ./model -run '^TestSystemTaskScheduledQueryDialectVerification$' -count=1 -timeout 90s -v
```

MySQL 和 PostgreSQL 均使用本轮新建、只绑定 127.0.0.1 的临时实例，验证后已正常关闭。

## 前端验证

宿主未安装 Bun，使用已有依赖的 Node CLI 入口完成验证；16 个测试文件、91 项测试通过。类型检查、两个改动文件的 oxlint / oxfmt 检查均通过。

Rsbuild 生产构建退出 0，耗时 21 秒；产物写入临时目录，构建前后 routeTree.gen.ts 的 SHA256 相同：

```powershell
cd web
$env:NODE_PATH = 'C:\Users\yhxjs\AppData\Local\Temp\newapi-native-deps-20261010\node_modules'
node node_modules/@rsbuild/core/bin/rsbuild.js build --env-mode production --dist-path 'C:\Users\yhxjs\AppData\Local\Temp\new-api-degradation-followup-build-08c708d28b2e4ad8aadb25fe9a6863be'
```

## 验证范围

未调用真实付费模型接口；Responses 协议通过本地 HTTP 服务和官方协议字段验证。无 Git 写操作，无依赖或锁文件修改，无新增 UI 文案或翻译修改。
