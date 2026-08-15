package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"task015-bloom/internal/bloom"
	"task015-bloom/internal/httpapi"
	"task015-bloom/internal/selfcheck"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--smoke-test" {
		os.Exit(selfcheck.Run())
	}

	sub := "server"
	if len(args) > 0 {
		sub = args[0]
	}
	if sub != "server" {
		fmt.Fprintf(os.Stderr, "未知命令 %q\n用法: task015-bloom [server|--smoke-test]\n", sub)
		os.Exit(2)
	}

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	addr := ":8080"
	capacity := uint64(0)
	fpRateStr := ""
	fs.StringVar(&addr, "addr", ":8080", "HTTP 监听地址")
	fs.Uint64Var(&capacity, "capacity", 0, "设计期望容量（正整数，必填）")
	fs.StringVar(&fpRateStr, "fp-rate", "", "设计期望误判率（严格介于 0 与 1，必填）")
	if err := fs.Parse(args[1:]); err != nil {
		os.Exit(2)
	}

	filter, err := buildFilter(capacity, fpRateStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置非法:", err)
		os.Exit(2)
	}

	api := httpapi.New(filter)
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
	}
	s := filter.Stats()
	log.Printf("布隆过滤器服务已启动，监听 %s，容量=%d 误判率=%g m=%d k=%d", addr, s.Capacity, s.FPRate, s.M, s.K)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "服务启动失败:", err)
		os.Exit(1)
	}
}

// buildFilter 校验启动参数并构造过滤器：capacity 必须为正整数，fpRate 必须严格介于 0 与 1。
func buildFilter(capacity uint64, fpRateStr string) (*bloom.Filter, error) {
	if capacity == 0 {
		return nil, fmt.Errorf("缺少必填参数 --capacity（正整数）")
	}
	if fpRateStr == "" {
		return nil, fmt.Errorf("缺少必填参数 --fp-rate（0<p<1）")
	}
	fpRate, err := strconv.ParseFloat(fpRateStr, 64)
	if err != nil {
		return nil, fmt.Errorf("--fp-rate 不是合法浮点数: %w", err)
	}
	f, err := bloom.New(capacity, fpRate)
	if err != nil {
		return nil, err
	}
	return f, nil
}
