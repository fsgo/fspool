// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/29

package fspool

import (
	"context"
	"io"
	"net"
)

// GroupNewConnFunc 给 Group 创建新的 pool
type GroupNewConnFunc func(addr net.Addr) NewConnFunc

func (gn GroupNewConnFunc) trans() GroupNewElementFunc {
	return func(key any) NewElementFunc {
		return func(ctx context.Context, pool PoolPutter) (Element, error) {
			conn, err := gn(key.(net.Addr))(ctx)
			if err != nil {
				return nil, err
			}
			return newPConn(conn, pool), nil
		}
	}
}

// NewConnPoolGroup 创建新的 ConnPool Group
func NewConnPoolGroup(opt *Option, gn GroupNewConnFunc) *ConnPoolGroup {
	return &ConnPoolGroup{
		raw: NewSimplePoolGroup(opt, gn.trans()),
	}
}

// ConnPoolGroup 按照 key 分组的 连接池
//
// 如有一批 IP：
//  1. 192.168.0.1:80
//  2. 192.168.0.2:80
//  3. 192.168.0.3:81
//
// 每个IP 都有独立的连接池。
// 配置的 Option 是针对每个IP的。
// 如 MaxOpen=1，则允许每个 IP 都最多创建1个连接，上面共有3个 IP，则一一共最多创建3个连接。
type ConnPoolGroup struct {
	raw *SimplePoolGroup
}

func (cg *ConnPoolGroup) Range(fn func(el net.Conn) error) error {
	return cg.raw.Range(func(el io.Closer) error {
		return fn(el.(net.Conn))
	})
}

func (cg *ConnPoolGroup) Option() Option {
	return cg.raw.Option()
}

func (cg *ConnPoolGroup) Get(ctx context.Context, addr net.Addr) (net.Conn, error) {
	el, err := cg.raw.Get(ctx, addr)
	if err != nil {
		return nil, err
	}
	return el.(net.Conn), err
}

func (cg *ConnPoolGroup) GroupStats() GroupStats {
	return cg.raw.GroupStats()
}

func (cg *ConnPoolGroup) Close() error {
	return cg.raw.Close()
}
