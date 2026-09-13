[General]
loglevel = notify
ipv6 = false
skip-proxy = localhost, *.local, 0.0.0.0/8, 10.0.0.0/8, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4
dns-server = 223.6.6.6, 119.29.29.29
test-timeout = 5
internet-test-url = http://bing.com
proxy-test-url = http://bing.com

[Proxy]
{{proxies}}

[Proxy Group]
PROXY = select, AUTO, {{proxy_names}}
AUTO = url-test, {{proxy_names}}, url=http://www.gstatic.com/generate_204, interval=300

[Rule]
GEOIP,CN,DIRECT
FINAL,PROXY
