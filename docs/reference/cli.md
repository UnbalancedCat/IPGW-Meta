---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# CLI 参考

## CLI-BINARY-001：三个入口

| 二进制 | 无参数行为 | 生命周期 |
|---|---|---|
| `ipgw-legacy` | 保留 1.x 旧版无参数登录 | 整个 1.x 支持，最早 2.0 移除 |
| `ipgw-meta` | 只读 status/help，不隐式登录或注销 | 新命令主入口 |
| `ipgw` | 按模式分发，不执行网络或凭据逻辑 | 安装级稳定入口 |

分发模式优先级为 `--mode` > `IPGW_MODE` > 独立 launcher 配置 > 安装批次默认值。现有安装永不被升级静默切换；新安装从 v1 起默认 meta。

## CLI-TREE-001：现代命令树

```text
ipgw-meta
  status
  login [--method password|qr] [--switch]
  logout
  network
    list
    scan
  profile
    list
    show
    add
    remove
    migrate [--yes] [--credential PROFILE=PROVIDER]
```

后续 1.x 才加入 `session` 与 `account`。`network list` 只枚举本地 UP、非 loopback IPv4；`network scan` 才对候选地址组合调用 status。

现代入口接受以下全局参数：

| 参数 | 含义 |
|---|---|
| `--json` / `--output json` | 启用单 envelope JSON 输出 |
| `--output human` | 显式使用人类输出 |
| `--profile NAME` | 选择 named profile；省略时使用默认 profile |
| `--bind-ip IPv4` | 为本次请求覆盖出站 IPv4 |
| `--config PATH` | 覆盖 profile 配置路径；相关 marker、journal、protocol cache 与 migration backup 以该文件父目录为 BaseDir |

## CLI-AUTH-001：登录与账号切换

- 默认 method 是 `password`；QR 必须显式使用 `login --method qr`。
- `ExpectedUsername` 必须来自显式参数或选定 profile，不得从在线状态反推目标账号。
- 同账号已在线返回成功 `already_online`，且不读取凭据。
- 异账号在线默认退出 5；只有显式 `--switch` 才先注销，注销及离线复核失败时不得继续。
- meta 不提供推荐的命令行密码参数。`ipgw-legacy -p` 只为 1.x 兼容保留，并必须警告 shell history 风险。
- 非 TTY 或 JSON 模式遇到二维码、OTP 或设备验证时不呈现、不轮询，返回 `interaction_required` 和退出码 7。

## CLI-STREAM-001：输出通道

人类模式为默认。业务结果写 stdout；提示、二维码、警告和脱敏诊断写 stderr。`--json` 时 stdout 恰好包含一个以换行结尾的 JSON envelope；stderr 不得混入 JSON 或秘密。取消、参数解析错误和启动期失败也遵循同一规则。

## CLI-EXIT-001：稳定退出码

| 退出码 | 错误 code / 语义 |
|---:|---|
| 0 | 成功，包括 offline status、already_online、already_offline |
| 1 | `internal` 或未分类错误 |
| 2 | usage、`config`、`invalid_argument`、`unsupported` |
| 3 | `network`、deadline |
| 4 | `authentication` |
| 5 | `session_conflict` |
| 6 | `protocol_changed` |
| 7 | `interaction_required` |
| 130 | 用户取消、Ctrl-C、`context.Canceled` |

命令实现必须返回错误到统一入口映射退出码；禁止打印错误后返回 `nil`。

## CLI-PROFILE-001：Profile

profile 是应用层概念，保存 username、可选 bind IP、switch policy 与 credential provider 引用，不保存密码。支持的 provider 为 `keyring`、`env`、`file` 和 `prompt`；command provider 不属于 v1。

### 新建 profile

桌面交互环境默认选择 keyring。省略 `--credential-provider` 时，CLI 生成不可预测的全新引用，在人类 TTY 中隐藏输入密码，确认 keyring 写入并回读成功后才提交 profile；该路径在非 TTY 或 JSON 模式下失败，不从 keyring 自动降级：

```shell
ipgw-meta profile add campus --username YOUR_USERNAME --default
```

其他 provider 只保存引用或读取策略，不在 profile 命令中接收密码：

```shell
ipgw-meta profile add campus-env \
  --username YOUR_USERNAME \
  --credential-provider env \
  --credential-ref IPGW_PASSWORD

ipgw-meta profile add campus-file \
  --username YOUR_USERNAME \
  --credential-provider file \
  --credential-ref /absolute/private/path/ipgw.password

ipgw-meta profile add campus-prompt \
  --username YOUR_USERNAME \
  --credential-provider prompt
```

可选的 `--network-bind-ip IPv4` 写入 profile 级绑定，`--switch refuse|logout-current` 写入默认切换策略；未指定时采用 `refuse`。`--default` 将新 profile 设为默认；若此前没有默认 profile，第一个新 profile 自动成为默认。

file provider 的路径应使用绝对路径。迁移命令强制绝对路径；普通 profile 虽可把相对引用解释为 BaseDir 下路径，也不建议依赖这一行为。Unix credential 文件不得授予 group/other 或 execute 权限；Windows 必须通过当前用户 ACL 检查。profile YAML 只记录引用。

`profile list`、`profile show [NAME]` 和 `profile remove NAME` 分别用于枚举、查看和删除。remove 不删除 keyring 项、环境变量或 credential file，防止在共享 provider 场景误删秘密；需要时由用户在对应 provider 中单独清理。

### 迁移旧配置

交互迁移先输出脱敏 preview，逐 profile 解决冲突和 credential provider，再请求最终确认：

```shell
ipgw-meta profile migrate
```

TTY 可选择 `keyring`、`env:VARIABLE`、`file:ABSOLUTE_PATH` 或 `prompt`。只有来源中存在可安全导入的旧秘密时才允许 keyring；CLI 为其创建全新不透明引用，且绝不覆盖既有 keyring 项。可重复提供 `--credential` 预先解决部分或全部 profile：

```shell
ipgw-meta profile migrate \
  --credential campus=keyring \
  --credential lab=prompt
```

非 TTY 或 JSON 模式必须使用 `--yes`，并为每个 pending profile 恰好提供一个 env 或绝对 file 映射；keyring 和 prompt 在自动化迁移中被拒绝：

```shell
ipgw-meta --json profile migrate --yes \
  --credential campus=env:IPGW_PASSWORD \
  --credential lab=file:/absolute/private/path/lab.password
```

映射值是 provider 引用，不是密码。迁移器不读取或设置 env，不创建或写入 credential file；操作者必须通过独立的秘密管理流程准备它们。缺失、重复、未知 profile、相对 file 路径、未解决冲突或配置损坏都会在副作用前失败，并在 JSON 的脱敏 `error.details.migration` 中提供不含秘密的报告。

迁移的事务、备份与幂等规则详见 [`MIG-CONFIG-001`](../operations/config-migration.md#mig-config-001目标布局)。
