/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/23
 */

package fspool

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type testConnServer struct {
	onAcceptErr func(err error)
	connecting  int32
}

func (ts *testConnServer) Serve(ctx context.Context, l net.Listener) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := l.Accept()
		if err != nil {
			ts.onAcceptErr(err)
			continue
		}
		go ts.handler(conn)
	}
}

func (ts *testConnServer) handler(conn net.Conn) {
	atomic.AddInt32(&ts.connecting, 1)
	defer func() {
		atomic.AddInt32(&ts.connecting, -1)
	}()
	rd := bufio.NewReader(conn)
	for {
		line, _, err := rd.ReadLine()
		if err != nil {
			return
		}
		_, _ = conn.Write(line)
		_, _ = conn.Write([]byte("\n"))
	}
}

func (ts *testConnServer) getConnecting() int {
	val := atomic.LoadInt32(&ts.connecting)
	return int(val)
}

func TestNewConnPool(t *testing.T) {
	ts := &testConnServer{
		onAcceptErr: func(err error) {},
		connecting:  0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Fatalf("Listen error %v", errListen)
	}

	go func() {
		_ = ts.Serve(ctx, l)
	}()

	createConn := func(ctx context.Context) (net.Conn, error) {
		t.Logf("create new pConn start")
		conn, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
		t.Logf("create new pConn finish")
		return conn, err
	}

	t.Run("case 1", func(t *testing.T) {
		opt := &Option{
			MaxOpen: 10,
			MaxIdle: 10,
		}
		cp := NewConnPool(opt, createConn)
		for i := 0; i < 10; i++ {
			t.Run(fmt.Sprintf("for_%d", i), func(t *testing.T) {
				conn, errGet := cp.Get(context.Background())
				if errGet != nil {
					t.Fatalf("cp.Get unexpect error %v", errGet)
				}

				t.Logf("cp.Get()=%s", conn.RemoteAddr())

				sendContent := []byte(fmt.Sprintf("loop %d", i))

				{
					_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
					wrote, errWrite := conn.Write(append(sendContent, '\n'))
					_ = conn.SetWriteDeadline(time.Time{})

					t.Logf("conn.Write %d %v", wrote, errWrite)
					if errWrite != nil {
						t.Fatalf("pConn.Write unexpect error %v", errWrite)
					}
				}

				{
					_ = conn.SetReadDeadline(time.Now().Add(time.Second))
					rd := bufio.NewReader(conn)
					gotLine, _, errRead := rd.ReadLine()
					_ = conn.SetReadDeadline(time.Time{})

					t.Logf("rd.ReadLine()=%q", gotLine)

					if errRead != nil {
						t.Fatalf("rd.ReadLine() unexpect error %v", errRead)
					}
					if !bytes.Equal(gotLine, sendContent) {
						t.Fatalf("rd.ReadLine()=%q sendContent=%q", gotLine, sendContent)
					}
				}

				{
					want := 1
					if got := ts.getConnecting(); got != want {
						t.Fatalf("ts.getConnecting()=%d sendContent=%d", got, want)
					}
				}

				_ = conn.Close()

				t.Run("stats", func(t *testing.T) {
					got := cp.Stats()
					want := Stats{
						MaxOpen: 10,
						NumOpen: 1,
						InUse:   0,
						Idle:    1,
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("cp.Stats()=%s \n\t\t\t want=%s", got, want)
					}
				})
			})
		}
	})

}
