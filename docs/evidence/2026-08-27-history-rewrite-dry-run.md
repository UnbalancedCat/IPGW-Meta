---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
evidence_type: security_rehearsal
result: pass_with_external_blocks
---

# Git 历史重写隔离演练

## EVID-HISTORY-DRYRUN-001：历史重写隔离演练

本记录只描述 2026-08-27 在 `build/safety/` 私有镜像中的脱敏演练。演练没有修改当前工作仓库的 refs，也没有访问或更新远端。

### 输入与工具

- 原始历史共 6 个提交；已知首个受影响提交为 `3862b21fed9ccf252249e31ad9b9ff2fe7049dc9`。
- 受控原始历史 bundle 的 SHA-256 为 `F99A596AE7D2D0B46A9B413002EFE017CDB63F673B77ED860938B5917B1F83A3`。
- 重写工具固定为 `git-filter-repo 2.47.0`，使用 `--sensitive-data-removal --replace-text` 和不含任何秘密值的模式规则。
- 扫描工具按 CI 固定为 gitleaks `v8.30.1`；合成 positive/negative canary 先通过。

### 脱敏结果

- 重写前全历史扫描得到 4 条脱敏命中：`test/parse/main.go` 的 `neu-cas-service-ticket`、`neu-ticket-query-material`，以及 `test/kick_v1/main.go` 的 2 条 `neu-campus-session-cookie`。未保存或复制 finding 内容。
- `git-filter-repo` 报告从 `3862b21` 起重写 4/6 个提交，并保留提交拓扑和消息。
- 工具会跳过非 commit 的 tree ref。镜像中两个本地 `refs/codex/turn-diffs/...` tree ref 仍可达旧 `test/`，演练因此显式移除这些仅本地执行快照，再执行 reflog expiry 和不可达对象清理。正式窗口必须重复该 refs 枚举，不能只检查 branches/tags。
- 清理后的全 refs/full-history/reflog gitleaks 扫描为 0 条命中。

### Tag 演练映射

| Tag | 重写前 | 隔离演练后 |
|---|---|---|
| `v0.1.0` | `be44b6ad0a56a0d776ac9ff1e023ddf9a85fd03c` | `be44b6ad0a56a0d776ac9ff1e023ddf9a85fd03c` |
| `v0.1.1` | `3862b21fed9ccf252249e31ad9b9ff2fe7049dc9` | `08e40b61d22d92096e329788170bde6b70fac525` |
| `v0.1.2` | `94e7334c6a744e2380be9dbc237cb2db77fefd6a` | `bebea92d3317b8ef02c0a8b07103c0fcb90905c0` |
| `v0.1.3` | `3721fd60dbdc8e6a35cab9569ec295b6fcc7bdea` | `7762747cf223f5c3d7214cfa2c18ce293d0215c9` |

`v0.1.0` 按计划保持不变；其余值只用于正式窗口复核，不能在真实输入 refs 不一致时盲目写入。

### 尚未完成

- 泄露会话尚未获得可审计的失效确认；安全审查不允许在缺少更明确授权时重新发送历史 Cookie。
- 当前工作树尚未执行真实 refs 重写；远端认证、强制更新、GitHub 缓存/PR 引用处理和协作者重新克隆均未完成。
- 本记录不能关闭 `SEC-HISTORY-001` 或 M0，也不能作为发布证据。
