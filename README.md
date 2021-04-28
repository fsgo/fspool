# fspool
一个通用的对象池

[![Build Status](https://travis-ci.org/fsgo/fspool.png?branch=master)](https://travis-ci.org/fsgo/fspool)
[![GoCover](https://gocover.io/_badge/github.com/fsgo/fspool?status.svg)](https://gocover.io/github.com/fsgo/fspool)
[![GoDoc](https://godoc.org/github.com/fsgo/fspool?status.svg)](https://godoc.org/github.com/fsgo/fspool)

## 1. 连接池配置
```go
// Option pool option
type Option struct {
	// MaxOpen max opening element
	// <= 0 means unlimited
	MaxOpen int

	// MaxIdle
	// <=0 means disabled
	MaxIdle int

	// MaxLifetime
	// maximum amount of time a Element may be reused
	MaxLifetime time.Duration

	// MaxIdleTime
	// maximum amount of time a Element may be idle before being closed
	MaxIdleTime time.Duration
}
```

## 2. 连接池状态
```go
// Stats 状态
type Stats struct {
	Open bool // 连接池的状态，true-正常，false-已关闭

	NumOpen int // 已打开的总数
	InUse   int // 正被使用的总数
	Idle    int // 连接池里空闲的总数

	// Counters
	WaitCount         int64         // 等待的请求数
	WaitDuration      time.Duration // 等待的总时间
	MaxIdleClosed     int64         // 由于超过 MaxIdle,被关闭的总数
	MaxIdleTimeClosed int64         // 由于超过 MaxIdleTime，被关闭的总数
	MaxLifetimeClosed int64         // 由于超过 MaxLifetime，被关闭的总数
}
```

## 3. 网络连接池 ConnPool ( 单个固定 IP )
```go
p = fspool.NewConnPool(nil, func(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", "127.0.0.1:80")
})

// 获取一个连接，可能是复用的老的连接，也可能是新创建的
// 若无空闲的，同时达到 MaxOpen 上限时，Get 会阻塞直到获取到 或者 超时
conn, err := p.Get(ctx)

// 获取 连接的创建时间、使用次数、使用时长等信息
meta:=fspool.ReadMeta(conn)
```
使用示例详见 [Examples : pool_client](./examples/server_client/pool_client)

## 4. 网络连接池 ConnPoolGroup (多个IP)
如有一批 IP：
1. 192.168.0.1:80
2. 192.168.0.2:80
3. 192.168.0.3:81

每个IP 都有独立的连接池。  
配置的 Option 是针对每个IP的。
如 MaxOpen=1，则允许每个 IP 都都最多创建1个连接，上面共有3个IP，则一一共最多创建3个连接。 

ConnPoolGroup 是基于 ConnPool 封装而来。

```go
pg := NewConnPoolGroup(nil, func(addr net.Addr) NewConnFunc {
	return func(ctx context.Context) (net.Conn, error) {
		return net.DialTimeout(addr.Network(), addr.String(), time.Second)
	}
})

// Get 行为同上述 ConnPool
conn, err := pg.Get(ctx,net.Addr{192.168.0.1:80})

conn2, err2 := pg.Get(ctx,net.Addr{192.168.0.3:80})
```

## 5. 通用对象池 SimplePool
SimplePool 是 ConnPool 的底层实现。