# 降智检测第五轮审查修复

用户已授权全部修复本轮三项发现，不执行 Git 写操作。

## 设计与实施计划

1. 在 `controller/degradation_reasoning_test.go` 增加真实 DeepSeek/Ollama adaptor 回归，断言序列化后的原生字段：DeepSeek low/medium/high 的 `max_tokens` 分别为 1280/2304/4352；显式 high/off 覆盖 DeepSeek `-none`/`-max` 后缀；nil/空绑定保留后缀继承；Ollama off 发送 `think:false`、`options.num_predict:256`。保留 custom 和未知供应商 off 的拒绝行为。先运行测试确认失败。
2. 仅调整 `controller/degradation_executor.go`：非空显式绑定用现有解析器去掉 DeepSeek 思考后缀；DeepSeek 预设使用 `max_tokens`；DeepSeek/Ollama off 使用 `reasoning_effort:none` 并保持原生输出上限。保留原始计费模型身份和现有 OpenAI 行为。运行同一回归确认通过。
3. 运行 `go test ./controller ./relay/channel/deepseek ./relay/channel/ollama ./relay/channel/openai -count=1 -timeout 180s`、`go build ./...`、gofmt 和差异检查，并对照修复前临时备份复核改动。本轮不改数据库、relaykit 或前端，不重复这些模块的验证矩阵。

## 验证记录

### 失败回归与修复后验证

修改生产代码前运行：

```powershell
go test ./controller -run 'TestDegradation(DeepSeek|Ollama|NativeProviders)' -count=1 -timeout 60s
```

退出码 1：DeepSeek 三个预设的原生 `max_tokens` 缺失，两个后缀覆盖显式 high，三个 DeepSeek off 用例及 Ollama off 返回不支持。nil/空绑定的后缀继承和 custom 拒绝用例通过。

修复后运行：

```powershell
go test ./controller -run 'TestDegradation(DeepSeek|Ollama|NativeProviders|UnknownProvider)' -count=1 -timeout 60s
go test ./controller ./relay/channel/deepseek ./relay/channel/ollama ./relay/channel/openai -count=1 -timeout 180s
go build ./...
gofmt -l controller/degradation_executor.go controller/degradation_reasoning_test.go
git diff --check
```

定向回归、controller/Ollama/OpenAI 完整包测试及根模块构建均退出码 0；DeepSeek 包没有自身测试，本轮在 controller 中调用真实 DeepSeek adaptor 验证序列化协议。gofmt 没有待格式化文件，Git 差异检查没有空白错误。

两份源码与修复前临时备份逐行比对：生产改动仅限显式 DeepSeek 后缀处理、DeepSeek 原生上限、DeepSeek/Ollama off 映射；测试新增 16 个确定性场景。额外执行 `git diff --no-index --check` 无空白错误诊断，退出码 1 仅表示文件存在差异。原模型计费身份、空绑定继承、OpenAI 上限映射及不支持供应商的拒绝行为保留。

### 验证边界

未调用真实供应商服务。本轮没有修改数据库、relaykit 或前端，未重跑这些模块的验证矩阵。上一轮审查曾发现独立 `relay/channel` HTTP/2 测试在 Windows 因连接被中止而失败；本轮未修改或重跑该测试，不宣称整个仓库测试全部通过。保留既有工作区改动，未执行 Git 写操作。
