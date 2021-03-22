# fspool
一个通用的对象池

[![Build Status](https://travis-ci.org/fsgo/fspool.png?branch=master)](https://travis-ci.org/fsgo/fspool)
[![GoCover](https://gocover.io/_badge/github.com/fsgo/fspool?status.svg)](https://gocover.io/github.com/fsgo/fspool)
[![GoDoc](https://godoc.org/github.com/fsgo/fspool?status.svg)](https://godoc.org/github.com/fsgo/fspool)


```

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

```go
// Stats 状态
type Stats struct {
	MaxOpen int // Maximum number of open Elements to the pool.

	// SimplePool Status
	NumOpen int // The number of established Elements both in use and idle.
	InUse   int // The number of Elements currently in use.
	Idle    int // The number of idle Elements.

	// Counters
	WaitCount         int64         // The total number of Elements waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new element.
	MaxIdleClosed     int64         // The total number of Elements closed.
	MaxIdleTimeClosed int64         // The total number of Elements closed.
	MaxLifetimeClosed int64         // The total number of Elements closed.
}
```