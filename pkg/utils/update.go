package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

type ReleaseInfo struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
	Body    string  `json:"body"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchLatestRelease 从 Github 拉取最新版本信息
func FetchLatestRelease() (*ReleaseInfo, error) {
	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/UnbalancedCat/IPGW-Meta/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	// 避免受到 API rate limit 影响，尽量请求公有读取
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 请求失败 (状态码 %d)", resp.StatusCode)
	}

	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// GetAssetForCurrentPlatform 为当前操作系统匹配打包资产
func GetAssetForCurrentPlatform(rel *ReleaseInfo) *Asset {
	target := fmt.Sprintf("ipgw-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if a.Name == target {
			return &a
		}
	}
	return nil
}
