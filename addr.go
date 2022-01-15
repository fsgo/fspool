// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/4/13

package fspool

import (
	"net"
)

// NewAddr  net net.Addr
func NewAddr(network string, host string) net.Addr {
	return &cAddr{
		network: network,
		host:    host,
	}
}

var _ net.Addr = (*cAddr)(nil)

type cAddr struct {
	network string
	host    string
}

func (c *cAddr) Network() string {
	return c.network
}

func (c *cAddr) String() string {
	return c.host
}
