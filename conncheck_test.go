// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/4

//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd || solaris || illumos

package fspool

import (
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func Test_connCheck(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	t.Run("good conn", func(t *testing.T) {
		conn, err := net.DialTimeout(ts.Listener.Addr().Network(), ts.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf(err.Error())
		}
		defer conn.Close()
		if err = connCheck(conn); err != nil {
			t.Fatalf(err.Error())
		}
		conn.Close()

		if err = connCheck(conn); err == nil {
			t.Fatalf("expect has error")
		}
	})

	t.Run("bad conn 2", func(t *testing.T) {
		conn, err := net.DialTimeout(ts.Listener.Addr().Network(), ts.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf(err.Error())
		}
		defer conn.Close()

		ts.Close()

		if err = connCheck(conn); err == nil {
			t.Fatalf("expect has err")
		}
	})
}
