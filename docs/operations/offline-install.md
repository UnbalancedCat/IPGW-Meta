---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# 离线安装规范

本文规定 v1 的离线 acquisition、归档验证和事务激活。离线测试使用的安装器与公开 release asset 相同，不允许远端重新打包。

## REL-INSTALL-001：离线 acquisition

Unix 固定接口：

```text
install.sh --bundle ABS_ARCHIVE
           --bundle-sha256 HEX64
           [--version EXPECTED]
           [--install-root ABS_PATH]
           [--bin-dir ABS_PATH]
```

Windows 固定接口：

```text
install.ps1 -BundlePath ABS_ARCHIVE
            -BundleSha256 HEX64
            [-Version EXPECTED]
            [-InstallRoot ABS_PATH]
            [-BinDir ABS_PATH]
```

- bundle 与 SHA-256 必须同时出现；缺少任一项均在产生副作用前失败。
- 离线模式不得初始化下载器、解析 release URL、执行 DNS 或调用网络。
- bundle 必须是绝对、本地、大小为 `1..100 MiB` 的普通文件；拒绝 symlink、junction、reparse point、UNC、设备路径和其他非磁盘文件。
- Unix 拒绝 group/world writable 来源；Windows 拒绝 Users、Authenticated Users 或 Everyone 可写来源。
- 安装器先把来源复制到自己创建的私有临时目录，再对副本验证调用方提供的 SHA-256；验证期间不再次读取外部路径。
- 在线与离线仅 acquisition 不同，随后必须进入同一条验证和激活链，不能维护宽松的离线旁路。

## REL-INSTALL-002：归档、路径与权限

共同验证链固定为：

```text
outer SHA-256
→ exact member names/types/sizes
→ bounded extraction
→ inner SHA256SUMS
→ canonical release manifest
→ transactional activation
```

- manifest 必须精确列出当前平台的三个入口、launcher 元数据、版本、平台、大小和 SHA-256；多余、缺失、重复或大小不符的成员全部拒绝。
- 只接受普通文件和必要目录；拒绝绝对路径、`..`、alternate data stream、控制字符、大小写或 Unicode 归一化冲突、symlink、hardlink、FIFO、设备和其他特殊成员。
- 解包必须限制单成员大小、总大小、成员数量和压缩比，并始终写入安装器创建的私有 stage。
- install root、bin dir 和 config dir 必须是绝对、本地、非根路径，彼此不得相同、祖先重叠或指向仓库/用户主目录等宽泛目标。
- 从现有祖先到最终目标逐级拒绝 symlink、junction 和 reparse point；路径校验与打开/替换必须尽量使用同一已验证句柄或平台原语，避免 check/use 之间重新解析。
- 需要原子 rename 的 stage、version、active 和 backup 必须位于同一卷；无法证明时停止，不能复制后伪装成原子切换。
- Unix 正式目录为 `0755`、binary 为 `0755`、公开 metadata 为 `0644`，私有 stage/backup 为 `0700/0600`。Windows v1 仅当前用户安装，私有目录只允许当前用户、SYSTEM 和 Administrators。

## REL-INSTALL-003：事务、回滚与失败注入

安装事务依次发布已验证 version、分离旧 active、切换新 active、发布三个入口、发布 launcher、更新 PATH、持久化 commit。失败时依据受限 journal 逆序恢复；未知或无法安全恢复的状态必须保留恢复材料并 fail closed。

固定前向 failpoint：

```text
after_verified_stage
after_version_publish
after_old_active_detach
after_active_switch
after_entry_1
after_entry_2
after_launcher_publish
after_path_update
before_commit
```

固定回滚 failpoint：

```text
before_restore_active
before_restore_entry_1
before_remove_new_version
```

测试变量只允许 `IPGW_INSTALL_TEST_ROOT`、`IPGW_INSTALL_TEST_TOKEN`、`IPGW_INSTALL_TEST_FAILPOINT`、`IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT`。只有离线模式、测试根由当前运行创建且权限私有、token 精确匹配、所有输入和目标经解析后严格位于测试根内时才生效。禁止 eval、任意命令/路径、命令替换和 sleep hook。

六个原生平台都必须执行 fresh install、upgrade、三入口 `--version`、launcher 默认行为和基础 rollback。Linux amd64、Windows amd64、macOS arm64 还必须覆盖所有固定 failpoint、rollback failure、路径攻击和权限矩阵。原生 runner 不可用时门禁为 blocked，交叉编译不能替代。
