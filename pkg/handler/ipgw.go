package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ipgw-meta/pkg/model"
	"ipgw-meta/pkg/utils"

	"github.com/PuerkitoBio/goquery"
)

const (
	PublicKeyStr = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnjA28DLKXZzxbKmo9/1WkVLf1mr+wtLXLXt6sC4WiBCtsbzF5ewm7ARZeAdS3iZtqlYPn6IcUoOw42H8nAK/tfFcIb6dZ1K0atn0U39oWCGPzYuKtLJeMuNZiDXVuAXtojrckOjLW9B3gUnaNGLuIx0fYe66l0o9WjU2cGLNZQfiIxs2h00z1EA9IdSnVxiVQWSD+lsP3JZXh2TT287la4Y4603SQNKTK/QvXfcmccwTEd1IW6HwGxD6QrkInBiHisKWxmveN7UDSaQRZ/J97G0YC32pD38WT53izXeK0p/kU/X37VP555um1wVWFvPIuc9I7gMP1+hq5a+X6c++tQIDAQAB"
	CASLoginURL  = "https://pass.neu.edu.cn/tpass/login?service=http%3A%2F%2Fipgw.neu.edu.cn%2Fsrun_portal_sso%3Fac_id%3D1"
)

// IPGWHandler 处理具体的登录、注销等网关交互
type IPGWHandler struct {
	client *http.Client
}

// NewIPGWHandler 生成核心网关处理器
func NewIPGWHandler() *IPGWHandler {
	return &IPGWHandler{
		client: utils.NewNetworkClient(),
	}
}

// DoLogin 执行完整的登录流程
func (h *IPGWHandler) DoLogin(username, password string) error {
	utils.Log.Debug("正在初始化网关底层环境...")
	// 忽略该步的超时不报错，对标 python 中的 pass
	h.client.Get("http://ipgw.neu.edu.cn/srun_portal_pc?ac_id=1&theme=pro")

	utils.Log.Debug("第一步：拉取统一身份认证（CAS）拦截参数")
	resp, err := h.client.Get(CASLoginURL)
	if err != nil {
		return fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 推荐使用 goquery 解析 DOM 树（容错率远强于硬正则）
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return fmt.Errorf("HTML文档解析失败: %w", err)
	}

	lt, _ := doc.Find("input[name='lt']").Attr("value")
	execution, _ := doc.Find("input[name='execution']").Attr("value")

	if lt == "" || execution == "" {
		// 备用：如果 HTML 结构彻底变了找不到，尝试正则兜底
		bodyBytes, _ := io.ReadAll(resp.Body) // 其实已经被 goquery 消费了，这里是个兜底逻辑占位
		_ = bodyBytes
		return errors.New("提取 CAS 动态参数 (lt/execution) 失败，统一认证前端代码可能已更改")
	}

	utils.Log.Debug("提取出签发令牌", "lt", lt, "execution", execution)
	utils.Log.Debug("第二步：对凭证密码进行公钥级 RSA 混淆包装")
	rsaEncrypted, err := utils.RSAEncrypt(username+password, PublicKeyStr)
	if err != nil {
		return fmt.Errorf("RSA 密码加密失败: %w", err)
	}

	utils.Log.Info("正在向认证中枢发起主体验证...")
	formData := url.Values{}
	formData.Set("rsa", rsaEncrypted)
	formData.Set("ul", strconv.Itoa(len(username)))
	formData.Set("pl", strconv.Itoa(len(password)))
	formData.Set("lt", lt)
	formData.Set("execution", execution)
	formData.Set("_eventId", "submit")

	req, err := http.NewRequest("POST", CASLoginURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 关键：因为 http.Client 默认会自动处理 302 重定向，
	// 所以我们需要写一个 Hook 来捕获包含 ticket 的跳转链接
	var capturedTicket string
	h.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if t := req.URL.Query().Get("ticket"); t != "" {
			capturedTicket = t
		}
		return nil
	}

	loginResp, err := h.client.Do(req)
	h.client.CheckRedirect = nil // 恢复默认行为

	if err != nil {
		return fmt.Errorf("提交登录请求发生异常: %w", err)
	}
	defer loginResp.Body.Close()

	finalURL := loginResp.Request.URL.String()
	if strings.Contains(finalURL, "srun_portal") || strings.Contains(finalURL, "cas_login") {
		utils.Log.Debug("[+] 已成功抵达重定向页面！")

		if capturedTicket == "" {
			capturedTicket = loginResp.Request.URL.Query().Get("ticket")
		}

		if capturedTicket == "" {
			// 在有些情况下设备已在线，或者302直接消耗掉了ticket
			// 但如果有确切的报错关键字，优先阻断并向用户明确错误原因
			bodyBytes, _ := io.ReadAll(loginResp.Body)
			bodyStr := string(bodyBytes)
			if strings.Contains(bodyStr, "密码错误") || strings.Contains(bodyStr, "不正确") || strings.Contains(bodyStr, "无效的") || strings.Contains(bodyStr, "不存在") {
				return errors.New("校园网统一认证失败: 账号不存在或密码错误")
			}

			utils.Log.Warn("未提取到 Ticket，网关底层可能已自行认证放行...")
			// 尝试直接探测网络连通性
			return h.TestInternet()
		}

		utils.Log.Debug("成功截获 Ticket: ", "ticket", capturedTicket)
		utils.Log.Info("向深澜底层 API 发送强制激活指令...")

		apiURL := fmt.Sprintf("https://ipgw.neu.edu.cn/v1/srun_portal_sso?ac_id=1&ticket=%s", capturedTicket)
		apiReq, _ := http.NewRequest("GET", apiURL, nil)
		apiReq.Header.Set("X-Requested-With", "XMLHttpRequest")
		apiReq.Header.Set("Referer", finalURL)

		apiRes, err := h.client.Do(apiReq)
		if err != nil {
			return fmt.Errorf("调用深澜 API 激活异常: %w", err)
		}
		defer apiRes.Body.Close()

		resBytes, _ := io.ReadAll(apiRes.Body)
		utils.Log.Debug("深澜 API 激活反馈", "Response", string(resBytes))

		utils.Log.Debug("等待核心交换机应用放行规则 (休眠 3 秒)...")
		time.Sleep(3 * time.Second)

		return h.TestInternet()
	}

	return errors.New("登录流程异常，请检查学号与密码是否正确，或者未进入预期的跳转链")
}

