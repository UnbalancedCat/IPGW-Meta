---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 配置迁移操作手册

## MIG-CONFIG-001：目标布局

配置位于操作系统标准 config 目录，使用带 schema version 的 YAML。三个状态文件必须分离：

1. profiles：username、可选 bind IP、credential provider 类型与引用；
2. launcher：已选择的 legacy/meta 模式，不包含 profile 或秘密；
3. protocol cache：保留独立位置供未来可靠 network fingerprint 使用；v1 禁用持久 Load/Save/fallback，每次动态发现，不以 bind IP 充当 network key。

密码、验证码、Cookie、ticket、TGT、LT、QR payload 和原始响应不得出现在任何文件。桌面默认 keyring；其他内置 provider 为 env、权限受限文件和 TTY prompt。command provider 推迟到 v1 后。

## MIG-SOURCE-001：支持的来源

- 旧 `neucn/ipgw` 的 `~/.ipgw` JSON 配置；
- 当前 IPGW-Meta YAML 配置；
- 已存在的新格式 profiles，用于冲突检测和幂等重跑。

迁移器必须通过内容和 schema 明确识别格式，不能只凭扩展名猜测。JSON/YAML 必须限制大小、只接受单一文档并严格检查已知字段；未知字段以字段路径进入脱敏报告后立即失败，不能静默删除或携带字段值。格式歧义、未知字段或解析失败都必须保持零副作用和原文件不变。

## MIG-PREVIEW-001：迁移流程

1. 只读发现候选文件并解析；不在 discovery 阶段创建目标文件。
2. 每个发现到的旧秘密或缺少可用 provider 的 profile 默认标记为 `pending`，不自动选择 file、keyring 或其他目标。
3. 解决 profile 冲突与 `pending` credential 决策，再生成脱敏 preview：来源类型、profile 名、username 是否存在、bind IP、credential 动作、冲突与将写入的路径。
4. TTY 逐 profile 选择并确认；非交互必须同时使用 `profile migrate --yes` 和完整的显式 credential 映射。
5. 在创建目录、backup、journal、keyring 项或临时文件之前，一次性校验所有参数、来源一致性、冲突和目标配置。任何未解决项都以 config/invalid argument 失败且零副作用。
6. 按 `MIG-TRANSACTION-001` 创建 journal 与恢复材料，生成受限权限 backup，提交并重新读取验证新配置，最后提交并验证 marker。
7. 输出结果、backup 位置与仍需由用户设置的 env/file/prompt 指引；不得打印凭据内容。

## MIG-CREDENTIAL-001：旧密码处理

旧 Base64、弱加密或其他可逆字段必须按明文秘密处理，而不是“解密后重新保存”。即使能成功解码，每个旧秘密也必须先进入 `pending`。解码后的值只能以可清理的内存字节短暂保存，不得放入可序列化 preview、字符串错误、日志或长期 plan 对象。

TTY 模式逐 profile 提供以下选择，并在最终 preview 后确认：

- `keyring`：生成全新、不透明的引用，预检该引用不存在，并只在确认后导入内存中的旧秘密；迁移器永不覆盖既有 keyring 项。桌面 keyring/Secret Service 不可用时，必须解释 headless 环境下的 env/file/prompt 方案，不能自动回退。
- `env`：只把合法环境变量名写为引用；迁移过程不读取或设置环境变量，也不复制旧秘密。
- `file`：只把绝对路径写为引用；迁移过程不读取、创建或写入该 credential 文件，也不复制旧秘密。用户须另行按 `MIG-FILE-001` 安全创建它。
- `prompt`：写入 prompt provider 且不导入旧秘密；后续登录仅在交互终端按需读取。

非 TTY 模式必须使用 `--yes`，并为每个含旧秘密或需要 credential 设置的 profile 提供且只提供一次：

```text
--credential PROFILE=env:VARIABLE
--credential PROFILE=file:ABSOLUTE_PATH
```

非 TTY 禁止选择 keyring 或 prompt。缺失、重复、指向未知 profile、非法环境变量名、非绝对 file 路径或其他 provider 值都必须在任何副作用前失败；已提供的映射也不能携带秘密值。env/file 决策只写引用，成功结果必须明确提示秘密仍需由用户单独设置。

不得自动把旧密码复制到新 YAML、credential file、命令行、journal、marker、日志、backup metadata 或迁移报告。旧来源 backup 仍可能含秘密，必须提示其位置、权限与清理责任，且不能提交到 Git。

## MIG-FILE-001：写入与权限

- Unix credential 文件必须是 `0600`；配置和 marker 至少拒绝其他用户写入。
- Windows credential 文件 ACL 仅允许当前用户及必要系统主体；不能以 Unix mode 检查替代 ACL。
- Config 读取必须从同一个已验证句柄有界完成：Unix 拒绝 symlink、非普通文件、非当前用户 owner 及 group/other 可写文件；Windows 拒绝 reparse、非磁盘普通文件、未保护或授予非受信主体访问的 DACL。不得先按路径检查后再重新打开。
- 所有普通 Config `Save`/`Update` 与迁移事务共享固定、常驻的跨进程 mutation lock。Unix 使用受保护目录中的 `flock` 句柄，Windows 使用独占文件句柄；锁文件不承载 PID、事务状态或秘密，也不在解锁时删除，进程崩溃由内核释放锁。存在 pending migration journal 时，普通 `Save`/`Update` 必须 fail closed。
- 发布前的写入、权限、文件 flush 或替换失败必须保留原配置和临时恢复信息，不得宣布成功。若原子替换已发布新 Config、但最后的父目录 durability barrier 失败，返回独立的“已发布但持久性未确认”config 错误；此时不得删除已被新 Config 引用的 keyring 项，也不得谎报普通未提交失败。
- backup 保存经 apply 前一致性复核的完整来源字节，禁止截断。实现固定写入 `BaseDir/migration-backups/`，而不是旧来源文件旁边；名称格式为 `<source-kind>-<transaction-id>-<ordinal>.backup`，不得含 username、IP、时间戳或 provider secret。
- backup 目录在 Unix 为 `0700`，backup 文件使用私密 `0600`；Windows 使用受保护的当前用户及必要系统主体 ACL。当前实现不把 backup 标记为只读，因此运维保护与清理不能依赖 `0400` 或只读属性。创建时必须 flush 文件和父目录。
- backup 仍是旧来源的完整副本，可能包含可恢复秘密；成功结果和 marker 记录其受限路径，操作者负责限制访问、在确认不再需要回滚后安全处置，并保证其永不进入版本控制或公共证据。
- 损坏的新配置不得触发默认配置回写。

