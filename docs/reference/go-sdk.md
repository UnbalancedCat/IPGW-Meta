---
plan_id: IPGW-META-V1
revision: 2026-08-28-r2
---

# Go SDK 参考

module：`github.com/UnbalancedCat/ipgw-meta`
根包：`ipgw`

## SDK-API-001：公开 façade

```go
func NewClient(opts ...Option) (*Client, error)

func WithBindIP(netip.Addr) Option
func WithRoundTripper(http.RoundTripper) Option
func WithObserver(Observer) Option
func WithProtocolStateStore(ProtocolStateStore) Option

func (c *Client) Status(context.Context) (Status, error)
func (c *Client) Login(context.Context, LoginRequest) (LoginResult, error)
func (c *Client) Logout(context.Context) (LogoutResult, error)
func (c *Client) ListInterfaces(context.Context) ([]Interface, error)
```

`NewClient` 不访问网络。SDK 不读取 profile 或配置文件、不选择 keyring、不渲染 CLI，也不持有全局状态。调用者提供的 RoundTripper 不得被修改；认证 Cookie Jar 和 redirect policy 按操作隔离。

## SDK-LOGIN-001：认证输入

```go
type LoginRequest struct {
    Method           AuthMethod
    ExpectedUsername string
    Credentials      CredentialProvider
    Switch           SwitchPolicy
    Interactions     InteractionHandler
}
```

- `AuthMethodPassword` 与 `AuthMethodTerminalQR` 是 v1 允许值。
- `ExpectedUsername` 必填；同账号在线和账号冲突判断发生在读取 CredentialProvider 之前。
- `SwitchRefuse` 为默认；`SwitchLogoutExisting` 必须完成注销和离线验证才继续。
- password 方法要求 CredentialProvider；terminal QR 要求 InteractionHandler。
- Credential 及 QRCodePrompt 没有 JSON 契约，禁止序列化、记录或持久化。

`LoginResult.Outcome` 为 `logged_in` 或 `already_online`；`LogoutResult.Outcome` 为 `logged_out` 或 `already_offline`。两者都携带最终 `Status`。

## SDK-STATUS-001：状态模型

`Status` 包含：

- `NetworkState`：成功到达网关时为 `reachable`；真正的网络失败返回 error，不伪装为 offline；
- `SessionState`：`online`、`offline`、`unknown`；
- 可选 username 和 online IPv4；
- UTC `ObservedAt`；
- 可选 `OnlineSummary`，流量用整数 bytes、时长用整数 seconds、余额用 `Money{Currency, MinorUnits}`。

`ListInterfaces` 是纯本地方法，只返回 UP、非 loopback 的具体 IPv4，结果包含名称、index 和 `netip.Addr`，不探测网关。

## SDK-ERROR-001：错误契约

稳定 `ErrorCode`：

```text
invalid_argument  config  network  authentication
session_conflict  protocol_changed  interaction_required
unsupported       internal
```

`Error` 包含 Code、可本地化 Message、Retryable、脱敏 Details 与不参与 JSON 的 Cause。实现必须按稳定 error code／challenge kind 选择固定的脱敏 Message 模板，不得拼接上游错误、原始响应或任意详情。调用者使用 `CodeOf`/`IsCode`，不能解析或依赖 message。`errors.Is` 必须保留 `context.Canceled` 与 `context.DeadlineExceeded`；CLI 再将用户取消映射为 130。

身份不匹配时 details 可包含 expected/actual username 供直接业务调用者处理，但 CLI、日志和 evidence 必须按安全策略决定是否呈现，默认不得记录账号。

## SDK-INTERACTION-001：人工挑战

```go
type QRCodePrompt struct {
    Payload      string
    ExpiresAt    time.Time
    PollInterval time.Duration
}

type InteractionHandler interface {
    PromptQRCode(context.Context, QRCodePrompt) error
}
```

QR prompt 只在调用期间存在；SDK 管理轮询、取消、期限和最终页面验证。`InteractionDetails` 只允许固定、脱敏元数据：`challenge_kind`、`origin_method`、`capability_status`、可选 `session_binding`、`resume_mode`、`tty_required` 与可选 `help_id`。恢复方式只用固定 `resume_mode` 枚举表达；v1 允许 `retry_in_tty`、`restart`、`official_portal`，会话绑定值使用 `cas_session`。不得提供自由文本用户动作、投递提示或从上游内容派生的字段。

## SDK-OBSERVE-001：可观测性

Observer 只接收封闭 Event：operation started/finished、protocol discovered、interaction required。Event 只含名称、操作、阶段、结果和 UTC 时间；不得加入任意 map、URL、header、body、username、IP 或认证材料。

## SDK-CACHE-001：协议状态存储

`WithProtocolStateStore` 在 v1 只是为未来保留的公开扩展缝，不会启用持久协议缓存的 Load、Save 或 fallback。v1 尚不能生成可靠的 network fingerprint；绑定 IP 不是网络身份，不能据此证明缓存来自同一网络。因此 Client 每次操作都动态发现协议，发现失败时返回 `protocol_changed`。

未来启用 `ProtocolStateStore` 前，必须先具备可靠且不含秘密的 network key。届时存储只允许保存曾通过业务成功和最终身份校验的 discovery 结果，按 network key 隔离且最长 7 天；Load/Save 接受 context，启发式诊断结果不得保存为已验证状态。协议行为以 [`PROTO-DISCOVERY-001`](../architecture/protocol-correctness.md#proto-discovery-001发现优先) 为准。

## SDK-CONCURRENCY-001：并发与边界

- Client 只读调用并发安全；同 Client 的 Login/Logout 串行。
- 所有网络方法接受 context、限制响应大小并设置分阶段超时。
- `Status` offline 是成功结果；HTTPS/TLS/解析失败是 typed error。
- SDK 的安全和成功不变量以 [`protocol-correctness.md`](../architecture/protocol-correctness.md) 为准。
