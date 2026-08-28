---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0007
status: accepted
---

# ADR-0007：一次构建的不可变候选原样晋升

旧发布流程会在 tag 阶段重新构建或重新打包，因此无法证明校园网验收的字节与最终公开资产完全相同。单独保存文件哈希也不足以绑定来源提交、构建输入、GitHub artifact 和人工验收证据。

决定如下：

- 候选只从受保护 `main` 的精确完整 SHA 构建一次，生成包含公开 `release_assets` 与私有 `test_tools` 的单一 candidate-set；两个集合在 manifest 中分开列出。
- manifest 绑定计划修订、candidate ID、source commit/tree、工具链、构建输入摘要以及每个成员的名称、平台、大小和 SHA-256。
- candidate-set 作为不可变 GitHub artifact 上传，记录 artifact ID、artifact digest、workflow run ID/attempt，并对集合和每个公开资产生成 provenance attestation。
- 真实网络验收只能执行该候选中的冻结产品字节；artifact 不可用、hash/attestation 不符或构建输入改变时，候选立即失效并重新构建、重新验收。
- 验收通过后提交 promotion lock。candidate source 与最终 tag 之间只允许 evidence、正式状态、认证能力和 release notes 白名单变化；其他变化使候选失效。
- 最终 tag 是维护者创建的 SSH 签名 annotated tag。promotion workflow 不构建、不打包、不重压缩，只按精确 artifact ID 下载候选、验证 lock/签名/attestation/hash，上传不可见 draft，再下载逐项核验后公开。
- 禁止覆盖已存在的单一 release asset；任何失败都保留不可见 draft，不允许发布半包。

代价是候选失效后必须重跑真实网络矩阵，且 GitHub artifact 生命周期成为正式门禁的一部分；收益是“测试字节等于发布字节”可以由 manifest、attestation、promotion lock 和签名 tag 共同验证。
