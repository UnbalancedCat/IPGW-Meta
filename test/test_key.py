import urllib.request
import re

url = "https://pass.neu.edu.cn/tpass/login?service=http%3A%2F%2Fipgw.neu.edu.cn%2Fsrun_portal_sso%3Fac_id%3D1"

try:
    req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
    html = urllib.request.urlopen(req).read().decode('utf-8')
    scripts = re.findall(r'<script[^>]+src=["\']([^"\']+)["\']', html)
    print("Found scripts:", scripts)
    
    for src in scripts:
        if "login" in src or "rsa" in src:
            if src.startswith('/'):
                script_url = "https://pass.neu.edu.cn" + src
            elif not src.startswith('http'):
                script_url = "https://pass.neu.edu.cn/tpass/" + src
            else:
                script_url = src
                
            print("Fetching:", script_url)
            js_req = urllib.request.Request(script_url, headers={'User-Agent': 'Mozilla/5.0'})
            js = urllib.request.urlopen(js_req).read().decode('utf-8')
            
            # Look for const publicKeyStr = ...
            match = re.search(r'publicKeyStr\s*=\s*[\'"](MIIBI.*?)[\'"]', js)
            if match:
                print("FOUND KEY IN", script_url)
                print("KEY:", match.group(1)[:50] + "...")
except Exception as e:
    print("Error:", e)
