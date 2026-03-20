package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=ST-564129-FK9cbB3ddhxgxWFfUC1e-tpass")
	fmt.Println("Ticket:", u.Query().Get("ticket"))

	u2, _ := url.Parse("https://ipgw.neu.edu.cn/srun_portal_pc?ac_id=1&theme=pro")
	fmt.Println("URL without ticket:", u2.String())
}
