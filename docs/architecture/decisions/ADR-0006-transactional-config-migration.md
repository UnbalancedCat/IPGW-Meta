---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0006
status: accepted
---

# ADR-0006：配置迁移采用显式凭据决策与可恢复事务

旧配置可能包含可逆编码或弱加密的密码；profiles、keyring、备份、marker 又是多个不能由单次文件替换共同提交的状态。仅按“备份、写凭据、写配置、写 marker”的顺序执行，会在进程崩溃或任一步失败时留下无法判断是否完成的部分迁移。

决定如下：

- 每个发现到的旧秘密首先进入 `pending`，不得因能够解码就选择落盘目标。TTY 用户必须逐 profile 决定导入全新 keyring 引用，或只配置 env/file/prompt 引用；非 TTY 只能通过显式 env/file 映射解决全部 pending 项。
- discovery、preview、参数校验和冲突解决保持零副作用。旧秘密只在当前进程内短暂存在；原始来源摘要也只用于进程内的 discovery/apply 一致性检查。
- apply 使用不含秘密的 write-ahead journal、随机 transaction ID、配置与 marker 的恢复快照以及单调 phase。新 keyring 引用在 journal 中预先登记且禁止覆盖已有项，因此失败时可以安全删除。
- marker 只保存脱敏来源 stamp、无秘密目标配置摘要、目标 schema、工具版本、完成时间和 transaction ID。marker、目标配置与当前来源必须共同验证，单独存在的 marker 不代表迁移成功。
- marker 验证完成前的崩溃执行确定性回滚；marker 已验证而仅剩清理未完成时继续清理。无法安全恢复时保留 journal 和恢复材料并停止，不猜测完成状态。

代价是迁移实现需要显式状态机、失败注入测试和 keyring/file-system 抽象；收益是不会自动复制旧秘密，重跑不会重复导入，且每个部分失败都具备可审计的恢复路径。具体命令、权限和验收契约由 `MIG-CREDENTIAL-001`、`MIG-TRANSACTION-001` 与 `MIG-IDEMPOTENT-001` 定义。
