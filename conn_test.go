// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/23

package fspool

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testConnServer struct {
	onAcceptErr func(err error)
	connecting  int32
	listener    net.Listener
	close       func()
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
	defer conn.Close()
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

func (ts *testConnServer) clientRW(conn net.Conn) error {
	defer conn.Close()
	_, err := conn.Write([]byte("hello\n"))
	if err != nil {
		return err
	}
	rd := bufio.NewReader(conn)
	_, _, errRead := rd.ReadLine()
	return errRead
}

func (ts *testConnServer) getConnecting() int {
	val := atomic.LoadInt32(&ts.connecting)
	return int(val)
}

func (ts *testConnServer) Start() {
	if ts.onAcceptErr == nil {
		ts.onAcceptErr = func(err error) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	ts.close = cancel

	l, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		panic(errListen)
	}
	ts.listener = l
	go func() {
		_ = ts.Serve(ctx, l)
	}()
}

func (ts *testConnServer) Close() {
	ts.close()
	ts.listener.Close()
}

func (ts *testConnServer) dialCtx(ctx context.Context) (net.Conn, error) {
	addr := ts.listener.Addr()
	return (&net.Dialer{}).DialContext(ctx, addr.Network(), addr.String())
}

func TestNewConnPool(t *testing.T) {
	ts := &testConnServer{}
	ts.Start()
	defer ts.Close()

	createConn := func(ctx context.Context) (net.Conn, error) {
		t.Logf("create new pConn start")
		conn, err := net.DialTimeout("tcp", ts.listener.Addr().String(), time.Second)
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
				require.NoError(t, errGet)

				// todo check it
				_ = ReadMeta(conn)

				t.Logf("cp.Get()=%s", conn.RemoteAddr())

				sendContent := []byte(fmt.Sprintf("loop %d", i))

				t.Run("write", func(t *testing.T) {
					require.NoError(t, conn.SetWriteDeadline(time.Now().Add(time.Second)))
					wrote, errWrite := conn.Write(append(sendContent, '\n'))
					t.Logf("conn.Write %d %v", wrote, errWrite)
					require.NoError(t, errWrite)
					require.NoError(t, conn.SetWriteDeadline(time.Time{}))
				})

				t.Run("read", func(t *testing.T) {
					require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
					rd := bufio.NewReader(conn)
					gotLine, _, errRead := rd.ReadLine()
					t.Logf("rd.ReadLine() %q err=%v", gotLine, errRead)
					require.NoError(t, errRead)
					require.NoError(t, conn.SetReadDeadline(time.Time{}))

					require.Equal(t, string(sendContent), string(gotLine))
				})

				t.Run("getConnecting", func(t *testing.T) {
					require.Equal(t, 1, ts.getConnecting())
				})

				t.Run("close", func(t *testing.T) {
					require.NoError(t, conn.Close())
				})

				t.Run("stats", func(t *testing.T) {
					got := cp.Stats()
					want := Stats{
						Open:    true,
						NumOpen: 1,
						InUse:   0,
						Idle:    1,
					}
					require.Equal(t, want, got)
				})
			})
		}
	})
}

func Benchmark_RW_ConnPool(b *testing.B) {
	ts := &testConnServer{}
	ts.Start()
	defer ts.Close()
	opt := &Option{
		MaxOpen: 10,
		MaxIdle: 10,
	}
	cp := NewConnPool(opt, ts.dialCtx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := cp.Get(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if err = ts.clientRW(conn); err != nil {
				b.Fatal(err)
			}
		}()
	}
}

func Benchmark_RW_NoPool(b *testing.B) {
	ts := &testConnServer{}
	ts.Start()
	defer ts.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := ts.dialCtx(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if err = ts.clientRW(conn); err != nil {
				b.Fatal(err)
			}
		}()
	}
}