// TestInternet 验证 IPv4 公网连通性
// 由于用户可能使用 IPv6 代理（如 Clash TUN）导致 Bing 测试出现假阳性，
// 这里改用访问纯 IPv4 的直连探针或直接复查网关在线状态来确保判断的严格性。
func (h *IPGWHandler) TestInternet() error {
	// 如果用户通过代理访问，直接测试 bing 会被代理接管，我们改为测试网关认证接口的最终放行状态
	_, isLoggedIn, _, _, err := h.GetNetworkStatus()
	if err != nil {
		return fmt.Errorf("连通性验证失败(无法访问网关): %w", err)
	}

	if !isLoggedIn {
		return errors.New("检测到网络请求可能被劫持或网关未予放行（未登录验证）")
	}

	// 进一步：如果确认在网关层面已登录，尝试探测纯 IPv4 站点 (比如只提供 IPv4 的 IP 查询接口)
	// 此处即使有 IPv6 代理，如果是纯 v4 节点，代理可能也会去解析。
	// 但无论如何，依靠 GetNetworkStatus 的双重确认已经足以排除“假登录”的情况。

	testReq, _ := http.NewRequest("GET", "http://connect.rom.miui.com/generate_204", nil)
	testResp, err := h.client.Do(testReq)
	if err != nil {
		return fmt.Errorf("外网探测异常（可能被防火墙拦截）：%w", err)
	}
	defer testResp.Body.Close()

	if testResp.StatusCode == 204 || testResp.StatusCode == 200 {
		return nil
	}

	return fmt.Errorf("探测由于未预期的 HTTP 状态码（%d）失败", testResp.StatusCode)
}

// DoLogout 向网络核心交换机抛出强制断联注销请求
func (h *IPGWHandler) DoLogout(username string) error {
	logoutURL := "https://ipgw.neu.edu.cn/cgi-bin/srun_portal?action=logout&username=" + username
	req, _ := http.NewRequest("GET", logoutURL, nil)

	// Srun 典型的假请求头要求骗过 WAF
	req.Header.Set("Referer", "https://ipgw.neu.edu.cn/srun_portal_success?ac_id=1")

	utils.Log.Debug("发送底层深澜注销指令", "URL", logoutURL)
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("提交注销请求网络被阻断: %w", err)
	}
	defer resp.Body.Close()

	if bodyBytes, err := io.ReadAll(resp.Body); err == nil {
		utils.Log.Debug("注销接口回显", "ResponseBody", string(bodyBytes))
	}

	return nil
}

