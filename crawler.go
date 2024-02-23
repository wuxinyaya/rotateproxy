package rotateproxy

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var crawlDone = make(chan struct{}, 1)
var lockflag bool

type fofaAPIResponse struct {
	Err     bool       `json:"error"`
	Mode    string     `json:"mode"`
	Page    int        `json:"page"`
	Query   string     `json:"query"`
	Results [][]string `json:"results"`
	Size    int        `json:"size"`
}

func addProxyURL(url string) {
	CreateProxyURL(url)
}
func Crawlergithub(proxy string) {
	req, err := http.NewRequest("GET", "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt", nil)
	if err != nil {
		return
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
	}}
	if proxy != "" {
		proxyUrl, err := url.Parse(proxy)
		if err == nil { // 使用传入代理
			tr.Proxy = http.ProxyURL(proxyUrl)
		}
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	lines := strings.Split(string(body), "\n")
	InfoLog(Notice("获取 %d 个代理", len(lines)))
	for _, line := range lines {
		addProxyURL(fmt.Sprintf("socks5://%s", line))
	}
	proxies, err := QueryFirstProxyURL()
	if err != nil {
		ErrorLog(Warn("[!] query db error: %v", err))
	}
	checkAlive("https://www.google.com", "Copyright The Closure Library Authors", proxies)
	return
}
func Crawlerfofa(fofaApiKey, fofaEmail, rule string, pageNum int, proxy string) (bool, error) {
	req, err := http.NewRequest("GET", "https://fofa.info/api/v1/search/all", nil)
	if err != nil {
		return true, err
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
	}}
	if proxy != "" {
		proxyUrl, err := url.Parse(proxy)
		if err == nil { // 使用传入代理
			tr.Proxy = http.ProxyURL(proxyUrl)
		}
	}
	rule = base64.StdEncoding.EncodeToString([]byte(rule))
	q := req.URL.Query()
	q.Add("email", fofaEmail)
	q.Add("key", fofaApiKey)
	q.Add("qbase64", rule)
	q.Add("size", "100")
	q.Add("page", fmt.Sprintf("%d", pageNum))
	q.Add("fields", "host,title,ip,domain,port,country,city,server,protocol")
	req.URL.RawQuery = q.Encode()
	// resp, err := http.DefaultClient.Do(req)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return true, err
	}
	//InfoLog(Noticeln("start to parse proxy url from response"))
	defer resp.Body.Close()
	var res fofaAPIResponse
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return true, err
	}
	if len(res.Results) == 0 {
		return false, err
	}
	InfoLog(Notice("获取 %d 个代理", len(res.Results)))
	for _, value := range res.Results {
		host := value[0]
		addProxyURL(fmt.Sprintf("socks5://%s", host))
	}
	crawlDone <- struct{}{}
	return true, nil
}

func StartRunCrawler(ctx context.Context, fofaApiKey, fofaEmail, rule string, pageCount int, proxy string) {
	runCrawlerfofa := func() {
		for i := 1; i <= pageCount; i++ {
			flag, err := Crawlerfofa(fofaApiKey, fofaEmail, rule, i, proxy)
			if err != nil {
				ErrorLog(Warn("[!] error: %v", err))
			}
			if !flag {
				break
			}
		}
	}
	runCrawlergithub := func() {
		Crawlergithub(proxy)
	}
	go func() {
		lockflag = true
		runCrawlerfofa()
		InfoLog(Notice("从fofa获取代理服务器完成，15分钟后重新获取"))
		runCrawlergithub()
		InfoLog(Notice("从github获取代理服务器完成，15分钟后重新获取"))
		lockflag = false
		ticker := time.NewTicker(900 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				lockflag = true
				runCrawlerfofa()
				InfoLog(Notice("从fofa获取代理服务器完成，15分钟后重新获取"))
				runCrawlergithub()
				InfoLog(Notice("从github获取代理服务器完成，15分钟后重新获取"))
				lockflag = false
			case <-ctx.Done():
				//InfoLog(Notice("所有源代理服务器完成，60分钟后重新获取"))
				return
			}

		}
	}()
}
