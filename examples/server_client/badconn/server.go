// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/4

package main

import (
	"flag"
	"log"
	"net"
	"time"
)

var addr = flag.String("addr", ":9123", "server addr")

func main() {
	flag.Parse()
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalln(err.Error())
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatalln(err.Error())
		}
		go handler(conn)
	}
}

func handler(conn net.Conn) {
	log.Println("new conn", conn.RemoteAddr())
	defer conn.Close()
	time.Sleep(3 * time.Second)
	log.Println("conn close", conn.RemoteAddr())
}
