package observe

//一个问题是没有说对，
//这里的observer指的应该是需要文件的程序。。
//而不是监听文件的部分。。

import (
	"hot-reload/config"
)

type Observer interface {
	Notify(interface{})
}

//从文件中读取信息的observer
type MyFileObserver1 struct {
}

func (observer MyFileObserver1) Notify(message interface{}) {

	//要反解message中的信息 emm但是这样就需要把中间的格式先确认好。。
	// json.Unmarshal(message)

	config.NewConfig("ini", message.(string))
}
