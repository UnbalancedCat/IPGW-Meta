---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0010
status: accepted
---

# ADR-0010：macOS 固定系统别名作为受信任路径锚点

Unix 私密文件读取必须拒绝用户可控的父目录 symlink 与最终文件 symlink，并从同一个已验证句柄完成 metadata 检查和有界读取。现有实现从 `/` 开始逐组件拒绝全部 symlink；但 macOS 的固定系统布局把字面路径 `/var` 映射到 `/private/var`，标准临时目录因此可能位于 `/var/folders/...`。把这个系统级 alias 当作普通用户路径 symlink 会拒绝合法的 `0600` 当前用户文件，并使迁移 journal、恢复快照和 credential file 在标准 macOS 临时路径下无法读取。仅修改 CI `TMPDIR` 或测试路径会掩盖相同的产品缺陷。

决定如下：

- 路径先经过绝对化与词法清理；Darwin 只允许清理后绝对路径的第一个组件 `/var` 进入固定受信任 alias 分支。`/var` 必须由 UID 0 所有、必须是 symlink，且读取到的原始相对目标必须精确为 `private/var`。当前唯一允许的映射是 `/var` 到规范锚点 `/private/var`；该 allowlist 不从环境变量、配置、文件内容或调用方输入扩展。
- 实现必须从 `/` directory handle 以 no-follow 方式验证 `/var` alias 条目的类型、owner、原始目标和锚定期间的身份稳定性。通过后不得跟随或打开 `/var`；而是从同一个根句柄逐组件 no-follow 打开固定的 `private/var` 规范锚点。缺失、owner/类型/原始目标不符、规范锚点含 symlink 或 alias 条目在锚定期间变化一律 fail closed。
- 规范锚点之后的每个父组件仍必须作为单个相对组件、从已验证 directory handle 以 no-follow 方式打开；最终对象也必须以 no-follow、非阻塞方式取得同一读取句柄，再在该句柄上验证为非 symlink 普通文件，并继续满足既有 owner/mode 合约和有界读取要求。
- 禁止对整条用户路径调用 `EvalSymlinks` 后直接信任结果，禁止接受任意顶层或中间 symlink，也禁止把 `/tmp`、`/etc` 或其他 alias 隐式加入 allowlist。未来扩展必须新建后继 ADR，并同步 [`MIG-FILE-001`](../../operations/config-migration.md#mig-file-001写入与权限)。
- Linux 与其他 Unix 平台、Config Store、安装器、bundle 和 archive 的现有路径行为不变；直接使用 `/private/var/...` 的 macOS 路径继续按普通无 symlink 路径遍历。CLI、JSON 和退出码契约不变，验证失败仍返回 config error。

验收必须在 macOS 原生 runner 上证明：标准 `/var/folders/...` 下当前用户私有普通文件和迁移状态可以读取；受信任锚点之后人为创建的父目录 symlink、最终文件 symlink、过宽 mode 与错误 owner 仍被拒绝。跨平台测试还必须证明非 Darwin 不启用该例外。

代价是 Unix 路径打开多一个 Darwin 专用锚点验证分支和原生回归测试；收益是支持 macOS 标准系统布局，同时没有把用户可控 symlink 解析扩大为通用能力。
