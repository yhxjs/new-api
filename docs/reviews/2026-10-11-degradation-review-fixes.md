# 降智检查审查修复记录

日期：2026-10-10 至 2026-10-11。根据“全部修复”授权，完成本轮三项审查问题；生产代码和回归测试共涉及 13 个文件，保留工作区既有改动，未执行 Git 写操作。

## 修复与回归

| 问题 | 最终行为与验证 |
| --- | --- |
| 探针超时后仍阻塞整轮运行 | Coze 检索、详情读取和轮询等待继承渠道测试上下文；讯飞 WebSocket 握手、读取及数据投递可取消，取消后关闭连接并释放等待者。仅渠道测试与探针继承客户端取消，普通中转保持既有完成及结算行为。回归覆盖 HTTP 响应头/响应体阻塞、轮询间取消、WebSocket 握手/读取/无人消费、正常完成及普通客户端取消后仍完成。轮询等待测试在响应消费后同步取消；临时恢复 Sleep 已验证失败。 |
| Claude 新型号收到不支持的推理参数 | Opus 4.7/4.8 预设映射到 adaptive thinking 和 output_config.effort；移除不支持的采样参数，显式探针配置覆盖冲突模型后缀，custom token budget 在本地明确拒绝。保留 AWS 前缀、旧型号预算及关闭优先级；OpenRouter 使用 reasoning 对象。测试检查真实 adaptor 转换后的 JSON 和独立转换器行为。 |
| 逐项保存刷新覆盖未保存草稿 | dirty 或提交中的表单忽略服务器快照，失败保留全部草稿以便重试。完成所有写入后等待新读取，以提交值重建基线并保留保存期间的新编辑；Saving 覆盖完整提交过程。system-options 读取由 React Query 去重，绕过 HTTP 层旧 GET Promise 复用。真实 Axios adapter 回归保留生产 api.get 与拦截器，确认旧部分读取不能阻塞最终刷新或覆盖成功后的表单。 |

新增回归均已在修复前确认失败，并在修复后通过。独立只读复核检查本轮 13 个文件及相关取消、结算、推理转换和查询调用链，最终没有待修阻塞项。

## 后端验证

环境：Go 1.25.1，Windows amd64；设置 GOMAXPROCS=2，并以 -p 1 串行编译，避免 Windows 多个链接进程同时占用内存。最终受影响包完整测试及根模块构建均退出 0：

```powershell
$env:GOMAXPROCS = '2'
go test -p 1 ./controller ./relay/channel/coze ./relay/channel/xunfei ./relay/channel/aws ./relay/channel/openai ./relay/channel ./service ./router -count=1 -timeout 5m
go build -p 1 ./...
```

relaykit 在自身目录内关闭 workspace 后，完整转换器测试与独立构建均退出 0：

```powershell
cd relaykit
$env:GOWORK = 'off'
$env:GOMAXPROCS = '2'
go test -p 1 ./relayconvert/internal/oai_chat -count=1 -timeout 60s
go build -p 1 ./...
```

涉及的 10 个 Go 文件通过 gofmt 检查；git diff --check 通过。

## 前端验证

宿主未安装 Bun，使用已有依赖的 Node 23.1.0 CLI 入口。最终 15 个文件、82 项测试通过；类型检查和本轮 3 个 TS/TSX 文件的 lint、格式检查均退出 0：

```powershell
cd web
node node_modules/vitest/vitest.mjs run src/features/degradation-check/__tests__ src/features/system-settings/models/__tests__ src/features/system-settings/maintenance/__tests__ src/lib/__tests__/nav-modules.test.ts --reporter=dot
node node_modules/@typescript/native-preview/bin/tsgo -b --noEmit
node node_modules/oxlint/bin/oxlint -c .oxlintrc.json src/features/system-settings/api.ts src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx
node node_modules/oxfmt/bin/oxfmt -c .oxfmtrc.json --check src/features/system-settings/api.ts src/features/system-settings/models/degradation-check-section.tsx src/features/system-settings/models/__tests__/degradation-settings-persistence.test.tsx
```

Rsbuild 2.1.6 生产构建通过，最终构建耗时 24.1 秒，产物写入临时目录，routeTree.gen.ts 构建前后的 SHA256 相同：

```powershell
$env:NODE_PATH = 'C:\Users\yhxjs\AppData\Local\Temp\newapi-native-deps-20261010\node_modules'
node node_modules/@rsbuild/core/bin/rsbuild.js build --env-mode production --dist-path 'C:\Users\yhxjs\AppData\Local\Temp\newapi-degradation-fixes-final-build-20261011'
```

宿主依赖缺少 Rspack/Lightning CSS 的 Windows 原生包，曾导致 CLI 启动和 CSS 构建失败。按错误指向检索并读取 [npm/cli #4828](https://github.com/npm/cli/issues/4828)，其中记录了平台可选依赖缺失的问题；该历史 issue 不用于断言本机的安装原因。实际验证方案来自本地原生模块加载入口：从官方 npm registry 获取准确匹配的 @rspack/binding-win32-x64-msvc 2.1.4、lightningcss-win32-x64-msvc 1.32.0、@tailwindcss/oxide-win32-x64-msvc 4.3.3，仅在临时目录提供给 NODE_PATH，并核对包名与版本。本轮没有安装或升级工作区依赖、修改锁文件；临时包未写入项目 node_modules，构建产物未写入已有 dist。

完整构建日志：C:\Users\yhxjs\AppData\Local\Temp\newapi-native-deps-20261010\final-build-20261011.log。

本轮未调用真实付费模型接口，协议验证止于 adaptor 构造和转换结果；取消行为使用真实本地 HTTP/WebSocket 服务验证。本轮未修改数据库行为或翻译文件，未重新执行三数据库矩阵或翻译同步；此前验证记录不作为本轮的新验证结果。
