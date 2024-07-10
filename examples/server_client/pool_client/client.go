// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/4/14

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsgo/fspool"
)

var pool *fspool.ConnPool

var serverAddr = flag.String("server", "127.0.0.1:8019", "server addr")
var lAddr = flag.String("addr", "0.0.0.0:8020", "http server for client status api")
var optMaxOpen = flag.Int("max_open", 10, "MaxOpen")
var optMaxIdle = flag.Int("max_idle", 10, "MaxIdle")
var connectTimeout = flag.Int("ctimeout", 10, "connect timeout, ms")
var optMaxLifeTime = flag.Int("max_life_time", 30, "MaxLifeTime, second")
var optMaxIdleTime = flag.Int("max_idle_time", 30, "MaxIdleTime, second")
var workerTotal = flag.Int("worker", 11, "client worker total")

var workerStatus = map[int]time.Time{}
var mu sync.Mutex

func main() {
	flag.Parse()
	initPool()

	for i := 0; i < *workerTotal; i++ {
		go worker(i)
	}

	go printStatus()

	startHTTPServer()

	select {}
}

func startHTTPServer() {
	l, err := net.Listen("tcp", *lAddr)
	if err != nil {
		log.Fatalln(err.Error())
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{
			"Pool": pool.Stats(),
		}
		bf, err := json.Marshal(info)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(bf)
	})
	go http.Serve(l, h)
}

func initPool() {
	opt := &fspool.Option{
		MaxOpen:     *optMaxOpen,
		MaxIdle:     *optMaxIdle,
		MaxLifeTime: time.Duration(*optMaxLifeTime) * time.Second,
		MaxIdleTime: time.Duration(*optMaxIdleTime) * time.Second,
	}

	var totalConn int64

	pool = fspool.NewConnPool(opt, func(ctx context.Context) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Millisecond*time.Duration(*connectTimeout))
		defer cancel()
		atomic.AddInt64(&totalConn, 1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", *serverAddr)
	})

	go func() {
		for range time.NewTicker(time.Minute).C {
			st := pool.Stats()
			log.Println(
				"pool.Stats=", st,
				"totalConn=", atomic.LoadInt64(&totalConn),
			)
		}
	}()
}

func worker(id int) {
	var index int64

	var meta *fspool.Meta

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

		conn, err := pool.Get(ctx)
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
