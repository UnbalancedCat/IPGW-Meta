---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
adr: ADR-0008
status: accepted
---

# ADR-0008：离线安装与事务激活共用验证链

校园网实验机可能不具备稳定公网访问，在线下载也无法保证远端测试使用的就是固定候选。安装过程中若在版本发布、active 切换、入口更新或 PATH 更新后失败，还可能留下半安装状态。路径中的 symlink、junction、reparse point 和宽松权限会扩大替换攻击面。

决定如下：

- Unix 与 Windows 安装器提供显式的绝对本地 bundle 和外部 SHA-256 参数；两者必须成对提供。离线模式不得初始化或调用网络。
- acquisition 后在线与离线共用同一验证链：外部 SHA、精确成员/类型/大小、受限解包、内部 SHA256SUMS、规范 manifest、事务激活。
- bundle 先复制到安装器创建的私有临时目录再校验；拒绝 UNC、symlink、junction、reparse point、非普通文件、过大文件和可被非授权主体修改的来源。
- install root、bin dir、config dir 必须为绝对、本地、不重叠、非根路径；逐祖先拒绝链接或重解析点；需要原子 rename 的两端必须同卷。
- 版本目录、active 指针、三个入口、launcher 和 PATH 更新组成可回滚事务。失败时按 journal 恢复；无法确认恢复完成时保留恢复材料并 fail closed。
- 仅为自动化提供固定名称的前向/回滚 failpoint。它们只有在离线模式、私有测试根、匹配 token 且所有目标严格位于测试根内时生效；禁止 eval、任意命令、任意路径和 sleep hook。
- Unix 强制公开/私有文件模式；Windows v1 仅支持当前用户安装，并校验来源和目标 ACL。

代价是需要独立的 Unix/Windows 路径与权限实现，以及真实平台失败注入矩阵；收益是离线实验、在线安装和发布资产都使用同一安全验证与事务语义。
