package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

var (
	client      *futures.Client
	httpClient  *http.Client
	symbolsInfo []futures.Symbol
	err         error
	listenKey   string
	Osymbol     []string
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
	// 获取交易信息
	info, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	// 赛选币种
	for _, s := range info.Symbols {
		if s.QuoteAsset == "USDT" && s.ContractType == "PERPETUAL" && s.Status == "TRADING" {
			symbolsInfo = append(symbolsInfo, s)
		}
	}
	// listenKey
	listenKey, err = client.NewStartUserStreamService().Do(context.Background())
	if err != nil {
		log.Fatal(err)
	}

}

func main() {
	// 启动ws
	go wsUserGo()
	go wsUserGo2()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	<-sigC
	if config.Debug {
		log.Println("收到退出信号，正在关闭...")
	}
}

func wsUserGo() {
	retryCount := 0
	for {
		doneC, _, err := futures.WsAllLiquidationOrderServe(signalHandler, func(err error) {
			time.Sleep(time.Duration(2<<retryCount) * time.Second)
		})
		if err != nil {
			if config.Debug {
				log.Println("WsUserGo 重试中...")
			}
			time.Sleep(time.Second * 5) // 初始等待 5s 再重试
			continue
		}
		retryCount = 0
		log.Println("开始监听")
		if doneC != nil {
			<-doneC
		} else {
			log.Println("Error: doneC is nil")
		}
	}
}

func signalHandler(event *futures.WsLiquidationOrderEvent) {
	origQuantity, err := strconv.ParseFloat(event.LiquidationOrder.OrigQuantity, 64)
	if err != nil && origQuantity == 0 {
		return
	}
	// 平均价格
	avgPrice, err := strconv.ParseFloat(event.LiquidationOrder.AvgPrice, 64)
	if err != nil && avgPrice == 0 {
		return
	}
	if origQuantity*avgPrice > 5000 {

		// 取订单铺
		book, ree := client.NewDepthService().Symbol(event.LiquidationOrder.Symbol).Limit(5).Do(context.Background())
		if ree != nil {
			log.Println(ree)
			return
		}

		var (
			side         futures.SideType         = "BUY"
			positionSide futures.PositionSideType = "LONG"
			priceGo      string                   = book.Bids[0].Price
		)
		if event.LiquidationOrder.Side == "BUY" {
			side = "SELL"
			positionSide = "SHORT"
			priceGo = book.Asks[0].Price
		}

		log.Println(origQuantity*avgPrice, event.LiquidationOrder)

		_, quantityGo, err := processSymbolInfo(event.LiquidationOrder.Symbol, avgPrice, 20/avgPrice)
		if err != nil {
			log.Println(err)
			return
		}

		go order(event.LiquidationOrder.Symbol, side, positionSide, quantityGo, priceGo)

	}
}

// 处理币种数量和价格
func processSymbolInfo(symbol string, p float64, q float64) (price string, quantity string, err error) {
	var symbolInfo *futures.Symbol
	for _, s := range symbolsInfo {
		if s.Symbol == symbol {
			symbolInfo = &s
		}
	}
	if symbolInfo == nil {
		return "", "", errors.New("symbolInfo is nil")
	}
	// if config.Debug {
	// 	log.Println(symbolInfo)
	// }
	if q != 0 {
		quantity, err = takeDivisible(q, symbolInfo.Filters[1]["stepSize"].(string))
		if err != nil {
			return "", "", err
		}
	}
	if p != 0 {
		price, err = takeDivisible(p, symbolInfo.Filters[0]["tickSize"].(string))
		if err != nil {
			return "", "", err
		}
	}

	return price, quantity, nil
}

// 调整小数位数并确保可以整除
func takeDivisible(inputVal float64, divisor string) (string, error) {

	divisorVal, err := strconv.ParseFloat(divisor, 64)
	if err != nil || divisorVal == 0 {
		return "", fmt.Errorf("无效的 divisor: %v", err)
	}

	// 计算小数点位数
	decimalPlaces := 0
	if dot := strings.Index(divisor, "."); dot != -1 {
		decimalPlaces = len(divisor) - dot - 1
	}

	// 计算最大整除的值
	quotient := int(inputVal / divisorVal)
	maxDivisible := divisorVal * float64(quotient)

	// 格式化输出
	format := fmt.Sprintf("%%.%df", decimalPlaces)
	return fmt.Sprintf(format, maxDivisible), nil
}

