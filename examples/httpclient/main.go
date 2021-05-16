/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/30
 */

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fsgo/fspool"
	"github.com/fsgo/fspool/httptransport"
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
	ts := &httptransport.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			ad := fspool.NewAddr(network, addr)
			return pg.Get(ctx, ad)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			ad := fspool.NewAddr(network, addr)
			conn, err := pg.Get(ctx, ad)
			if err != nil {
				return nil, err
			}
			c := &tls.Config{
				InsecureSkipVerify: true,
			}
			return tls.Client(conn, c), nil
		},
		Proxy: func(request *http.Request) (*url.URL, error) {
			return url.Parse("http://127.0.0.1:3128/")
		},
	}
	log.Println("Request:", callURL)
	req, _ := http.NewRequest("GET", callURL, nil)
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

	pg = fspool.NewConnPoolGroup(opt, func(addr net.Addr) fspool.NewConnFunc {
		return func(ctx context.Context) (net.Conn, error) {
			conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
			log.Println(connInfo("PoolGroup net.Dial", addr.String(), conn, err))
			return conn, err
		}
	})
}

func connInfo(prefix string, addr string, conn net.Conn, err error) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%-20s", prefix))
	buf.WriteString(" remoteAddr=")
	buf.WriteString(addr)
	buf.WriteString(", ")
	if err != nil {
		buf.WriteString("error=" + err.Error())
	} else {
		buf.WriteString("localAddr=" + conn.LocalAddr().String())
	}
	return buf.String()
}
