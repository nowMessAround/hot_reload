package manager

import (
	"hot-reload/observe"
	"io/ioutil"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type Manager interface {
	Watch()
	Notify()
	add()
}

//单个文件监督
type SingleFile struct {
	lastModTime int64         //文件最后一次修改时间
	path        string        //文件路径
	interval    int           //检查文件修改的间隔时间
	modChan     chan struct{} //文件变化通知chan
	stopChan    chan struct{} //中断文件监督的chan
	typeName    string        //配置文件的类型
}

//注意上面的channel是需要初始化的
//比如说：
// modified := make(chan struct{})
//或者是说，后面同时管理多个文件的时候，使用同一个channel来给这些单文件监督的对象进行初始化
//啊后者不行，因为如果用同一个channel的话，就不清楚是哪一个文件变化了
//试试这个 https://blog.csdn.net/adream307/article/details/110386172

func (s *SingleFile) watch() {
	ticker := time.NewTicker((time.Second * time.Duration(s.interval)))
	for {
		select {
		case <-s.stopChan: //有信息说明要退出
			return
		case <-ticker.C:

			//就是监控文件
			f, err := os.Open(s.path)
			if err != nil {

			}
			defer f.Close()

			fi, err := f.Stat()
			if err != nil {
				//打开失败就跳过这一轮，因为有可能监督的文件还没创建或者有剪切操作啥的暂时不在
				break
			}

			//获取到最后一次修改的时间了
			modTime := fi.ModTime().Unix()

			if modTime > s.lastModTime {
				//有更新了，需要处理！
				//给一个信息过去 说明已经要处理了
				s.lastModTime = modTime
				s.modChan <- struct{}{}
			}
		}
	}
}

func (s *SingleFile) stop() {
	s.stopChan <- struct{}{}
}
func (s *SingleFile) raw() string {
	file, err := os.Open(s.path)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	content, err := ioutil.ReadAll(file)
	return string(content)
}

//应该是可以对多个文件同时进行监督的
//多个文件监督
type HotReloadManager struct {
	// files        [](*SingleFile) //对应的所有文件
	// pathFileMaps map[string]int  //文件路径与对应文件下标的映射
	files map[string](*SingleFile) //文件路径与对应文件下标的映射

	// fileChans   []chan struct{}    //文件对应的chan 下标一致
	// isStart bool //标识是否启动
	isStart atomic.Value

	observers   []*observe.Observer //对应所有的观察者
	outloopChan chan struct{}
	mutex       sync.Mutex

	addFiles    map[string](*SingleFile) //要添加的文件 map去重
	removeFiles map[string]struct{}      //要删除的文件 set格式
}

func NewManager() *HotReloadManager {
	return &HotReloadManager{
		files:       make(map[string](*SingleFile)),
		outloopChan: make(chan struct{}),
		addFiles:    make(map[string](*SingleFile)),
		removeFiles: make(map[string]struct{}),
		observers:   make([]*observe.Observer, 0),
	}
}

func (manager *HotReloadManager) Stop() {
	manager.isStart.Store(false)
	manager.outloopChan <- struct{}{}
}

//现在要处理 对于多个文件监督后，对于不同文件处理的问题
func (manager *HotReloadManager) Watch() {

	//对于每一个文件的监督打开
	//manager.files只被当前函数访问和修改，所以不用锁
	for _, f := range manager.files {
		go f.watch()
	}

	manager.isStart.Store(true)
	for manager.isStart.Load() == true {

		//先看有没有监听文件修改操作
		if len(manager.addFiles) > 0 || len(manager.removeFiles) > 0 {

			//由于对于files的读写只有watch函数会进行，所以不用上锁
			//添加
			for path, file := range manager.addFiles {
				manager.files[path] = file
				go file.watch() //启动监听
			}

			//删除
			for path, _ := range manager.removeFiles {
				if file, ok := manager.files[path]; ok {
					file.stop() //取消监听
					delete(manager.files, path)
				}
			}

			//清空两个容器 创建新的容器来实现清空
			//这两个容器会被其他函数访问，需要上锁
			//这里使用defer的话会导致锁阻塞在for中
			manager.mutex.Lock()

			manager.addFiles = make(map[string](*SingleFile))
			manager.removeFiles = make(map[string]struct{})

			manager.mutex.Unlock()

		}

		//剩下就是监听

		//创建能够容纳filechans的长度 并增加一个给context的位置
		//这里不先记录长度也行，因为只有前面阶段长度才会变化
		fileChanNum := len(manager.files)
		cases := make([]reflect.SelectCase, 0, fileChanNum+1)

		//把已有的chan依次增加到cases里面
		//由于map是哈希表，遍历顺序可能不太稳定，这里用一个map来记录一下映射
		chanPath := make([]string, 0, fileChanNum)
		for path, file := range manager.files {
			cases = append(cases, reflect.SelectCase{
				Dir:  reflect.SelectRecv,            //代表一个接收操作 即chan<-
				Chan: reflect.ValueOf(file.modChan), //具体的chan对象
			})
			chanPath = append(chanPath, path)
		}

		//增加用于退出select的chan
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,                   //代表一个接收操作 即chan<-
			Chan: reflect.ValueOf(manager.outloopChan), //具体的chan对象
		})

		//循环switch处理chan的信息
		for {
			//进行select操作，通道有信息会退出
			//第二个参数是接收到的内容，但我们这里是struct{}所以不需要关心中间内容
			chosen, _, ok := reflect.Select(cases)
			if ok {
				//说明是通道中有信息，而不是通道关闭了，如果是通道关闭则重复这个循环就可以，无需处理本轮
				if chosen == fileChanNum {
					//中断
					break

				} else {
					//有文件的chan有消息
					//通知
					manager.notify(chanPath[chosen])
				}
			}
		}
	}

	//退出后要关闭
	for _, f := range manager.files {
		f.stop()
	}

}

