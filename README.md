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
	// maximum amount of time a connection may be reused
	MaxLifetime time.Duration

	// MaxIdleTime
	// maximum amount of time a connection may be idle before being closed
	MaxIdleTime time.Duration
}
```

```go
// Stats 状态
type Stats struct {
	MaxOpen int // Maximum number of open connections to the database.

	// SimplePool Status
	NumOpen int // The number of established connections both in use and idle.
	InUse   int // The number of connections currently in use.
	Idle    int // The number of idle connections.

	// Counters
	WaitCount         int64         // The total number of connections waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new connection.
	MaxIdleClosed     int64         // The total number of connections closed due to SetMaxIdleConns.
	MaxIdleTimeClosed int64         // The total number of connections closed due to SetConnMaxIdleTime.
	MaxLifetimeClosed int64         // The total number of connections closed due to SetConnMaxLifetime.
}
```