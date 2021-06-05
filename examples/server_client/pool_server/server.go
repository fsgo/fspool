// Copyright(C) 2021 github.com/hidu  All Rights Reserved.
// Author: hidu (duv123+git@baidu.com)
// Date: 2021/4/14

package main

import (
	"bufio"
	"bytes"
	"flag"
	"log"
	"net"
	"sync/atomic"
	"time"
)

var addr = flag.String("addr", ":8019", "server addr")

func main() {
	flag.Parse()
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Listen at %s failed: %v", *addr, err)
	}
	defer l.Close()

	log.Println("server start, Listen at", *addr)

	startServer(l)
	log.Println("server exit")
}

func startServer(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("l.Accept() err:", err)
			time.Sleep(time.Second)
			continue
		}
		go serverHandler(conn)
	}
}

var connTotal int64
var connID int64

func serverHandler(conn net.Conn) {
	atomic.AddInt64(&connTotal, 1)
	defer func() {
		atomic.AddInt64(&connTotal, -1)
	}()

	defer conn.Close()

	id := atomic.AddInt64(&connID, 1)

	rd := bufio.NewReader(conn)
	var err error
	var line []byte

	var loopID int64

	start := time.Now()

	printLog := func(name string) {
		log.Println(
			name,
			"connID=", id,
			"loopID=", loopID,
			"remote=", conn.RemoteAddr().String(),
			"start=", start.Format("01-02 15:04:05"),
			"cost=", time.Since(start),
			"err=", err,
			"conc=", atomic.LoadInt64(&connTotal),
		)
	}

	defer printLog("conn_close")

	for {
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		line, _, err = rd.ReadLine()
		if err != nil {
			break
		}
		out := bytes.ToUpper(line)
		_, err = conn.Write(append(out, '\n'))
		if err != nil {
			break
		}
		loopID++

		if loopID%10000 == 9999 {
			printLog("ok99")
		}
	}

}
