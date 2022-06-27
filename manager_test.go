package manager

import (
	"hot-reload/config"
	"hot-reload/manager"
	"hot-reload/observe"
	"os"
	"reflect"
	"testing"
	"time"
)

type TestObserver1 struct {
	c chan string
}

func (observer TestObserver1) Notify(message interface{}) {
	conf, _ := config.NewConfig("ini", message.(string))
	observer.c <- conf.String("server::ip")
}

type TestObserver2 struct {
	c chan string
}

func (observer TestObserver2) Notify(message interface{}) {
	conf, _ := config.NewConfig("ini", message.(string))
	observer.c <- conf.String("client::info")
}

var g_manager *manager.HotReloadManager
var observers []*observe.Observer

func init() {
	g_manager = manager.NewManager()
}

func iniFile1() {
	path := "conf/test.ini"
	os.Create(path)
	conf, _ := config.NewConfig("ini", path)
	conf.Set("server::ip", "127.0.0.1")
	conf.Set("server::port", "8888")
	conf.Set("client::name", "mike")
	conf.Set("client::info", "hello world")
	conf.SaveConfigFile(path)
}

func iniFile2() {
	path := "conf/test.ini"
	conf, _ := config.NewConfig("ini", path)
	conf.Set("server::ip", "0.0.0.0")
	conf.Set("server::port", "9999")
	conf.Set("client::name", "mike")
	conf.Set("client::info", "the ini file has been modified")
	conf.SaveConfigFile(path)
}

func manaegerTest(t *testing.T) {

	o1 := TestObserver1{c: make(chan string)}
	o2 := TestObserver2{c: make(chan string)}
	iniFile1()

	g_manager.AddFile("conf/test.ini", "ini", 3)

	go g_manager.Watch()
	time.Sleep(time.Second * 5)

	g_manager.AddObserver(o1)
	g_manager.AddObserver(o2)

	iniFile2()

	var data1, data2 string
	data1 = <-o1.c
	data2 = <-o2.c

	if !reflect.DeepEqual(data1, "0.0.0.0") {
		t.Fatal("notify data1 wrong!")
	}
	if !reflect.DeepEqual(data2, "the ini file has been modified") {
		t.Fatal("notify data2 wrong!")
	}

	time.Sleep(time.Second * 10)
	g_manager.Stop()

}
