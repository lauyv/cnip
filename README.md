# geoip-cn

每天自动生成并发布的中国 IP 地址数据库，提供 MMDB、TXT、sing-box rule set、V2Ray DAT 四种格式。

## 数据来源

| 类型                    | 来源                                                                                                |
| ----------------------- | --------------------------------------------------------------------------------------------------- |
| IPv4 CIDR               | [misakaio/chnroutes2](https://github.com/misakaio/chnroutes2)                                       |
| IPv6 CIDR               | [gaoyifan/china-operator-ip](https://github.com/gaoyifan/china-operator-ip)                         |
| Netflix IPv4/IPv6（NF） | [Loyalsoldier/geoip](https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/netflix.txt) |

## 发布

GitHub Actions 每天北京时间 20:00 构建一次，并在 `main` 更新时自动构建。源码保存在
`main` 分支，产物发布到 `rel` 分支；该分支每次发布都会重建，只保留最新结果。

| 文件             | 格式                                                   |
| ---------------- | ------------------------------------------------------ |
| `chnroutes.mmdb` | MaxMind GeoIP2 MMDB（含 CN + NF + LAN）                |
| `chnroutes.dat`  | V2Ray GeoIPList protobuf（含 CN + NF + LAN + PRIVATE） |
| `chnroutes.txt`  | 纯文本 CIDR 列表                                       |
| `chnroutes.srs`  | 中国地址 sing-box 编译后的 rule set                    |
| `netflix.txt`    | Netflix IPv4/IPv6 CIDR 列表                            |
| `netflix.srs`    | Netflix sing-box 编译后的 rule set                     |

下载地址格式：

```text
https://github.com/lauyv/cnip/raw/rel/<文件名>
```

例如：

```text
https://github.com/lauyv/cnip/raw/rel/chnroutes.mmdb
https://github.com/lauyv/cnip/raw/rel/chnroutes.dat
https://github.com/lauyv/cnip/raw/rel/chnroutes.txt
https://github.com/lauyv/cnip/raw/rel/chnroutes.srs
https://github.com/lauyv/cnip/raw/rel/netflix.txt
https://github.com/lauyv/cnip/raw/rel/netflix.srs
```

## V2Ray 地址库

`chnroutes.dat` 使用 [V2Ray GeoIPList protobuf 格式](https://github.com/v2fly/v2ray-core/blob/master/app/router/routercommon/common.proto)，包含中国地址 `CN`、Netflix 地址 `NF`、私有/保留地址 `LAN` 和内容相同的别名 `PRIVATE`，各分组均包含对应的 IPv4/IPv6 地址段。

将文件放入 V2Ray 资源目录后，可在路由规则的 `ip` 字段使用 `ext:chnroutes.dat:cn`、`ext:chnroutes.dat:nf`、`ext:chnroutes.dat:lan` 或 `ext:chnroutes.dat:private`。各分组独立保留原始网段，重叠地址可同时属于多个分组，匹配结果由路由规则顺序决定。

## Netflix 地址

Netflix 上游的 IPv4/IPv6 地址段在 MMDB 中标记为 `NF`，可用
`GEOIP,NF` 匹配。写入顺序为 NF → CN → LAN，重叠地址优先标记为 LAN，其次为 CN。
同时独立生成 `netflix.txt` 和 `netflix.srs`。Netflix 地址不会加入仅包含中国地址的 `chnroutes.txt` / `chnroutes.srs`。

## 私有/局域网地址

MMDB 中除了中国地址（`GEOIP,CN`）外，还把常用的保留/私有地址段单独标记为
`LAN`（`GEOIP,LAN`），包括 `10.0.0.0/8`、`172.16.0.0/12`、
`192.168.0.0/16`、`fc00::/7`、`fe80::/10` 等。这些网段不会写入
`chnroutes.txt` / `chnroutes.srs`，避免污染纯中国 IP 列表。

使用示例：

```yaml
# Clash / mihomo
rules:
  - GEOIP,NF,PROXY
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

输出文件会生成在 `dist/` 目录下。`chnroutes.json` 和 `netflix.json` 是编译 SRS 时使用的中间文件；GitHub Actions 会将它们编译为对应的 `.srs` 后发布。

## 自动化更新

通过 GitHub Actions 每天自动更新：

- **定时触发**：北京时间每天 20:00（`Asia/Shanghai`，UTC 12:00）
- **源码更新**：`main` 分支有新提交时
- **手动触发**：在 Actions 页面点击 "Run workflow"

构建完成后会强制更新独立的 `rel` 分支，无需手动操作。

## 项目结构

```
.
├── .github/workflows/generate.yml   # GitHub Actions 工作流
├── main.go                          # 生成器主程序
└── go.mod / go.sum                  # Go 依赖
```

## License

数据来源于公开项目，本仓库代码及生成数据可自由使用。
