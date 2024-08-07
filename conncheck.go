// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/4

// https://github.com/go-sql-driver/mysql/blob/master/conncheck.go

//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd || solaris || illumos
// +build linux darwin dragonfly freebsd netbsd openbsd solaris illumos

package fspool

import (
	"errors"
	"io"
	"net"
	"syscall"
)

var errUnexpectedRead = errors.New("unexpected read from socket")
var errConnNil = errors.New("conn is nil")

func connCheck(conn net.Conn) error {
	if conn == nil {
		return errConnNil
	}
	var sysErr error

	sysConn, ok := conn.(syscall.Conn)
	if !ok {
		return nil
	}
	rawConn, err := sysConn.SyscallConn()
	if err != nil {
		return err
	}

	errRead := rawConn.Read(func(fd uintptr) bool {
		var buf [1]byte
		n, err2 := syscall.Read(int(fd), buf[:])
		switch {
		case n == 0 && err2 == nil:
			sysErr = io.EOF
		case n > 0:
			sysErr = errUnexpectedRead
		case errors.Is(err2, syscall.EAGAIN) || errors.Is(err2, syscall.EWOULDBLOCK):
			sysErr = nil
		default:
			sysErr = err2
		}
		return true
	})
	if errRead != nil {
		return errRead
	}

	return sysErr
}
