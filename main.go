package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/adshao/go-binance/v2/futures"
	hook "github.com/robotn/gohook"
	"log"
	"net/http"
	"net/url"
)

var (
	client     *futures.Client
	httpClient *http.Client
	err        error
)

func init() {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	// 代理
	if config.Proxy != "" {
		proxyURL, err := url.Parse(config.Proxy)
		if err != nil {
			log.Fatal(err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		futures.SetWsProxyUrl(config.Proxy)
	}
	httpClient = &http.Client{Transport: transport}
	// 配置账号
	client = futures.NewClient(config.API, config.Secret)
	client.HTTPClient = httpClient
	// 时间偏移
	_, err = client.NewSetServerTimeService().Do(context.Background())
	if err != nil {
		log.Fatal(err)
	}

}

func main() {
	hook.Register(hook.KeyDown, []string{"q"}, func(e hook.Event) {
		fmt.Println("ctrl-shift-q")
		hook.End()
	})

	s := hook.Start()
	<-hook.Process(s)
}