func order(symbol string, side futures.SideType, positionSide futures.PositionSideType, quantityGo string, priceGo string) {
	isSymbol := contains(Osymbol, symbol)
	if isSymbol {
		return
	}
	o, err := client.NewCreateOrderService().
		Symbol(symbol).
		Side(side).
		PositionSide(positionSide).
		Type("LIMIT").
		TimeInForce("GTC").
		Quantity(quantityGo).
		Price(priceGo).
		Do(context.Background())
	if err != nil {
		return
	}
	// Osymbol 加入symbol
	Osymbol = append(Osymbol, symbol)
	log.Println(Osymbol)
	// 延迟3秒
	time.Sleep(2 * time.Second)
	// 查看订单状态o
	orderStatus, err := client.NewGetOrderService().Symbol(symbol).OrderID(o.OrderID).Do(context.Background())
	if err != nil {
		return
	}
	if orderStatus.Status != "FILLED" {
		// 取消订单(symbol, o.OrderID)
		_, err := client.NewCancelOrderService().Symbol(symbol).OrderID(o.OrderID).Do(context.Background())
		if err != nil {
			return
		}
		// 删除symbol
		Osymbol = remove(Osymbol, symbol)
	}
	time.Sleep(120 * time.Second)
	// 平掉 symbolBUY
	if side == "BUY" {
		side = "SELL"
	}
	_, err = client.NewCreateOrderService().Symbol(symbol).Side(side).Quantity(quantityGo).PositionSide(positionSide).Type("MARKET").Do(context.Background())
	if err != nil {
		log.Println(err)
		return
	}
}

func wsUserGo2() {
	retryCount := 0
	for {
		doneC, _, err := futures.WsUserDataServe(listenKey, func(event *futures.WsUserDataEvent) {
			if event.Event == "ORDER_TRADE_UPDATE" {
				dataOrder := event.OrderTradeUpdate
				if dataOrder.ExecutionType == "TRADE" && dataOrder.Status == "FILLED" {
					orderMapping := map[bool]map[futures.SideType]int{
						false: {"BUY": 1, "SELL": 3},
						true:  {"SELL": 2, "BUY": 4},
					}
					// 1 多下单 2 多平单 3 空下单 4 空平单
					orderStatus := orderMapping[dataOrder.IsReduceOnly][dataOrder.Side]
					if orderStatus == 1 || orderStatus == 3 {
						endPrice, err := strconv.ParseFloat(dataOrder.LastFilledPrice, 64)
						if err != nil || endPrice == 0 {
							return
						}
						if orderStatus == 1 {
							endPrice = endPrice * (1 + 0.001)
						} else {
							endPrice = endPrice * (1 - 0.001)
						}
						priceGoSub, _, err := processSymbolInfo(dataOrder.Symbol, endPrice, 0)
						if err != nil {
							log.Println(err)
							return
						}
						var side futures.SideType = "BUY"
						if dataOrder.Side == "BUY" {
							side = "SELL"
						}
						_, err = client.NewCreateOrderService().
							Symbol(dataOrder.Symbol).
							Side(side).
							PositionSide(dataOrder.PositionSide).
							Type("LIMIT").
							TimeInForce("GTC").
							Quantity(dataOrder.AccumulatedFilledQty).
							Price(priceGoSub).
							Do(context.Background())
						if err != nil {
							log.Println(err)
							return
						}
					}
					if orderStatus == 2 || orderStatus == 4 {
						// 删除symbol
						Osymbol = remove(Osymbol, dataOrder.Symbol)
					}
				}
			}
		}, func(err error) {
			time.Sleep(time.Duration(2<<retryCount) * time.Second)
		})
		if err != nil {
			if config.Debug {
				log.Println("WsUserGo 重试中...")
			}
			time.Sleep(time.Second * 5) // 初始等待 5s 再重试
			continue
		}
		retryCount = 0
		log.Println("开始监听")
		if doneC != nil {
			<-doneC
		} else {
			log.Println("Error: doneC is nil")
		}
	}
}
func contains(list []string, item string) bool {
	for i := range list {
		if list[i] == item {
			return true
		}
	}
	return false
}

func remove(list []string, item string) []string {
	for i := range list {
		if list[i] == item {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	return list
}
