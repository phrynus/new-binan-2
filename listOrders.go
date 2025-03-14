package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// 获取单个币种的交易记录
func getAccountTradesForSymbol(client *futures.Client, symbol string, startTime, endTime int64, wg *sync.WaitGroup, tradesChan chan<- []*futures.AccountTrade, limiter <-chan time.Time) {
	defer wg.Done()

	// 等待令牌，确保每秒不超过20个请求
	<-limiter

	// 获取交易记录数据
	trades, err := client.NewListAccountTradeService().
		Symbol(symbol).
		StartTime(startTime).
		EndTime(endTime).
		Do(context.Background())

	if err != nil {
		log.Printf("获取 %s 交易记录失败: %v\n", symbol, err)
		tradesChan <- nil
		return
	}

	log.Printf("获取 %s 交易记录成功\n", symbol)

	// 返回交易记录数据
	tradesChan <- trades
}

// 获取所有币种的交易记录（并行，且每秒限制20个请求）
func getAccountTrades(client *futures.Client, days int) ([]*futures.AccountTrade, error) {
	exchangeInfo, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	// 获取时间范围
	startTime := time.Now().AddDate(0, 0, -days).UnixMilli()
	endTime := time.Now().UnixMilli()

	var wg sync.WaitGroup
	tradesChan := make(chan []*futures.AccountTrade, len(exchangeInfo.Symbols)) // 并行获取交易记录的通道

	// 创建速率限制器，限制每秒 20 个请求
	limiter := time.NewTicker(time.Second / 20).C

	// 启动 Goroutines 并发获取所有币种的交易记录
	for _, symbolInfo := range exchangeInfo.Symbols {
		wg.Add(1)
		go getAccountTradesForSymbol(client, symbolInfo.Symbol, startTime, endTime, &wg, tradesChan, limiter)
	}

	// 等待所有 Goroutines 完成
	wg.Wait()
	close(tradesChan)

	// 合并所有交易记录
	var allTrades []*futures.AccountTrade
	for trades := range tradesChan {
		if trades != nil {
			allTrades = append(allTrades, trades...)
		}
	}

	// 按时间（Time）排序
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Time > allTrades[j].Time
	})

	return allTrades, nil
}

// 导出到 CSV
func exportToCSV(trades []*futures.AccountTrade, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入 UTF-8 BOM
	// UTF-8 BOM: 0xEF, 0xBB, 0xBF
	_, err = file.Write([]byte{0xEF, 0xBB, 0xBF})
	if err != nil {
		log.Println("写入 BOM 时出错:", err)
		return err
	}

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 表头
	header := []string{"币种", "持仓方向", "价格", "成交数量", "盈亏", "手续费", "净利润", "时间"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// 写入数据
	for _, trade := range trades {
		if trade.RealizedPnl == "0" {
			// 忽略没有盈亏的交易记录
			continue
		}
		pnl, _ := strconv.ParseFloat(trade.RealizedPnl, 64)
		commission, _ := strconv.ParseFloat(trade.Commission, 64)
		row := []string{
			trade.Symbol,
			string(trade.PositionSide),
			trade.Price,
			trade.Quantity,
			fmt.Sprintf("%.7f", pnl),
			fmt.Sprintf("%.7f", commission*2),
			fmt.Sprintf("%.7f", pnl-commission*2),
			time.UnixMilli(trade.Time).Format("2006-01-02 15:04:05"),
		}

		writer.Write(row)
	}

	log.Println("CSV 文件已生成:", filename)
	return nil
}

func AccountTrades() {
	client := futures.NewClient(client.APIKey, client.SecretKey)

	// 获取交易记录
	trades, err := getAccountTrades(client, 1) // 获取最近 1 天的交易记录
	if err != nil {
		fmt.Println("获取交易记录失败:", err)
		return
	}
	fmt.Println("交易记录数:", len(trades))
	// 导出 CSV
	exportToCSV(trades, "account_trades.csv")
}
