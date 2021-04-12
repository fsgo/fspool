/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/30
 */

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fsgo/fspool"
)

var pg fspool.ConnPoolGroup

var callURL = flag.String("url", "http://127.0.0.1:8088/cal/sum?ids=123,456,{index}", "")
var tryTotal = flag.Int("n", 1, "try total")

func main() {
	flag.Parse()

	for i := 0; i < *tryTotal; i++ {
		log.Println("index=", i)
		log.Println("before> ", pg.GroupStats().All)
		log.Println("NumGoroutine=", runtime.NumGoroutine())

		api := strings.ReplaceAll(*callURL, "{index}", strconv.Itoa(i))
		doQuery(api)

		log.Println("after> ", pg.GroupStats().All)
		log.Println("NumGoroutine=", runtime.NumGoroutine())
		fmt.Fprint(os.Stderr, "\n")
	}
}

func doQuery(callURL string) {
	ts := &http.Transport{
		// DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := pg.Get(ctx, addr)
			log.Println(connInfo("Transport Pool.Get", addr, conn, err))
			return conn, err
		},
	}
	defer ts.CloseIdleConnections()
	log.Println("Request:", callURL)
	req, _ := http.NewRequest("get", callURL, nil)
	// req.Close=true
	resp, err := ts.RoundTrip(req)
	if err != nil {
		log.Println("ERROR:", err)
	} else {
		bd, e2 := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		log.Println("SUCCESS:", resp.Status)
		log.Println("Resp Body:", string(bd), e2)
	}
}

func init() {
	opt := &fspool.Option{
		MaxOpen:     3,
		MaxIdle:     3,
		MaxLifeTime: time.Minute,
		MaxIdleTime: 30 * time.Second,
	}

	pg = fspool.NewConnPoolGroup(opt, func(key interface{}) fspool.NewConnFunc {
		addr := key.(string)
		return func(ctx context.Context) (net.Conn, error) {
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			log.Println(connInfo("PoolGroup net.Dial", addr, conn, err))
			return conn, err
		}
	})
}

func connInfo(prefix string, addr string, conn net.Conn, err error) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%-20s", prefix))
	buf.WriteString(" addr=")
	buf.WriteString(addr)
	buf.WriteString(", ")
	if err != nil {
		buf.WriteString("error=" + err.Error())
	} else {
		buf.WriteString("LocalAddr=" + conn.LocalAddr().String())
	}
	return buf.String()
}
