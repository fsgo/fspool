/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"net"
	"sync"
	"time"
)

// NewConnFunc 创建新连接
type NewConnFunc func(ctx context.Context) (net.Conn, error)

// Trans 转换为原始的 NewElementFunc
func (nf NewConnFunc) Trans(p *connPool) NewElementFunc {
	return func(ctx context.Context, pool NewElementNeed) (Element, error) {
		raw, err := nf(ctx)
		if err != nil {
			return nil, err
		}
		vc := newPConn(raw, p)
		return vc, nil
	}
}

// NewConnPool 创建新的 net.Conn 的连接池
func NewConnPool(option *Option, newFunc NewConnFunc) ConnPool {
	p := &connPool{}
	p.raw = NewSimple(option, newFunc.Trans(p))
	return p
}

// ConnPool 网络连接池
type ConnPool interface {
	Get(ctx context.Context) (net.Conn, error)
	Option() Option
	Stats() Stats
	Range(func(net.Conn) error) error
	Close() error
}

var _ ConnPool = (*connPool)(nil)

// connPool 网络连接池
type connPool struct {
	raw SimplePool
}

// Get get
func (cp *connPool) Get(ctx context.Context) (el net.Conn, err error) {
	value, err := cp.raw.Get(ctx)
	if err != nil {
		return nil, err
	}
	return value.(net.Conn), nil

}

// Put put to pool
func (cp *connPool) Put(value interface{}) error {
	if value == nil {
		return cp.raw.(NewElementNeed).Put(nil)
	}
	// if type invalid, then panic
	return cp.raw.(NewElementNeed).Put(value.(*pConn))
}

func (cp *connPool) Range(fn func(net.Conn) error) error {
	return cp.raw.Range(func(el Element) error {
		return fn(el.(net.Conn))
	})
}

// Close close pool
func (cp *connPool) Close() error {
	return cp.raw.Close()
}

// Option get pool option
func (cp *connPool) Option() Option {
	return cp.raw.Option()
}

// Stats get pool stats
func (cp *connPool) Stats() Stats {
	return cp.raw.Stats()
}

const (
	statInit = iota
	statStart
	statDone
)

func newPConn(raw net.Conn, p NewElementNeed) *pConn {
	return &pConn{
		raw:      raw,
		pool:     p,
		MetaInfo: NewMetaInfo(),
	}
}

var _ net.Conn = (*pConn)(nil)
var _ Element = (*pConn)(nil)

type pConn struct {
	*MetaInfo

	pool NewElementNeed

	raw net.Conn

	mu      sync.RWMutex
	lastErr error

	readStat  uint8
	writeStat uint8
}

func (c *pConn) setErr(err error) {
	if err != nil {
		c.mu.Lock()
		c.lastErr = err
		c.mu.Unlock()
	}
}

func (c *pConn) Read(b []byte) (n int, err error) {
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
	if c.PEIsActive() {
		c.withLock(func() {
			c.readStat = statInit
			c.writeStat = statInit
		})
		return c.pool.Put(c)
	}
	_ = c.pool.Put(nil) // for numOpen--
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

	if !c.MetaInfo.IsActive(c.pool.Option()) {
		return false
	}

	if ra, ok := c.raw.(PEIsActiver); ok {
		if !ra.PEIsActive() {
			return false
		}
	}
	return true
}

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
