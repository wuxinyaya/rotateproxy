package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/akkuman/rotateproxy"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	baseCfg       rotateproxy.BaseConfig
	rule          string
	pageCount     int
	proxy         string
	checkURL      string
	checkURLwords string
	portPattern   = regexp.MustCompile(`^\d+$`)
)
var (
	email string
	token string
	mu    sync.Mutex // 保护email和token
	cond  = sync.NewCond(&mu)
)

var (
	client     *rotateproxy.RedirectClient // 全局变量
	clientLock sync.Mutex                  // 保护 client 实例的锁
)

func init() {
	flag.StringVar(&baseCfg.ListenAddr, "l", ":8899", "监听地址和端口")
	flag.StringVar(&baseCfg.Username, "user", "", "开启的socks5认证账号")
	flag.StringVar(&baseCfg.Password, "pass", "", "开启的socks5认证密码")
	//flag.StringVar(&email, "email", "", "fofa认证账号邮箱")
	//flag.StringVar(&token, "token", "", "fofa认证账号token")
	flag.StringVar(&proxy, "proxy", "", "访问fofa使用proxy")
	flag.IntVar(&pageCount, "page", 100, "爬取fofa页面数")
	flag.StringVar(&rule, "rule", fmt.Sprintf(`protocol=="socks5" && "Version:5 Method:No Authentication(0x00)" && after="%s" && country="CN"`, time.Now().AddDate(0, -3, 0).Format(time.DateOnly)), "search rule")
	flag.StringVar(&checkURL, "check", ``, "验证代理是翻墙")
	flag.StringVar(&checkURLwords, "checkWords", ``, "验证代理是否翻墙的关键字（即访问到URL里面包含制定关键字则判断代理可用）")
	flag.IntVar(&baseCfg.IPRegionFlag, "region", 0, "0: 全部 1: 不可翻墙 2: 可以翻墙")
	flag.IntVar(&baseCfg.SelectStrategy, "strategy", 1, "0: 随机选择（推荐）, 1: 选择最快的")
	flag.Parse()

	if checkURL != "https://www.google.com" && checkURLwords == "Copyright The Closure Library Authors" {
		fmt.Println("You set check url but forget to set `-checkWords`!")
		os.Exit(1)
	}

}

func handleRequests() {
	http.HandleFunc("/config/fofa", SetConfigFOFA)
	http.HandleFunc("/config/option", NewConfigOption)
	log.Fatal(http.ListenAndServe(":8888", nil))
}

func SetConfigFOFA(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()
	mu.Lock()
	defer mu.Unlock()
	email = queryValues.Get("email")
	token = queryValues.Get("token")
	if email != "" && token != "" {
		cond.Broadcast() // 通知等待条件的goroutine
	}
}
func NewConfigOption(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()

	IPRegionFlagStr := queryValues.Get("region")
	SelectStrategyStr := queryValues.Get("strategy")
	// 使用全局的 client 实例
	clientLock.Lock()
	defer clientLock.Unlock()
	newConfig := *client.GetConfig()
	if IPRegionFlagStr != "" {
		IPRegionFlag, err := strconv.Atoi(IPRegionFlagStr)
		if err != nil {
			// 错误处理
			http.Error(w, "参数错误", http.StatusBadRequest)
			return
		}
		newConfig.IPRegionFlag = IPRegionFlag
	}
	if SelectStrategyStr != "" {
		SelectStrategy, err := strconv.Atoi(SelectStrategyStr)
		if err != nil {
			// 错误处理
			http.Error(w, "参数错误", http.StatusBadRequest)
			return
		}
		newConfig.SelectStrategy = SelectStrategy
	}
	client.SetConfig(&newConfig)

}
func waitForEmailAndToken() {
	mu.Lock()
	defer mu.Unlock()
	for email == "" || token == "" {
		cond.Wait() // 等待email和token被设置
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func main() {
	//flag.Parse()
	go handleRequests()
	//rotateproxy.Crawlergithub("http://127.0.0.1:7890")
	waitForEmailAndToken()

	// print fofa query
	rotateproxy.InfoLog(rotateproxy.Info("You fofa query for rotateproxy is : %v", rule))
	rotateproxy.InfoLog(rotateproxy.Info("Check Proxy URL: %v", checkURL))
	rotateproxy.InfoLog(rotateproxy.Info("Check Proxy Words: %v", checkURLwords))

	baseCfg.ListenAddr = strings.TrimSpace(baseCfg.ListenAddr)

	if portPattern.Match([]byte(baseCfg.ListenAddr)) {
		baseCfg.ListenAddr = ":" + baseCfg.ListenAddr
	}

	c := make(chan os.Signal)
	// 监听信号
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	ctx := context.Background()
	rotateproxy.StartRunCrawler(ctx, token, email, rule, pageCount, proxy)
	rotateproxy.StartCheckProxyAlive(ctx, checkURL, checkURLwords)
	go func() {
		client = rotateproxy.NewRedirectClient(rotateproxy.WithConfig(&baseCfg))
		client.Serve(ctx)
	}()

	<-c
	err := rotateproxy.CloseDB()
	if err != nil {
		rotateproxy.ErrorLog(rotateproxy.Warn("Error closing db: %v", err))
		os.Exit(1)
	}
}
