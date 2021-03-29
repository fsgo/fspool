/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/29
 */

package fspool

import (
	"bufio"
	"context"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestNewConnPoolGroup(t *testing.T) {
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

	t.Run("case 1", func(t *testing.T) {
		pg := NewConnPoolGroup(nil, func(key interface{}) NewConnFunc {
			addr := key.(net.Addr)
			return func(ctx context.Context) (net.Conn, error) {
				return net.DialTimeout(addr.Network(), addr.String(), time.Second)
			}
		})
		t.Run("GroupStats", func(t *testing.T) {
			got := pg.GroupStats().All
			want := Stats{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%v, want=%v", got, want)
			}
		})

		t.Run("rw1", func(t *testing.T) {
			conn, err := pg.Get(context.Background(), l.Addr())
			if err != nil {
				t.Fatalf(err.Error())
			}
			_, errW := conn.Write([]byte("hello\n"))
			if errW != nil {
				t.Fatalf(errW.Error())
			}

			rd := bufio.NewReader(conn)
			_, _, errRead := rd.ReadLine()
			if errRead != nil {
				t.Fatalf(errRead.Error())
			}

			t.Run("before close", func(t *testing.T) {
				{
					got := pg.GroupStats().All
					want := Stats{
						NumOpen: 1,
						InUse:   1,
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("got=%v, want=%v", got, want)
					}
				}
			})

			if errClose := conn.Close(); errClose != nil {
				t.Fatalf("conn.Close()=%v", errClose)
			}

			t.Run("after close", func(t *testing.T) {
				{
					got := pg.GroupStats().All
					want := Stats{
						MaxIdleClosed: 1,
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("got=%v, want=%v", got, want)
					}
				}
			})
		})
		t.Run("close", func(t *testing.T) {
			err := pg.Close()
			if err != nil {
				t.Fatalf(err.Error())
			}
			st := pg.GroupStats()
			if got := len(st.Groups); got != 0 {
				t.Fatalf("st.Groups.len got=%d want=0", got)
			}
		})
	})
}