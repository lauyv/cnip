package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

const (
	ipv4URL = "https://raw.githubusercontent.com/misakaio/chnroutes2/master/chnroutes.txt"
	ipv6URL = "https://gaoyifan.github.io/china-operator-ip/china6.txt"
)

func main() {

	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-Country",
		RecordSize:   24,
	})
	if err != nil {
		log.Fatalf("创建 writer 失败: %v", err)
	}

	record := mmdbtype.Map{
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

	var allCIDRs []string
	for _, url := range []string{ipv4URL, ipv6URL} {
		cidrs, err := fetchAndInsert(writer, url, record)
		if err != nil {
			log.Fatalf("处理 %s 失败: %v", url, err)
		}
		fmt.Printf("%s: %d 条\n", url, len(cidrs))
		allCIDRs = append(allCIDRs, cidrs...)
	}

	// 写入 MMDB
	mmdbPath := "chnroutes.mmdb"
	mmdbFile, err := os.Create(mmdbPath)
	if err != nil {
		log.Fatalf("创建 %s 失败: %v", mmdbPath, err)
	}
	defer closeFile(mmdbFile)

	if _, err := writer.WriteTo(mmdbFile); err != nil {
		log.Fatalf("写入 %s 失败: %v", mmdbPath, err)
	}

	// 写入合并 txt
	txtPath := "chnroutes.txt"
	txtFile, err := os.Create(txtPath)
	if err != nil {
		log.Fatalf("创建 %s 失败: %v", txtPath, err)
	}
	defer closeFile(txtFile)

	w := bufio.NewWriter(txtFile)
	for _, cidr := range allCIDRs {
		if _, err := fmt.Fprintln(w, cidr); err != nil {
			log.Fatalf("写入 %s 失败: %v", txtPath, err)
		}
	}
	if err := w.Flush(); err != nil {
		log.Fatalf("刷新 %s 失败: %v", txtPath, err)
	}

	// 写入 sing-box rule set JSON
	if err := writeSingBoxJSON("chnroutes.json", allCIDRs); err != nil {
		log.Fatalf("写入 sing-box JSON 失败: %v", err)
	}

	fmt.Printf("✅ chnroutes.mmdb + chnroutes.txt + chnroutes.json，共 %d 条 CIDR\n", len(allCIDRs))
}

func fetchAndInsert(writer *mmdbwriter.Tree, url string, value mmdbtype.DataType) ([]string, error) {
	fmt.Printf("⬇️  %s\n", url)

	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var cidrs []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, network, err := net.ParseCIDR(line)
		if err != nil {
			return cidrs, fmt.Errorf("解析 CIDR %q 失败: %w", line, err)
		}
		if err := writer.Insert(network, value); err != nil {
			return cidrs, fmt.Errorf("插入 %q 失败: %w", line, err)
		}
		cidrs = append(cidrs, line)
	}
	return cidrs, scanner.Err()
}

func httpGet(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "geoip-cn/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		closeBody(resp)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func closeFile(f *os.File) {
	if err := f.Close(); err != nil {
		log.Printf("关闭文件失败: %v", err)
	}
}

type singBoxRuleSet struct {
	Version int             `json:"version"`
	Rules   []singBoxIPCIDR `json:"rules"`
}

type singBoxIPCIDR struct {
	IPCIDR []string `json:"ip_cidr"`
}

func writeSingBoxJSON(path string, cidrs []string) error {
	ruleSet := singBoxRuleSet{
		Version: 2,
		Rules: []singBoxIPCIDR{
			{IPCIDR: cidrs},
		},
	}
	data, err := json.MarshalIndent(ruleSet, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return nil
}

func closeBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		log.Printf("关闭响应体失败: %v", err)
	}
}
