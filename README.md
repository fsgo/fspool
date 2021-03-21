# fspool
一个通用的对象池

[![Build Status](https://travis-ci.org/fsgo/fspool.png?branch=master)](https://travis-ci.org/fsgo/fspool)
[![GoCover](https://gocover.io/_badge/github.com/fsgo/fspool?status.svg)](https://gocover.io/github.com/fsgo/fspool)
[![GoDoc](https://godoc.org/github.com/fsgo/fspool?status.svg)](https://godoc.org/github.com/fsgo/fspool)


```go
// New 创建一个新的对象池
func New(ctx context.Context, opt Option) IPool 
```

```go
// IPool 连接池接口
type IPool interface {
    // Get 获取一个对象
    // 特别注意，正常的获取到一个 Element 后，必须使用Put对象放回对象池
    // 否则会导致已创建连接数等状态不能正常更新，而且之后不能正常获取新的对象
    Get(ctx context.Context) (obj IElement, err error)
    
    // Put 将对象放回对象池
    Put(obj IElement)
    
    // Execute 获取一个对象并执行业务逻辑
    // 若从池中获取对象失败，将不会执行fn
    // 执行完成后，会自动将对象回收（自动调用Put）
    Execute(ctx context.Context, fn UserFn) error
    
    // Stats 获取状态信息
    Stats() Stats
    
    // Close 关闭对象池
    Close() error
}

```

```go
// Option 配置选项
type Option struct {
    // MaxIdle 最大空闲数量
    // 若为0，则对象不复用
    MaxIdle uint64
    
    // MaxOpen 总的对象数，包含空闲的对象
    // 若为0，则是不限制
    MaxOpen uint64
    
    // MaxTry 最多尝试次数，若<=0 则使用默认值3
    // 适用于调用Pool.Get方法时， Element 的 Open 和 CheckAlive 失败时(err!=nil)
    MaxTry uint32
    
    // MaxLifeTime 新对象存活有效期
    // 若值为0，则不过期
    MaxLifeTime time.Duration
    
    // New 创建新对象的方法
    // 不可为空
    New NewElementFn
}
```