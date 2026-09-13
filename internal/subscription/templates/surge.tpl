[General]
loglevel = notify
skip-proxy = localhost, *.local, 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4
dns-server = 223.5.5.5, 119.29.29.29
test-timeout = 5
proxy-test-url = http://www.gstatic.com/generate_204

[Proxy]
{{proxies}}

[Proxy Group]
PROXY = select, AUTO, {{proxy_names}}
AUTO = url-test, {{proxy_names}}, url=https://www.gstatic.com/generate_204, interval=300

[Rule]
GEOIP,CN,DIRECT
FINAL,PROXY
