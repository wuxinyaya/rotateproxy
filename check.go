package rotateproxy

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type IPInfo struct {
	Status      string  `json:"status"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Zip         string  `json:"zip"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	Isp         string  `json:"isp"`
	Org         string  `json:"org"`
	As          string  `json:"as"`
	Query       string  `json:"query"`
}

func CheckProxyAlive(proxyURL string) (respBody string, timeout int64, avail bool) {
	proxy, _ := url.Parse(proxyURL)
	httpclient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxy),
			//校验https，防止部分代理嗅探流量，可能会导致可用代理较少，不介意可以取消注释
			//TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
		// shorter timeout for better proxies
		Timeout: 5 * time.Second,
	}
	startTime := time.Now()

	resp, err := httpclient.Get("https://searchplugin.csdn.net/api/v1/ip/get?ip=")

	if err != nil {
		return "", 0, false
	}
	defer resp.Body.Close()
	timeout = int64(time.Since(startTime))
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, false
	}
	if !strings.Contains(string(body), `"address"`) {
		return "", 0, false
	}
	return string(body), timeout, true
}

func CheckProxyWithCheckURL(proxyURL string, checkURL string, checkURLwords string) (timeout int64, avail bool) {
	// InfoLog(Notice("check %s： %s", proxyURL, checkURL))
	proxy, _ := url.Parse(proxyURL)
	httpclient := &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxy),
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
		Timeout: 20 * time.Second,
	}
	startTime := time.Now()
	resp, err := httpclient.Get(checkURL)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	timeout = int64(time.Since(startTime))
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		return 0, false
	}

	// TODO: support regex
	if resp.StatusCode != 200 {
		return 0, false
	}

	if !strings.Contains(string(body), checkURLwords) {
		return 0, false
	}

	return timeout, true
}

func StartCheckProxyAlive(ctx context.Context, checkURL string, checkURLwords string) {
	go func() {
		ticker := time.NewTicker(1800 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-crawlDone:
				//InfoLog(Noticeln("Checkings"))
				proxies, err := QueryFirstProxyURL()
				if err != nil {
					ErrorLog(Warn("[!] query db error: %v", err))
				}
				checkAlive(checkURL, checkURLwords, proxies)
				//InfoLog(Noticeln("Check done"))
			case <-ticker.C:
				InfoLog(Noticeln("开始定期检测代理有效性"))
				proxies, err := QueryProxyURL()
				InfoLog(Notice("当前缓存中共有: %v 个代理", len(proxies)))
				if err != nil {
					ErrorLog(Warn("[!] query db error: %v", err))
				}
				checkAlive(checkURL, checkURLwords, proxies)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func checkAlive(checkURL string, checkURLwords string, proxies []ProxyURL) {
	//proxies, err := QueryProxyURL()
	//if err != nil {
	//	ErrorLog(Warn("[!] query db error: %v", err))
	//}
	var wg sync.WaitGroup           // 声明 WaitGroup
	sem := make(chan struct{}, 100) //maxGoroutines为最大线程
	for i := range proxies {
		proxy := proxies[i]
		wg.Add(1)
		sem <- struct{}{} // 获取信号量的一个槽位
		go func() {
			defer wg.Done() // 协程结束时减少计数
			defer func() { <-sem }()
			SetProxyURLUnFirst(proxy.URL)
			respBody, timeout, avail := CheckProxyAlive(proxy.URL)
			if avail {
				if checkURL != "" {
					timeout, GFWbypass := CheckProxyWithCheckURL(proxy.URL, checkURL, checkURLwords)
					if GFWbypass {
						InfoLog(Notice("%v 可用（过墙）", proxy.URL))
						SetProxyURLAvail(proxy.URL, timeout, CanBypassGFW(respBody))
						return
					}
				}
				InfoLog(Notice("%v 可用", proxy.URL))
				SetProxyURLAvail(proxy.URL, timeout, CanBypassGFW(respBody))
				return
			}
			AddProxyURLRetry(proxy.URL)
		}()
	}
	Availproxies, err := QueryAvailProxyURL(2)
	if err != nil {
		return
	}
	AvailCNproxies, err := QueryAvailProxyURL(1)
	if err != nil {
		return
	}
	InfoLog(Notice("当前拥有 %d 个国内有效代理，%d个国外有效代理", len(AvailCNproxies), len(Availproxies)))
}

// 修改逻辑，所有非中国的IP都判断为可以过GFW（主要用于区别国内IP和国外IP）
func CanBypassGFW(respBody string) bool {
	return !strings.Contains(respBody, "中国")
}
