# geoip-cn

每天自动生成的中国 IP 地址数据库，提供 MMDB、TXT、sing-box rule set 三种格式。

## 数据来源

| 类型      | 来源                                                                        |
| --------- | --------------------------------------------------------------------------- |
| IPv4 CIDR | [misakaio/chnroutes2](https://github.com/misakaio/chnroutes2)               |
| IPv6 CIDR | [gaoyifan/china-operator-ip](https://github.com/gaoyifan/china-operator-ip) |

## 输出格式

每次生成产出的文件：

| 文件             | 格式                               |
| ---------------- | ---------------------------------- |
| `chnroutes.mmdb` | MaxMind GeoIP2 MMDB（含 CN + LAN） |
| `chnroutes.txt`  | 纯文本 CIDR 列表                   |
| `chnroutes.srs`  | sing-box 编译后的 rule set         |

## 私有/局域网地址

MMDB 中除了中国地址（`GEOIP,CN`）外，还把常用的保留/私有地址段单独标记为
`LAN`（`GEOIP,LAN`），包括 `10.0.0.0/8`、`172.16.0.0/12`、
`192.168.0.0/16`、`fc00::/7`、`fe80::/10` 等。这些网段不会写入
`chnroutes.txt` / `chnroutes.srs`，避免污染纯中国 IP 列表。

使用示例：

```yaml
# Clash / mihomo
rules:
  - GEOIP,CN,DIRECT
  - GEOIP,LAN,DIRECT
```

> 提示：
> sing-box 可以用内置的 `ip_is_private` 规则, MMDB 里的 `LAN` 主要方便其他只按 MMDB 国家码匹配的客户端使用。

## 本地运行

```bash
# 前置要求：Go 1.27+
go mod download
go run main.go
```

输出文件会生成在当前目录下。

## 自动化更新

通过 GitHub Actions 每天自动更新：

- **定时触发**：北京时间每天 05:00（UTC 21:00）
- **手动触发**：在 Actions 页面点击 "Run workflow"

更新后自动 commit 并 push，无需手动操作。

## 项目结构

```
.
├── .github/workflows/generate.yml   # GitHub Actions 工作流
├── main.go                          # 生成器主程序
├── go.mod / go.sum                  # Go 依赖
├── chnroutes.mmdb                   # MaxMind MMDB 格式（自动生成）
├── chnroutes.txt                    # CIDR 纯文本（自动生成）
└── chnroutes.srs                    # sing-box rule set（自动生成）
```

## License

数据来源于公开项目，本仓库代码及生成数据可自由使用。
