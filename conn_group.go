// Copyright(C) 2021 github.com/hidu  All Rights Reserved.
// Author: hidu (duv123+git@baidu.com)
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
	return func(key interface{}) NewElementFunc {
		return func(ctx context.Context, pool NewElementNeed) (Element, error) {
			conn, err := gn(key.(net.Addr))(ctx)
			if err != nil {
				return nil, err
			}
			return newPConn(conn, pool), nil
		}
	}
}

// NewConnPoolGroup 创建新的 Group
func NewConnPoolGroup(opt *Option, gn GroupNewConnFunc) ConnPoolGroup {
	return &connGroup{
		raw: NewSimplePoolGroup(opt, gn.trans()),
	}
}

// ConnPoolGroup 按照 key 分组的 连接池
//
// 如有一批 IP：
// 	1. 192.168.0.1:80
// 	2. 192.168.0.2:80
// 	3. 192.168.0.3:81
//
// 每个IP 都有独立的连接池。
// 配置的 Option 是针对每个IP的。
// 如 MaxOpen=1，则允许每个 IP 都最多创建1个连接，上面共有3个 IP，则一一共最多创建3个连接。
type ConnPoolGroup interface {
	Get(ctx context.Context, addr net.Addr) (net.Conn, error)
	GroupStats() GroupStats
	Close() error
	Option() Option
	Range(func(el net.Conn) error) error
}

var _ ConnPoolGroup = (*connGroup)(nil)

type connGroup struct {
	raw SimplePoolGroup
}

func (cg *connGroup) Range(fn func(el net.Conn) error) error {
	return cg.raw.Range(func(el io.Closer) error {
		return fn(el.(net.Conn))
	})
}

func (cg *connGroup) Option() Option {
	return cg.raw.Option()
}

func (cg *connGroup) Get(ctx context.Context, addr net.Addr) (net.Conn, error) {
	el, err := cg.raw.Get(ctx, addr)
	if err != nil {
		return nil, err
	}
	return el.(net.Conn), err
}

func (cg *connGroup) GroupStats() GroupStats {
	return cg.raw.GroupStats()
}

func (cg *connGroup) Close() error {
	return cg.raw.Close()
}
