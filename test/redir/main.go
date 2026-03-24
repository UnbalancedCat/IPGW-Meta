package main

import (
	"fmt"
	"io"
	"ipgw-meta/pkg/handler"
	"ipgw-meta/pkg/utils"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	client := utils.NewNetworkClient()

	acID := "1"
	casLoginURL := fmt.Sprintf("https://pass.neu.edu.cn/tpass/login?service=http%%3A%%2F%%2Fipgw.neu.edu.cn%%2Fsrun_portal_sso%%3Fac_id%%3D%s", acID)
	resp, _ := client.Get(casLoginURL)
	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	lt, _ := doc.Find("input[name=\"lt\"]").Attr("value")
	execution, _ := doc.Find("input[name=\"execution\"]").Attr("value")

	username := "xxxx"
	password := "xxxx"
	rsaEncrypted, _ := utils.RSAEncrypt(username+password, handler.DefaultPublicKeyStr)

	formData := url.Values{}
	formData.Set("rsa", rsaEncrypted)
	formData.Set("ul", strconv.Itoa(len(username)))
	formData.Set("pl", strconv.Itoa(len(password)))
	formData.Set("lt", lt)
	formData.Set("execution", execution)
	formData.Set("_eventId", "submit")

	req, _ := http.NewRequest("POST", casLoginURL, strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, _ := client.Do(req)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()

	fmt.Println("Status:", res.StatusCode)
	fmt.Println("Final:", res.Request.URL.String())
	fmt.Println("Body Contains 密码错误:", strings.Contains(string(body), "密码错误"))
	fmt.Println("Body Contains 成功:", strings.Contains(string(body), "成功"))
}
