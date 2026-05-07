package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cluster-tumbler/internal/config"
	"cluster-tumbler/internal/runtime"
)

type repeatedFlag []string

func (f *repeatedFlag) String() string  { return strings.Join(*f, ",") }
func (f *repeatedFlag) Set(v string) error { *f = append(*f, v); return nil }

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	var etcdEndpoints repeatedFlag
	flag.Var(&etcdEndpoints, "etcd", "etcd endpoint address:port (repeatable, overrides etcd.endpoints)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if len(etcdEndpoints) > 0 {
		cfg.Etcd.Endpoints = []string(etcdEndpoints)
	}

	rt, err := runtime.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime init error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rt.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		os.Exit(1)
	}
}
