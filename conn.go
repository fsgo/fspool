/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNotPoolConn 不是 ConnPool 支持的类型
var ErrNotPoolConn = errors.New("not pool pConn")

// NewConnFunc 创建新
type NewConnFunc func(ctx context.Context) (net.Conn, error)

// Trans 转换为原始的 NewElementFunc
func (nf NewConnFunc) Trans(p *ConnPool) NewElementFunc {
	return func(ctx context.Context, pool *SimplePool) (Element, error) {
		raw, err := nf(ctx)
		if err != nil {
			return nil, err
		}
		vc := newConn(raw, p)
		return vc, nil
	}
}

// NewConnPool 创建新的 net.Conn 的连接池
func NewConnPool(option *Option, newFunc NewConnFunc) *ConnPool {
	p := &ConnPool{}
	p.raw = NewSimple(option, newFunc.Trans(p))
	return p
}

// ConnPool 网络连接池
type ConnPool struct {
	raw *SimplePool
}

// Get get
func (cp *ConnPool) Get(ctx context.Context) (el net.Conn, err error) {
	value, err := cp.raw.Get(ctx)
	if err != nil {
		return nil, err
	}
	return value.(net.Conn), nil

}

// Put put to pool
func (cp *ConnPool) Put(cn net.Conn) error {
	if v, ok := cn.(*pConn); ok {
		return cp.raw.Put(v)
	}
	cn.Close()
	return ErrNotPoolConn
}

// Close close pool
func (cp *ConnPool) Close() error {
	return cp.raw.Close()
}

// Option get pool option
func (cp *ConnPool) Option() Option {
	return cp.raw.Option()
}

// Stats get pool stats
func (cp *ConnPool) Stats() Stats {
	return cp.raw.Stats()
}

const (
	statStart = iota
	statDone
)

func newConn(raw net.Conn, p *ConnPool) *pConn {
	return &pConn{
		raw:          raw,
		p:            p,
		WithTimeInfo: NewWithTimeInfo(),
	}
}

type pConn struct {
	*WithTimeInfo

	p *ConnPool

	raw net.Conn

	mu      sync.RWMutex
	lastErr error

	readStat  uint8
	writeStat uint8
	closeNum  int64
}

func (c *pConn) setErr(err error) {
	if err != nil {
		c.mu.Lock()
		c.lastErr = err
		c.mu.Unlock()
	}
}

func (c *pConn) Read(b []byte) (n int, err error) {
	c.WithTimeInfo.MarkUsed()

	c.withLock(func() {
		c.readStat = statStart
	})
	n, err = c.raw.Read(b)
	c.setErr(err)
	c.withLock(func() {
		c.readStat = statDone
	})
	return n, err
}

func (c *pConn) Write(b []byte) (n int, err error) {
	c.WithTimeInfo.MarkUsed()

	c.withLock(func() {
		c.writeStat = statStart
	})
	n, err = c.raw.Write(b)
	c.setErr(err)
	c.withLock(func() {
		c.writeStat = statDone
	})
	return n, err
}

func (c *pConn) Close() error {
	n := atomic.AddInt64(&c.closeNum, 1)
	if c.PEIsActive() {
		// 避免多次 调用 close,造成连接池里元素重复
		if atomic.LoadInt64(&c.closeNum) == n {
			return c.p.Put(c)
		}
		return nil
	}
	return c.PERawClose()
}

func (c *pConn) LocalAddr() net.Addr {
	return c.raw.LocalAddr()
}

func (c *pConn) RemoteAddr() net.Addr {
	return c.raw.RemoteAddr()
}

func (c *pConn) SetDeadline(t time.Time) error {
	err := c.raw.SetDeadline(t)
	c.setErr(err)
	return err
}

func (c *pConn) SetReadDeadline(t time.Time) error {
	err := c.raw.SetReadDeadline(t)
	c.setErr(err)
	return err
}

func (c *pConn) SetWriteDeadline(t time.Time) error {
	err := c.raw.SetWriteDeadline(t)
	c.setErr(err)
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

func (c *pConn) Raw() net.Conn {
	return c.raw
}

func (c *pConn) PERawClose() error {
	return c.raw.Close()
}

func (c *pConn) PEIsActive() bool {
	c.mu.RLock()

	if c.lastErr != nil || c.isDoing() {
		c.mu.RUnlock()
		return false
	}

	c.mu.RUnlock()

	if !c.WithTimeInfo.IsActive(c.p.Option()) {
		return false
	}

	if ra, ok := c.raw.(PEIsActiver); ok {
		if !ra.PEIsActive() {
			return false
		}
	}
	return true
}

var _ net.Conn = (*pConn)(nil)
var _ Element = (*pConn)(nil)
