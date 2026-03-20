package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, _ := url.Parse("http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=ST-REDACTED")
	fmt.Println("Ticket:", u.Query().Get("ticket"))

	u2, _ := url.Parse("https://ipgw.neu.edu.cn/srun_portal_pc?ac_id=1&theme=pro")
	fmt.Println("URL without ticket:", u2.String())
}
