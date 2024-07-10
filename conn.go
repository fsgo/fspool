// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/21

package fspool

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

// NewConnFunc 创建新连接
type NewConnFunc func(ctx context.Context) (net.Conn, error)

// Trans 转换为原始的 NewElementFunc
func (nf NewConnFunc) trans() NewElementFunc {
	return func(ctx context.Context, pool PoolPutter) (Element, error) {
		raw, err := nf(ctx)
		if err != nil {
			return nil, err
		}
		vc := newPConn(raw, pool)
		return vc, nil
	}
}

// NewConnPool 创建新的 net.Conn 的连接池
func NewConnPool(option *Option, newFunc NewConnFunc) *ConnPool {
	return &ConnPool{
		raw: NewSimplePool(option, newFunc.trans()),
	}
}

// ConnPool 网络连接池
type ConnPool struct {
	raw *SimplePool
}

// Get 获取一个网络连接
func (cp *ConnPool) Get(ctx context.Context) (el net.Conn, err error) {
	value, err := cp.raw.Get(ctx)
	if err != nil {
		return nil, err
	}
	return value.(net.Conn), nil
}

func (cp *ConnPool) Range(fn func(net.Conn) error) error {
	return cp.raw.Range(func(el io.Closer) error {
		return fn(el.(net.Conn))
	})
}

// Close 关闭连接池
func (cp *ConnPool) Close() error {
	return cp.raw.Close()
}

// Option get pool option
func (cp *ConnPool) Option() Option {
	return cp.raw.Option()
}

// Stats 连接池的状态
func (cp *ConnPool) Stats() Stats {
	return cp.raw.Stats()
}

const (
	statInit = iota
	statStart
	statDone
)

func newPConn(raw net.Conn, p PoolPutter) *pConn {
	return &pConn{
		raw:      raw,
		pool:     p,
		MetaInfo: NewMetaInfo(),
	}
}

var _ net.Conn = (*pConn)(nil)
var _ CanSetError = (*pConn)(nil)
var _ Element = (*pConn)(nil)
var _ HasPERaw = (*pConn)(nil)
var _ HasMeta = (*pConn)(nil)

type pConn struct {
	*MetaInfo

	pool    PoolPutter
	lastErr error
	raw     net.Conn

	mu sync.RWMutex

	readStat  uint8
	writeStat uint8

	closed bool
}

func (c *pConn) isClosed() bool {
	c.mu.RLock()
	d := c.closed
	c.mu.RUnlock()
	return d
}

func (c *pConn) clonePConn() *pConn {
	c.mu.RLock()
	d := &pConn{
		MetaInfo: c.MetaInfo.cloneMeta(),
		pool:     c.pool,
		raw:      c.raw,
		lastErr:  c.lastErr,
	}
	c.mu.RUnlock()
	return d
}

func (c *pConn) SetError(err error) {
	if err != nil {
		c.mu.Lock()
		c.lastErr = err
		c.mu.Unlock()
	}
}

func (c *pConn) Read(b []byte) (n int, err error) {
	if c.isClosed() {
		return 0, ErrClosedValue
	}
	c.withLock(func() {
		c.readStat = statStart
	})
	n, err = c.raw.Read(b)
	c.SetError(err)
	c.withLock(func() {
		c.readStat = statDone
	})
	return n, err
}

func (c *pConn) Write(b []byte) (n int, err error) {
	if c.isClosed() {
		return 0, ErrClosedValue
	}
	c.withLock(func() {
		c.writeStat = statStart
	})
	n, err = c.raw.Write(b)
	c.SetError(err)
	c.withLock(func() {
		c.writeStat = statDone
	})
	return n, err
}

func (c *pConn) PEReset() {
	if c.isClosed() {
		return
	}
	c.withLock(func() {
		c.readStat = statInit
		c.writeStat = statInit
	})
	_ = c.SetDeadline(time.Time{})
	_ = c.SetReadDeadline(time.Time{})
	_ = c.SetWriteDeadline(time.Time{})
}

func (c *pConn) Close() error {
	var closed bool
	c.withLock(func() {
		closed = c.closed
		c.closed = true
	})
	if closed {
		return ErrClosedValue
	}
	return c.pool.Put(c.clonePConn())
}

func (c *pConn) LocalAddr() net.Addr {
	return c.raw.LocalAddr()
}

func (c *pConn) RemoteAddr() net.Addr {
	return c.raw.RemoteAddr()
}

func (c *pConn) SetDeadline(t time.Time) error {
	if c.isClosed() {
		return ErrClosedValue
	}
	err := c.raw.SetDeadline(t)
	c.SetError(err)
	return err
}

func (c *pConn) SetReadDeadline(t time.Time) error {
	if c.isClosed() {
		return ErrClosedValue
	}
	err := c.raw.SetReadDeadline(t)
	c.SetError(err)
	return err
}

func (c *pConn) SetWriteDeadline(t time.Time) error {
	if c.isClosed() {
		return ErrClosedValue
	}
	err := c.raw.SetWriteDeadline(t)
	c.SetError(err)
	return err
}

func (c *pConn) withLock(fn func()) {
	c.mu.Lock()
	fn()
	c.mu.Unlock()
}

func (c *pConn) isDoing() bool {
	return c.readStat == statStart || c.writeStat == statStart
}

func (c *pConn) Unwrap() net.Conn {
	if c.isClosed() {
		return nil
	}
	return c.raw
}

var _ HasPERaw = (*pConn)(nil)

func (c *pConn) PERaw() any {
	return c.Unwrap()
}

func (c *pConn) PERawClose() error {
	if c.isClosed() {
		return ErrClosedValue
	}
	return c.raw.Close()
}

func (c *pConn) PEActive() error {
	if c.isClosed() {
		return ErrClosedValue
	}

	c.mu.RLock()

	if c.lastErr != nil {
		c.mu.RUnlock()
		return c.lastErr
	}

	if c.isDoing() {
		c.mu.RUnlock()
		return ErrBadValue
	}

	c.mu.RUnlock()

	if ea := c.MetaInfo.Active(c.pool.Option()); ea != nil {
		return ea
	}

	if ra, ok := c.raw.(CanCheckActive); ok {
		if ea := ra.PEActive(); ea != nil {
			return ea
		}
	}

	// 检查连接是否有效
	if err := connCheck(c.rawConn()); err != nil {
		return err
	}
	return nil
}

func (c *pConn) rawConn() net.Conn {
	raw := c.raw
	for {
		if r, ok := raw.(interface{ Unwrap() net.Conn }); ok {
			raw = r.Unwrap()
		} else {
			break
		}
	}
	return raw
}
