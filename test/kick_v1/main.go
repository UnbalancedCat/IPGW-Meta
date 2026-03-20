package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func main() {
	req, _ := http.NewRequest("POST", "https://ipgw.neu.edu.cn/v1/batch-online-drop", strings.NewReader(""))
	req.Header.Set("Cookie", "lang=zh-CN; mysession=<redacted>; _csrf-<redacted>=<redacted>; phpsessid_<redacted>=<redacted>")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("状态码: %d\n响应内容: %s\n", resp.StatusCode, string(body))
}
