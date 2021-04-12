/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/29
 */

package fspool

import (
	"context"
	"net"
)

// GroupNewConnFunc 给 Group 创建新的 pool
type GroupNewConnFunc func(key interface{}) NewConnFunc

func (gn GroupNewConnFunc) trans() GroupNewElementFunc {
	return func(key interface{}) NewElementFunc {
		return func(ctx context.Context, pool NewElementNeed) (Element, error) {
			conn, err := gn(key)(ctx)
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
type ConnPoolGroup interface {
	Get(ctx context.Context, addr net.Addr) (net.Conn, error)
	GroupStats() GroupStats
	Close() error
	Option() Option
}

var _ ConnPoolGroup = (*connGroup)(nil)

type connGroup struct {
	raw SimplePoolGroup
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