// GetNetworkStatus 查询网关在线状态，并判断是否身处校园网环境
func (h *IPGWHandler) GetNetworkStatus() (inCampus bool, isLoggedIn bool, username string, ip string, err error) {
	utils.Log.Debug("试图连接深澜网关内部接口检测校园网环境...")

	req, _ := http.NewRequest("GET", "http://ipgw.neu.edu.cn/cgi-bin/rad_user_info", nil)
	client := utils.NewNetworkClient()
	client.Timeout = 3 * time.Second

	resp, err := client.Do(req)
	if err != nil {
		utils.Log.Debug("访问网关 API 失败，推断未在校园网环境中", "error", err)
		return false, false, "", "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false, false, "", "", fmt.Errorf("网关请求状态异常：%d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(bodyBytes))

	// 深澜 /cgi-bin/rad_user_info 默认会返回 CSV 或特定的错误字符
	if bodyStr == "not_online_error" {
		// 处于校园网内，但未登录
		// 这种纯文本模式不带 client_ip，如果是 JSON 则会带上
		return true, false, "", "", nil
	} else if strings.Contains(bodyStr, ",") {
		// 如果包含逗号，代表用 CSV 形式返回了当前账号数据
		parts := strings.Split(bodyStr, ",")
		if len(parts) >= 9 {
			username = parts[0]
			ip = parts[8] // 第 9 个参数通常为 IP
			return true, true, username, ip, nil
		}
	} else if strings.Contains(bodyStr, `"error":"ok"`) || strings.Contains(bodyStr, `"error":"not_online_error"`) {
		// 兼容 JSON 模式回显
		if strings.Contains(bodyStr, `"error":"ok"`) {
			usrRegex := regexp.MustCompile(`"user_name":"([^"]+)"`)
			ipRegex := regexp.MustCompile(`"online_ip":"([^"]+)"`)
			if match := usrRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
				username = match[1]
			}
			if match := ipRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
				ip = match[1]
			}
			return true, true, username, ip, nil
		}
		ipRegex := regexp.MustCompile(`"client_ip":"([^"]+)"`)
		if match := ipRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
			ip = match[1]
		}
		return true, false, "", ip, nil
	}

	return true, false, "", "", fmt.Errorf("网关返回数据无法识别具体登录状态（HTTP %d）：%s", resp.StatusCode, bodyStr)
}

// FetchGatewayInfo 只从底层网关 API 抓取当前连接设备的流量状态
func (h *IPGWHandler) FetchGatewayInfo() (*model.GatewayInfo, error) {
	req, _ := http.NewRequest("GET", "http://ipgw.neu.edu.cn/cgi-bin/rad_user_info", nil)
	client := utils.NewNetworkClient()
	client.Timeout = 3 * time.Second

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网关 API 请求失败 (可能未连接校园网): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("网关返回异常状态码: %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(bodyBytes))

	if bodyStr == "not_online_error" {
		return nil, errors.New("当前设备尚未登录认证，请先使用 ipgw login 进行登录")
	}

	if strings.Contains(bodyStr, ",") {
		parts := strings.Split(bodyStr, ",")
		if len(parts) >= 12 {
			traffic, _ := strconv.ParseInt(parts[6], 10, 64)
			duration, _ := strconv.ParseInt(parts[7], 10, 64)
			balance, _ := strconv.ParseFloat(parts[11], 64)

			return &model.GatewayInfo{
				Username: parts[0],
				Traffic:  traffic,
				Duration: duration,
				IP:       parts[8],
				Balance:  balance,
			}, nil
		}
	}

	return nil, errors.New("解析网关返回数据失败，格式已变更")
}

// DropAllConnections 请求网关的全部下线接口（等同于浏览器面板中的断开全部连接）。
// 注意：该操作会导致当前设备也断开网络，需要重新 login。
func (h *IPGWHandler) DropAllConnections(cookie string) error {
	req, _ := http.NewRequest("POST", "https://ipgw.neu.edu.cn/v1/batch-online-drop", strings.NewReader(""))
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	client := h.client

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发起断开全部连接请求失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	if strings.Contains(bodyStr, `"code":0`) || strings.Contains(bodyStr, "操作成功") {
		return nil
	}
	if strings.Contains(bodyStr, `"code":1`) && strings.Contains(bodyStr, "此账号不在线") {
		return errors.New("当前账号下没有任何在线设备")
	}

	return fmt.Errorf("网关返回异常响应: %s", bodyStr)
}