func (manager *HotReloadManager) notify(path string) {

	// file := manager.files[path]
	// raw := file.raw()

	//这种访问可能使用陈硕的那种copy on write会好一些
	manager.mutex.Lock()

	//复制一份出来遍历，避免上锁太久
	// 将数据复制到新的切片空间中
	copyData := make([](*observe.Observer), len(manager.observers))
	copy(copyData, manager.observers)

	manager.mutex.Unlock()

	//但如果遍历一半observer对象挂掉了也可能会有问题
	//这边希望能在Notify中处理，golang由于有gc设置应该不会出现对象销毁的问题
	for _, observer := range copyData {
		//暂时是本地的
		//如果是分布式的，应该是把信息解析好后发过去好一些
		go (*observer).Notify(path)
	}

}

//增加文件 有相同path的会覆盖
func (manager *HotReloadManager) AddFile(path string, fileType string, interval int) {

	//不存在才创建
	//对于同一个文件调用多次AddFile的情况，在添加的时候会使用set去重
	file := SingleFile{
		path:        path,
		typeName:    fileType,
		interval:    interval,
		modChan:     make(chan struct{}),
		stopChan:    make(chan struct{}),
		lastModTime: time.Now().Unix(),
	}

	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	manager.addFiles[path] = &file
}

//删除文件
func (manager *HotReloadManager) RemoveFile(path string) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	//用set来去重
	manager.removeFiles[path] = struct{}{}
}

//观察者的增加
func (manager *HotReloadManager) AddObserver(observer observe.Observer) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	manager.observers = append(manager.observers, &observer)
}

//本来想用index的，但是想到，如果中间先被其他remove的获取到了，而这边等待锁
//等修改完之后，这边获得锁，可能对应的index已经不准了，所以还是使用notifier来处理
func (manager *HotReloadManager) RemoveObserver(observer observe.Observer) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	for index, o := range manager.observers {
		if reflect.DeepEqual(o, observer) {
			manager.observers = append(manager.observers[:index], manager.observers[index:]...)
			break
		}
	}
}