## MIG-TRANSACTION-001：Journal、提交与回滚

迁移事务使用随机、无秘密的 transaction ID。固定 mutation lock 覆盖 `Recover pending journal → 重新读取并核对 preview 目标 Config 代次 → 完整事务提交/回滚`，因此 profile 更新不能在该区间交错；preview 后目标 Config 已变化时必须在 backup、keyring、Config、marker 等事务副作用前失败。pending journal 是 exclusive-create 的崩溃恢复记录，不充当进程锁。journal 必须限制大小、严格解析、受限权限、每次 phase 更新均原子替换并持久化；其中只能保存：journal schema、transaction ID、phase、工具/目标 schema、脱敏来源 stamp、计划 backup 路径、计划创建的全新 keyring 引用、无秘密配置/marker 恢复快照位置及其摘要。journal 不得保存旧秘密、原始来源、原始来源摘要、credential 值或可逆认证材料。

discovery 可在内存中保存原始来源摘要。apply 在首个副作用前重新读取来源并比较该摘要；不一致时销毁内存秘密、停止并重新 preview，原始摘要不得进入 journal、marker、JSON 或日志。持久化来源 stamp 只包含来源类型、无秘密 location ID、文件身份/修改代次和将秘密值替换为固定占位符后的规范结构摘要。

通过全部预检后按以下单调 phase 提交：

1. 深拷贝并验证目标 Config，准备原配置和旧 marker 的无秘密恢复快照；
2. 写入 `prepared` journal，预先登记全部 backup 路径和全新 keyring 引用；
3. 创建并验证受限权限 backup；
4. 只创建已确认且预检不存在的 keyring 项；env/file/prompt 无秘密写入；
5. 原子提交目标 Config，重新严格读取并核对其无秘密规范摘要；
6. 原子提交 marker，重新严格读取并同时验证来源 stamp、目标 Config 摘要和 transaction ID；
7. 标记 `marker_verified`，删除恢复快照和 pending journal，并持久化目录变更。

marker 验证前任一步失败或进程崩溃，下一次迁移必须先依据 journal 逆序回滚：仅在当前 Config/marker 仍匹配本事务摘要时恢复原值，删除本事务创建的 keyring 项和 backup，再验证恢复结果。不得覆盖并发产生的未知状态。任一步无法安全回滚时保留 journal 与恢复材料，返回 config 错误并要求人工恢复。若 marker 已验证而只剩清理未完成，则验证现状后继续清理，不重复导入或重新创建 backup。

## MIG-IDEMPOTENT-001：Marker 与重跑

marker 至少包含 migration schema、transaction ID、脱敏来源 stamp、目标 schema、目标 Config 的无秘密规范摘要、完成时间、工具版本和 backup 记录。marker 必须采用严格已知字段与单文档解析；未知字段、损坏内容或超限文件均为 config 错误。再次运行时：

- 来源 stamp 未变，且目标 Config 存在、严格解析成功、schema 和规范摘要均与 marker 一致：报告 already migrated，不重复写 keyring 或创建 backup；
- 来源变化：重新 preview 并按冲突规则处理；
- marker 存在但目标配置缺失、损坏、schema/摘要不符或 transaction 不一致：失败并给出恢复指引，不自动重建或覆盖。

## MIG-TEST-001：验收

必须用合成 fixture 覆盖：

- 两类来源、格式歧义、未知字段、无效/多文档 JSON/YAML、超限输入和 preview/apply 间来源变化；
- TTY 的 keyring/env/file/prompt 决策、keyring 不可用、已有引用拒绝、用户拒绝以及秘密只在实际 keyring 导入时读取；
- 非 TTY `--yes` 的完整 env/file 映射，以及缺失、重复、未知 profile、相对路径、keyring/prompt 选择均零副作用失败；
- 冲突、深拷贝、权限/flush/替换失败和每个 journal phase 的失败注入、崩溃恢复、回滚失败保留材料；
- 不同 Store/进程并发更新无丢失、pending journal 阻断普通写入、preview 后目标代次变化零副作用失败，以及迁移全过程持有同一 mutation lock；
- 重复运行、来源变化、marker 未知字段/损坏、目标缺失/损坏/摘要不一致和 `marker_verified` 后清理恢复；
- 旧密码、可逆编码、原始来源摘要不出现在 stdout、stderr、JSON、新 Config、journal、marker、文件名或错误文本的泄漏 canary。

测试 fixture 不得来自真实用户配置。验收至少运行 `go test -race ./internal/config ./internal/cli`，并在 Windows 与 Unix 分别验证 ACL/mode、`BaseDir/migration-backups/` 路径与命名、备份内容完整性和原子替换行为。
