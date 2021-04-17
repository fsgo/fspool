/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/4/14
 */

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsgo/fspool"
)

var p fspool.ConnPool

var serverAddr = flag.String("addr", "127.0.0.1:8019", "server addr")
var optMaxOpen = flag.Int("max_open", 10, "MaxOpen")
var optMaxIdle = flag.Int("max_idle", 10, "MaxIdle")
var optMaxLifeTime = flag.Int("max_life_time", 60, "MaxLifeTime s")
var optMaxIdleTime = flag.Int("max_idle_time", 60, "MaxIdleTime s")
var workerTotal = flag.Int("worker", 10, "client worker total")

var workerStatus = map[int]time.Time{}
var mu sync.Mutex

func main() {
	flag.Parse()
	initPool()

	for i := 0; i < *workerTotal; i++ {
		go worker(i)
	}

	go printStatus()

	select {}
}

func initPool() {
	opt := &fspool.Option{
		MaxOpen:     *optMaxOpen,
		MaxIdle:     *optMaxIdle,
		MaxLifeTime: time.Duration(*optMaxLifeTime) * time.Second,
		MaxIdleTime: time.Duration(*optMaxIdleTime) * time.Second,
	}

	var totalConn int64

	p = fspool.NewConnPool(opt, func(ctx context.Context) (net.Conn, error) {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Second)
			defer cancel()
		}
		atomic.AddInt64(&totalConn, 1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", *serverAddr)
	})

	go func() {
		for range time.NewTicker(time.Minute).C {
			st := p.Stats()
			log.Println(
				"pool.Stats=", st,
				"totalConn=", atomic.LoadInt64(&totalConn),
			)
		}
	}()
}

func worker(id int) {
	var index int64

	var meta fspool.Meta

	plog := func(name string, err error) {
		log.Println(
			name,
			"workerID=", id,
			"indexID=", index,
			"err=", err,
			"meta", meta,
		)
	}

	do := func() {
		index++

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		conn, err := p.Get(ctx)
		if err != nil {
			plog("get_fail", err)
			return
		}
		meta = fspool.ReadMeta(conn)

		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(time.Minute))

		str := fmt.Sprintf("hello_%d", index)
		_, err = conn.Write(append([]byte(str), '\n'))
		if err != nil {
			plog("write_fail", err)
			return
		}

		rd := bufio.NewReader(conn)
		_, _, err = rd.ReadLine()

		if err != nil {
			plog("read_fail", err)
			return
		}

		if index%10000 == 9999 {
			plog("ok99", nil)
		}

		mu.Lock()
		workerStatus[id] = time.Now()
		mu.Unlock()
	}

	for {
		do()
	}
}

func printStatus() {
	for range time.NewTicker(30 * time.Second).C {
		mu.Lock()
		bf, _ := json.Marshal(workerStatus)
		mu.Unlock()
		log.Println("worker status:", string(bf))
	}
}
