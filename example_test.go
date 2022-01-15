// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/5

package fspool_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/fsgo/fspool"
)

func ExampleNewSimplePool() {
	type userInfo struct {
		num  int
		used int
	}
	opt := &fspool.Option{
		MaxIdle: 1,
	}
	p := fspool.NewSimplePool(opt, func(ctx context.Context, need fspool.NewElementNeed) (fspool.Element, error) {
		return fspool.NewSimpleElement(&fspool.SimpleRawItem{
			Raw: &userInfo{},
			Reset: func(raw interface{}) {
				raw.(*userInfo).num = 0
				raw.(*userInfo).used++
			},
		}), nil
	})

	for i := 0; i < 3; i++ {
		item, err := p.Get(context.Background())
		if err != nil {
			panic(err.Error())
		}
		u := item.(fspool.HasRaw).Raw().(*userInfo)
		fmt.Println("user.num=", u.num)
		fmt.Println("user.used=", u.used)

		// 使用对象，验证在放回对象池之后，会将它重置
		u.num = i + 3
		item.Close()
	}

	// Output:
	// user.num= 0
	// user.used= 0
	// user.num= 0
	// user.used= 1
	// user.num= 0
	// user.used= 2
}

func ExampleNewConnPool() {
	opt := &fspool.Option{
		MaxOpen:     10,
		MaxIdle:     5,
		MaxIdleTime: time.Minute,
		MaxLifeTime: 10 * time.Minute,
	}
	dz := &net.Dialer{}

	fn := func(ctx context.Context) (net.Conn, error) {
		return dz.DialContext(ctx, "tcp", "www.example.com:80")
	}
	cp := fspool.NewConnPool(opt, fn)

	fetch := func(ctx context.Context, msg string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		// 从连接池获取一个连接
		conn, err := cp.Get(ctx)
		if err != nil {
			return "", err
		}
		// 使用完了，关闭连接(连接放回连接池)
		defer conn.Close()

		if _, err = conn.Write([]byte(msg)); err != nil {
			return "", err
		}
		bf := make([]byte, 1024)
		n, err := conn.Read(bf)
		if err != nil {
			return "", err
		}
		return string(bf[:n]), nil
	}
	_ = fetch
}
