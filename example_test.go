// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/6/5

package fspool_test

import (
	"context"
	"fmt"

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
		u := item.(fspool.PERaw).Raw().(*userInfo)
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
