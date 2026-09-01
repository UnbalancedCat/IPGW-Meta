---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0011
status: accepted
---

# ADR-0011：M0 残余治理不阻塞发布流水线

M0 已完成真实 fixture 移除、历史重写、全部可达 refs/tag 复扫、远端原子更新、fresh clone、严格 main/tag ruleset、泄漏测试和自更新入口禁用。当前未完成项是 GitHub 对三个已失去 ref 可达性的旧 commit 仍可按对象 ID 直接访问，以及其他既有副本的重新克隆；这些事项由 [`SEC-HISTORY-001`](../security.md#sec-history-001历史泄露响应) 持续跟踪。维护者接受该残余治理项继续存在，并决定它不再作为 Candidate、Promotion、release 或真实验收的状态门禁。

决定如下：

- M0 与 `SEC-HISTORY-001` 继续如实保持 `in_progress`，直到 GitHub Support 缓存/悬空对象处理和既有副本处置完成；不得为了放行而把未完成事项标记为 `complete`，也不得删除历史记录。
- Candidate gate 只要求 M1、M2 各自恰好出现一次且为 `complete`；M0 的缺失、状态或完成度不参与该 gate 的通过判定。
- Promotion gate 只要求 M1、M2 各自恰好出现一次且为 `complete`，并要求 M3 恰好出现一次且保持 `in_progress`，直到公开发布验证完成；M0 不参与判定。
- Release gate 只要求 M1、M2、M3 各自恰好出现一次且为 `complete`；M0 不参与判定。
- `docs/upgrade/status.md` 仍必须通过 doccheck 并保留规范的 M0 行。M0 非阻塞不等于允许无效状态文档，也不改变维护者对 GitHub Support 外部事项的跟踪责任。
- required CI、完整历史与工作树 secret scan、日志/JSON/Observer 泄漏测试、source/main/tag 签名、refs/ruleset、Candidate identity、build-input、artifact digest、attestation、真实网络 evidence、promotion lock 和逐资产复核保持独立 hard gate。任一失败仍 fail closed，不能用本 ADR 绕过。
- 本决定不授权创建 Candidate、tag、draft、release，不授权校园网、凭据、login/logout 或网络修改；这些动作仍分别受 [`REL-APPROVAL-001`](../../operations/release.md#rel-approval-001高风险动作逐项批准)、Candidate 和 live-gate 规范约束。

实现必须以单一可测试 gate helper 为 Makefile 的 Candidate、Promotion 与 release 入口提供状态判定，并用合成状态文件覆盖 M0 的 `not_started|in_progress|blocked|complete` 与缺失情况，证明它们不改变结果；同时覆盖 M1/M2/M3 缺失、重复和错误状态，证明其余门禁仍封闭失败。

代价是 v1 可能在 GitHub Support 尚未清理不可达旧对象时继续 Candidate 与发布流程；收益是外部对象缓存处置不再无限期冻结已经独立通过 secret scan、签名、CI、Candidate、真实 evidence 和 promotion 校验的工作。该风险接受只改变调度依赖，不改变秘密材料的处置义务或任何安全验证结果。
