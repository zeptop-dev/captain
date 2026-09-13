# Captain default Stash template.
# {{proxy_names}} inside a proxy-group expands to every server name.
mode: rule
log-level: info
proxies: []
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - AUTO
      - "{{proxy_names}}"
  - name: AUTO
    type: url-test
    url: https://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - "{{proxy_names}}"
rules:
  - GEOIP,private,DIRECT,no-resolve
  - GEOSITE,cn,DIRECT
  - GEOIP,cn,DIRECT
  - MATCH,PROXY
