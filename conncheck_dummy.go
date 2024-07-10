// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/4

// https://github.com/go-sql-driver/mysql/blob/master/conncheck_dummy.go

//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !solaris && !illumos

package fspool

import "net"

func connCheck(conn net.Conn) error {
	return nil
}
