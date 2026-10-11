# 巡检渠道匿名标签修复

用户已授权修复匿名标签可以枚举还原渠道编号的问题；保留已有工作区改动，不执行 Git 写操作。

## 方案

复用 `common.GenerateHMAC`，以已有的 `common.CryptoSecret` 为密钥，对 `degradation-node:<channelID>` 计算 HMAC-SHA256。公开标签为 `Node-` 加摘要前 12 位大写十六进制字符，避免公开算法在没有服务端密钥时可以从渠道编号直接推导标签，同时降低短标签碰撞概率。

不增加设置或数据库字段。密钥相同的节点对同一渠道生成相同标签；轮换密钥会更新标签。项目已有的 `CRYPTO_SECRET` 配置沿用现有初始化规则，未设置时回退至 `SESSION_SECRET`；要跨节点和重启保持标签一致，部署需要使用相同且固定的服务端密钥。

## 实施与验证计划

1. 在 `controller/degradation_test.go` 增加固定密钥、重复生成以及变更密钥的回归断言；更新匿名标签格式断言。执行 `go test ./controller -run '^TestMaskedNodeName' -count=1 -timeout 60s`，先确认旧实现不能满足密钥隔离要求。
2. 在 `controller/degradation.go` 用 HMAC 替换 FNV 与编号取模，保留公开接口的权限和字段脱敏行为。运行同一命令确认回归通过。
3. 运行 `go test ./controller ./router -count=1 -timeout 180s`、`go build ./...`、相关文件的 `gofmt -l` 与 `git diff --check`，并对本轮修复前备份进行只读差异复核。

本轮只修改匿名标签生成及其回归，不修改数据库模型、SQL、迁移、relaykit 或前端。

## 验证记录

修改生产代码前运行定向回归，两项测试均失败：原标签不满足新格式，且更换服务端密钥后仍返回 `Node-ODOC0`。替换 HMAC 后运行相同命令，回归通过；测试同时验证恢复同一密钥会恢复同一标签。

```powershell
go test ./controller -run '^TestMaskedNodeName' -count=1 -timeout 60s
go test ./controller ./router -count=1 -timeout 180s
go build ./...
gofmt -l controller/degradation.go controller/degradation_test.go
git diff --check
```

最终定向回归、controller/router 两包测试及根模块构建均退出码 0；gofmt 无待格式化文件，Git 差异检查没有空白错误。对照修复前备份，仅匿名标签函数、已失效的 FNV import 及相关测试发生源码变化。

独立只读 reviewer 对照两份源码备份，并检查 HMAC 工具函数、密钥初始化和权限脱敏调用链，未发现本轮改动引入的可行动 P0–P2 问题。

验证覆盖现有概览权限与脱敏回归。此次改动不涉及数据库行为，没有重跑真实三库矩阵；未调用供应商接口。
