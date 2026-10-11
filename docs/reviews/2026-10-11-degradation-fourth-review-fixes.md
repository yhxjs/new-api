# 降智检测第四轮审查修复

用户已授权修复本轮两项 P2 发现；保留现有工作区改动，不执行 Git 写操作。

## 设计与验证计划

1. 在 `controller/degradation_task_test.go` 增加全轮和单目标任务的渠道查询错误回归，验证错误导致任务状态为 failed；增加已删除渠道继续跳过的回归。先确认错误用例失败，再在 `controller/degradation_task.go` 区分 `gorm.ErrRecordNotFound` 与其他查询错误，后者携带渠道 ID 上报。
2. 在 `relay/channel/openai/adaptor_convert_test.go` 增加自定义 OpenAI 上游、官方地址、仿冒官方主机名和显式零值的转换回归。先确认自定义上游用例失败，再在 `relay/channel/openai/adaptor.go` 仅对默认或官方 OpenAI 主机及 Azure 去重上限；兼容上游保留两字段，已有推理模型参数迁移只填充缺失的 `max_completion_tokens`。
3. 执行定向回归、`go test ./controller ./router ./service ./relay/channel ./relay/channel/openai -count=1 -timeout 180s`、`go build ./...` 和格式/差异检查。生产改动限于控制器错误处理与请求字段转换，不修改数据库模型、SQL、迁移、驱动、relaykit 或前端。

## 验证记录

### 回归验证

在修改生产代码前执行：

```powershell
go test ./controller ./relay/channel/openai -run 'TestDegradationTaskChannelLookupFailure|TestDegradationTaskSkipsDeletedChannel|TestConvertOpenAIRequestMaxTokensExclusive' -count=1 -timeout 60s
```

全轮和单目标的查询错误用例均失败：任务实际状态为 succeeded，且错误信息为空。四个自定义上游转换用例均失败：`max_tokens` 意外为 nil。已删除渠道和既有官方上游场景保持通过。实施后执行相同命令，两包均通过；显式零值不会被旧字段覆盖。

### 测试与构建

```powershell
go test ./controller ./router ./service ./relay/channel ./relay/channel/openai -count=1 -timeout 180s
go build ./...
gofmt -l controller/degradation_task.go controller/degradation_task_test.go relay/channel/openai/adaptor.go relay/channel/openai/adaptor_convert_test.go
git diff --check
```

五个测试包及根模块构建均退出码 0；gofmt 没有待格式化文件，Git 差异检查没有空白错误。四份本轮源文件额外与修复前备份执行 `git diff --no-index --check`，没有空白错误诊断；退出码 1 表示文件存在差异。

独立只读 reviewer 对照修复前备份复核这四份文件及必要调用链，未发现 P0–P2 问题；确认手动强制探测停用渠道的原有语义保持不变。

### 验证边界

本轮没有修改数据库模型、SQL、迁移、ORM/驱动、relaykit 或前端。控制器任务状态回归使用真实 SQLite 和查询边界故障注入；未重跑真实 MySQL/PostgreSQL 矩阵，也未调用真实供应商接口。保留工作区原有改动，未执行 Git 写操作。
