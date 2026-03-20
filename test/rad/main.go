package main

import (
	"fmt"
	"io"
	"net/http"
)

func main() {
	resp, err := http.Get("http://10.100.61.3/cgi-bin/rad_user_info")
	if err != nil {
		resp, err = http.Get("https://ipgw.neu.edu.cn/cgi-bin/rad_user_info")
	}
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("UserInfo:", string(body))
}
