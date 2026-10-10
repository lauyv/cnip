package main

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

const (
	ipv4URL   = "https://raw.githubusercontent.com/misakaio/chnroutes2/master/chnroutes.txt"
	ipv6URL   = "https://gaoyifan.github.io/china-operator-ip/china6.txt"
	outputDir = "dist"
)

// lanCIDRs 是保留/私有地址段（对应 sing-box private、Clash GEOIP,private 的常用列表），
// 会单独以 iso_code=LAN 写入 MMDB，方便用 GEOIP,LAN 规则直接放行。
var lanCIDRs = []string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"172.16.0.0/12",
	"169.254.0.0/16",
	"192.0.0.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"255.255.255.255/32",
	"::/128",
	"::1/128",
	"::ffff:0:0/96",
	"64:ff9b::/96",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
}

type singBoxRuleSet struct {
	Version int             `json:"version"`
	Rules   []singBoxIPCIDR `json:"rules"`
}

type singBoxIPCIDR struct {
	IPCIDR []string `json:"ip_cidr"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "GeoLite2-Country",
		RecordSize:              24,
		IncludeReservedNetworks: true, // 允许写入 0.0.0.0/8、10.0.0.0/8 等保留/私有段
		DisableIPv4Aliasing:     true, // 允许写入 ::ffff:0:0/96 等 IPv4 映射段
	})
	if err != nil {
		return fmt.Errorf("创建 writer 失败: %w", err)
	}

	cnRecord := mmdbtype.Map{
		"country": mmdbtype.Map{
			"geoname_id":           mmdbtype.Uint32(1814991),
			"is_in_european_union": mmdbtype.Bool(false),
			"iso_code":             mmdbtype.String("CN"),
			"names": mmdbtype.Map{
				"de":    mmdbtype.String("China"),
				"en":    mmdbtype.String("China"),
				"es":    mmdbtype.String("China"),
				"fr":    mmdbtype.String("Chine"),
				"ja":    mmdbtype.String("中国"),
				"pt-BR": mmdbtype.String("China"),
				"ru":    mmdbtype.String("Китай"),
				"zh-CN": mmdbtype.String("中国"),
			},
		},
	}

	lanRecord := mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String("LAN"),
			"names": mmdbtype.Map{
				"en":    mmdbtype.String("Private Network"),
				"zh-CN": mmdbtype.String("局域网/私有网络"),
			},
		},
	}

	var allCIDRs []string
	for _, source := range []struct {
		url       string
		ipVersion int
	}{{ipv4URL, 4}, {ipv6URL, 6}} {
		cidrs, err := fetchAndInsert(writer, source.url, source.ipVersion, cnRecord)
		if err != nil {
			return fmt.Errorf("处理 %s 失败: %w", source.url, err)
		}
		fmt.Printf("%s: %d 条\n", source.url, len(cidrs))
		allCIDRs = append(allCIDRs, cidrs...)
	}

	lanCount, err := insertStaticCIDRs(writer, lanCIDRs, lanRecord)
	if err != nil {
		return fmt.Errorf("插入 LAN 地址失败: %w", err)
	}
	fmt.Printf("LAN/私有地址: %d 条（iso_code=LAN）\n", lanCount)

	if err := writeOutputs(outputDir, writer, allCIDRs); err != nil {
		return err
	}

	fmt.Printf("✅ chnroutes.mmdb（CN + LAN）+ chnroutes.txt + chnroutes.json，中国 %d 条、私有 %d 条\n", len(allCIDRs), lanCount)
	return nil
}

func fetchAndInsert(writer *mmdbwriter.Tree, url string, ipVersion int, value mmdbtype.DataType) ([]string, error) {
	fmt.Printf("⬇️  %s\n", url)

	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	cidrs, err := parseCIDRs(resp.Body, ipVersion)
	if err != nil {
		return nil, err
	}
	if _, err := insertStaticCIDRs(writer, cidrs, value); err != nil {
		return nil, err
	}
	return cidrs, nil
}

// parseCIDRs 校验地址族，规范化前缀，并按首次出现的顺序去重。
func parseCIDRs(r io.Reader, ipVersion int) ([]string, error) {
	if ipVersion != 4 && ipVersion != 6 {
		return nil, fmt.Errorf("不支持的 IP 版本: %d", ipVersion)
	}
	var cidrs []string
	seen := make(map[netip.Prefix]struct{})
	scanner := bufio.NewScanner(r)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行解析 CIDR %q 失败: %w", lineNumber, line, err)
		}
		if prefix.Addr().Is4In6() || prefix.Addr().Is4() != (ipVersion == 4) {
			return nil, fmt.Errorf("第 %d 行 CIDR %q 不是 IPv%d 地址段", lineNumber, line, ipVersion)
		}
		prefix = prefix.Masked()
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		cidrs = append(cidrs, prefix.String())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 CIDR 数据失败: %w", err)
	}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("IPv%d 数据源没有有效 CIDR", ipVersion)
	}
	return cidrs, nil
}

// insertStaticCIDRs 把内置的保留/私有地址段写入 MMDB。
func insertStaticCIDRs(writer *mmdbwriter.Tree, cidrs []string, value mmdbtype.DataType) (int, error) {
	for _, line := range cidrs {
		_, network, err := net.ParseCIDR(line)
		if err != nil {
			return 0, fmt.Errorf("解析 CIDR %q 失败: %w", line, err)
		}
		if err := writer.Insert(network, value); err != nil {
			return 0, fmt.Errorf("插入 %q 失败: %w", line, err)
		}
	}
	return len(cidrs), nil
}

func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "geoip-cn/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		closeBody(resp)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func closeBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		log.Printf("关闭响应体失败: %v", err)
	}
}

// writeOutputs 在同一文件系统的临时目录生成全部产物，成功后再替换 dist。
func writeOutputs(dir string, writer *mmdbwriter.Tree, cidrs []string) error {
	dir = filepath.Clean(dir)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("创建输出父目录失败: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-staging-*")
	if err != nil {
		return fmt.Errorf("创建临时输出目录失败: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil {
			log.Printf("清理临时输出目录 %s 失败: %v", staging, err)
		}
	}()

	if err := writeFile(filepath.Join(staging, "chnroutes.mmdb"), func(w io.Writer) error {
		_, err := writer.WriteTo(w)
		return err
	}); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(staging, "chnroutes.txt"), func(w io.Writer) error {
		buffer := bufio.NewWriter(w)
		for _, cidr := range cidrs {
			if _, err := fmt.Fprintln(buffer, cidr); err != nil {
				return err
			}
		}
		return buffer.Flush()
	}); err != nil {
		return err
	}
	if err := writeSingBoxJSON(filepath.Join(staging, "chnroutes.json"), cidrs); err != nil {
		return err
	}
	if err := os.Chmod(staging, 0755); err != nil {
		return fmt.Errorf("设置输出目录权限失败: %w", err)
	}
	return replaceOutputDir(staging, dir)
}

func writeSingBoxJSON(path string, cidrs []string) error {
	ruleSet := singBoxRuleSet{
		Version: 2,
		Rules: []singBoxIPCIDR{
			{IPCIDR: cidrs},
		},
	}
	data, err := json.Marshal(ruleSet)
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	return writeFile(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// replaceOutputDir 保留旧目录作为备份；替换失败时恢复旧产物。
func replaceOutputDir(staging, dir string) error {
	backup, err := os.MkdirTemp(filepath.Dir(dir), "."+filepath.Base(dir)+"-backup-*")
	if err != nil {
		return fmt.Errorf("创建备份目录失败: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("准备备份路径失败: %w", err)
	}
	hasBackup := false
	if err := os.Rename(dir, backup); err == nil {
		hasBackup = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("备份旧产物失败: %w", err)
	}
	if err := os.Rename(staging, dir); err != nil {
		if hasBackup {
			if restoreErr := os.Rename(backup, dir); restoreErr != nil {
				return errors.Join(
					fmt.Errorf("替换产物失败: %w", err),
					fmt.Errorf("恢复旧产物失败，备份保存在 %s: %w", backup, restoreErr),
				)
			}
		}
		return fmt.Errorf("替换产物失败: %w", err)
	}
	if hasBackup {
		if err := os.RemoveAll(backup); err != nil {
			log.Printf("产物已更新，清理备份 %s 失败: %v", backup, err)
		}
	}
	return nil
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建 %s 失败: %w", path, err)
	}
	if err := writeAndClose(f, write); err != nil {
		return fmt.Errorf("写入或关闭 %s 失败: %w", path, err)
	}
	return nil
}

// 即使写入失败也关闭文件，并保留写入和关闭两个阶段的错误。
func writeAndClose(w io.WriteCloser, write func(io.Writer) error) error {
	writeErr := write(w)
	return errors.Join(writeErr, w.Close())
}
