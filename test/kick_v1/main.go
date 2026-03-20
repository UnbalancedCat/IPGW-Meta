package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func main() {
	req, _ := http.NewRequest("POST", "https://ipgw.neu.edu.cn/v1/batch-online-drop", strings.NewReader(""))
	req.Header.Set("Cookie", "lang=zh-CN; mysession=MTc3MjYyMjUzOXxEdi1CQkFFQ180SUFBUkFCRUFBQUl2LUNBQUVHYzNSeWFXNW5EQTBBQzJOb1pXTnJaV1JoWTJsa0EybHVkQVFDQUFJPXwDWwMYIczN7qP_2DjmW4CJ-VwIcOajxq-XxQ8l417Gaw==; _csrf-8800=7e0e135adb886c90ee984054853f908af341959887d6384824ec7cd7af7cb587a%3A2%3A%7Bi%3A0%3Bs%3A10%3A%22_csrf-8800%22%3Bi%3A1%3Bs%3A32%3A%22jhqf4SbRDwBt2nfsMadwtBKC0eVvHz-M%22%3B%7D; PHPSESSID_8800=1uolniaj5rlop1nrn7tcra9736")
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
