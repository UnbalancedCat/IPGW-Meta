<p align="center">
  <img src="./assets/logo.png" width="200" alt="IPGW-Meta logo"/>
</p>

<h1 align="center">IPGW-Meta</h1>

<p align="center">东北大学非官方、跨平台的校园网关 Go SDK 与命令行客户端</p>

> [!WARNING]
> 本仓库正在执行 `IPGW-META-V1` 收敛计划，当前不是可发布的 v1。历史敏感数据清理、远端协调和真实校园网候选版本验收尚未完成；在这些门禁关闭前不会发布新版本。当前代码可用于审阅、开发和合成测试，但“今天能登录”不等同于发布证据。

## 项目定位

IPGW-Meta 是对 [`neucn/ipgw`](https://github.com/neucn/ipgw) 工作流的下一代升级。项目采用一个公开 Go SDK、内部 NEU/CAS/Srun/Dashboard 协议边界和三套薄入口，重点解决旧实现中的明文 ticket、成功误判、凭据落盘、错误退出码和接口漂移问题。

当前设计坚持以下安全边界：

- status、activation、logout 和认证材料只走正常系统 PKI 验证的 HTTPS；不提供跳过证书或自动 HTTP 降级开关。
- CAS 保留已注册的 HTTP service 字符串，但程序会在带 ticket 的下一跳发出前截获它；ticket 只交给 HTTPS activation。
- 登录成功要求网关业务成功、最终状态在线且最终用户名与目标账号精确一致；公网可访问不是成功证据。
- YAML 只保存 profile 与 credential provider 引用，不保存密码。Base64 是编码，不是加密，也不再作为凭据保护方案。
- JSON、日志、Observer、缓存和诊断不得包含密码、手机号、验证码、Cookie、ticket、LT、QR payload 或原始认证响应。

完整规范以 [`docs/`](docs/README.md) 为唯一事实源；当前进度见 [`docs/upgrade/status.md`](docs/upgrade/status.md)。

## 三个入口

| 二进制 | 用途 | 无参数行为 |
|---|---|---|
| `ipgw-meta` | v1 现代命令入口 | 只读查询状态，不隐式登录或注销 |
| `ipgw-legacy` | 1.x 兼容入口 | 保留旧版无参数登录工作流；最早在 2.0 移除 |
| `ipgw` | 安装级分发器 | 选择并启动同目录下的 meta 或 legacy；自身不处理网络和凭据 |

`ipgw` 的模式选择优先级固定为：

```text
--mode > IPGW_MODE > OS config dir/ipgw-meta/launcher.yaml > 安装批次默认值
```

已有安装升级后必须继续使用 legacy，不能静默切换；通过 v1 门禁后的全新安装才默认 meta。也可临时显式选择：

```shell
ipgw --mode meta status
ipgw --mode legacy status
```

## 从源码构建与验证

构建基线是 Go 1.25。仓库 module 为 `github.com/UnbalancedCat/ipgw-meta`。

```shell
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/doccheck --check

go build -o ipgw ./cmd/ipgw
go build -o ipgw-meta ./cmd/ipgw-meta
go build -o ipgw-legacy ./cmd/ipgw-legacy
```

在支持 `make` 的环境中，`make ci` 还会交叉构建 Windows、Linux、macOS 的 amd64/arm64 三入口组合。secret scan 需要另行安装仓库 CI 固定版本的 gitleaks；完整发布验收命令见 [`docs/operations/release.md`](docs/operations/release.md)。

## Profile 与凭据

profile 保存用户名、可选 bind IP、账号切换策略和 credential provider 引用。现代入口拒绝命令行密码参数。

桌面交互环境默认使用 OS keyring。以下命令会在 TTY 中隐藏输入密码，生成不可预测的全新 keyring 引用，并把该 profile 设为默认：

```shell
ipgw-meta profile add campus --username YOUR_USERNAME --default
```

无人值守环境应由进程管理器或 secret manager 注入环境变量，配置中只记录变量名：

```shell
ipgw-meta profile add campus-env \
  --username YOUR_USERNAME \
  --credential-provider env \
  --credential-ref IPGW_PASSWORD
```

权限受限文件 provider 只记录路径；请用安全工具单独创建文件，不要把密码写进命令历史。建议使用绝对路径：

```shell
ipgw-meta profile add campus-file \
  --username YOUR_USERNAME \
  --credential-provider file \
  --credential-ref /absolute/private/path/ipgw.password
```

Unix credential 文件必须仅由当前用户读取（`0600` 或更严格的只读等价模式）；Windows 必须使用仅允许当前用户及必要系统主体的 ACL。需要每次登录交互输入时可使用：

```shell
ipgw-meta profile add campus-prompt \
  --username YOUR_USERNAME \
  --credential-provider prompt
```

`profile remove` 只删除 profile，不自动删除 provider 管理的凭据。更多参数与限制见 [`CLI-PROFILE-001`](docs/reference/cli.md#cli-profile-001profile)。

## 日常命令

```shell
# 默认 profile
ipgw-meta status
ipgw-meta login
ipgw-meta logout

# 显式 profile 或出站 IPv4
ipgw-meta --profile campus status
ipgw-meta --profile campus --bind-ip 10.0.0.10 login

# 本地接口枚举与逐接口状态扫描
ipgw-meta network list
ipgw-meta network scan
```

异账号在线时默认返回 `session_conflict`；只有显式 `login --switch` 才允许先注销当前会话，且注销或离线复核失败时不会继续认证。

自动化使用 `--json` 或 `--output json`。stdout 恰好输出一个换行结尾的 JSON envelope；脚本应依赖稳定的 `error.code` 和退出码，不应匹配本地化 message。契约见 [`docs/reference/json-cli.md`](docs/reference/json-cli.md)。

## Terminal QR、无 GUI 与手机验证码

QR 必须显式启用，并且仍要求 profile 中存在目标用户名用于最终身份核对：

```shell
ipgw-meta --profile campus login --method qr
```

SDK 不启动浏览器，也不假定设备存在 GUI。普通本地 TTY 与 SSH TTY 可直接在 `stderr` 显示一次性二维码；非 TTY、JSON 模式或无法安全呈现时不会显示或轮询 QR，而是立即返回 `interaction_required`（退出码 7），操作者应改用安全 TTY 或学校官方门户。

任意登录方式都有可能遇到手机验证码。v1 对该分支的能力状态固定为 `observed_anonymous + detected_only`：只识别并安全停止，不发送、不提交、不保存验证码，也不声称已经实测支持。详见 [`docs/runbooks/headless-auth.md`](docs/runbooks/headless-auth.md) 与 [`docs/compatibility/auth-capabilities.md`](docs/compatibility/auth-capabilities.md)。

## 迁移旧配置

迁移器识别旧 `neucn/ipgw` JSON 和早期 IPGW-Meta YAML。交互终端会先给出脱敏 preview、逐 profile 选择 provider，再确认提交：

```shell
ipgw-meta profile migrate
```

非交互或 JSON 模式必须同时提供 `--yes`，并为每个待处理 profile 完整映射到 env 或绝对 file 引用：

```shell
ipgw-meta --json profile migrate --yes \
  --credential campus=env:IPGW_PASSWORD \
  --credential lab=file:/absolute/private/path/lab.password
```

迁移不会把旧密码写入新 YAML，也不会替你创建 env/file 凭据。它采用 journal、原子配置写入、完成 marker 和 `BaseDir/migration-backups/` 下的私密原始来源备份；备份可能仍含旧秘密，必须妥善保护且绝不能提交到 Git。详细恢复与幂等规则见 [`docs/operations/config-migration.md`](docs/operations/config-migration.md)。

## 发布与安装状态

仓库内的安装和打包脚本属于候选发布实现，不代表已有 v1 可安装版本。M0–M3 全部变为 `complete`、同一候选 artifact 通过跨平台自动化与真实校园网验收后，才允许创建新 release。

在此之前，本 README 不提供 `curl | sh`、`irm | iex` 或其他直接执行远程脚本的一键安装命令。开发者应从已审阅的源码构建；未来正式版本应从小写仓库 [`UnbalancedCat/ipgw-meta`](https://github.com/UnbalancedCat/ipgw-meta) 的 release 页面下载并核验完整原子 bundle。内置自更新保持禁用。

## 致谢与声明

感谢 [`neucn/ipgw`](https://github.com/neucn/ipgw) 及社区实现提供的早期探索。本项目为非官方客户端，仅用于个人终端接入与协议研究；不代表东北大学，也不应被用于绕过学校网络或账号安全策略。
