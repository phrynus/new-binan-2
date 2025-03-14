package main

import (
	"encoding/json"
	"log"
	"os"
)

var config Config

type Config struct {
	API       string   `json:"api"`
	Secret    string   `json:"secret"`
	Proxy     string   `json:"proxy"`     // 代理
	Debug     bool     `json:"debug"`     // 是否开启调试模式
	Num       float64  `json:"num"`       // 额度
	TimeA     int64    `json:"timeA"`     // 快进快出
	TimeB     int64    `json:"timeB"`     // 强制平
	Quantity  float64  `json:"quantity"`  // 数量
	Blacklist []string `json:"blacklist"` // 黑名单
}

func init() {
	b, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	err = json.Unmarshal(b, &config)
	if err != nil {
		log.Fatal(err)
	}
}
