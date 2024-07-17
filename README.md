## goskynet
![hey](https://raw.githubusercontent.com/llsw/sgoly/dev/doc/sk/img/logo.gif)  
go 实现的skynet cluster，可以和lua版的skynet互相通信。集群底层网络linux、macosx使用的是netpoll，windows使用的是go标准的网络，详情可以看[network/skynet/cluster.go](network/skynet/cluster.go) listen方法。未来想实现[优雅的热重启](#优雅的热重启)。已在线上运行1年。
## 鸣谢
* 云风大佬的[skynet](https://github.com/cloudwu/skynet.git)
* asynkron的[protoactor-go](https://github.com/asynkron/protoactor-go.git)
* 字节跳动的[netpoll](https://github.com/cloudwego/hertz/tree/develop/pkg/network/netpoll)


# env
* os: linux macosx windows(暂时没有时间测试)
* go: 1.20.1
* make: GNU Make 3.81

## example
```bash
# service的例子 example/service
# cluster的例子 example/cluster

# build and run test
# linux
make linux
# macosx
make macosx
```

## 项目文件
```bash
.
├── bin                             # 编译产物，根据平台划分
│   └── macosx
├── example                         # 使用例子
│   ├── cluster1                    # 集群1
│   ├── cluster2                    # 集群2
│   ├── cluster3                    # 集群3
│   ├── service                     # 集群本地服务例子
│   └── clustername.yaml            # 集群ip配置
├── img
│   └── hot_reload.jpg
├── lib                             # 共用库
│   ├── config                      # 配置
│   ├── const                       # 常量
│   ├── log                         # 日志
│   ├── share                       # 共享定义
│   └── utils                       # 工具方法
├── log                             # 默认日志存放路径
├── network                         # 网络库
│   └── skynet                      # skynet网络核心
├── service                         # 公用服务
│   ├── cluster.go                  # 集群服务
│   ├── entity.go                   # 实体服务，只存放数据
│   ├── hello.go                    # 服务例子
│   ├── mysql.go                    # mysql服务
│   ├── pprof.go                    # 性能监控服务
│   ├── redis.go                    # redis服务
│   └── service.go                  # 核心服务
├── Makefile                        # Makefile配置文件，包含常用命令，如数据库orm模型生成命令
├── README.md       
├── go.mod                  
├── go.sum
└── platform.mk                     # make平台相关
```

## 自定义网络协议
1. 本项目只实现了集群间rpc，未处理来自外部客户端的消息，不同的客户端需求不一样，本项目难以兼顾。
2. 如果想添加外部消息协议，比如http、grpc，可以在service文件夹新建个服务，接收请求，收到请求后再集群间rpc处理。例如http可以使用gin、echo、hertz等优秀的开源http框架。grpc这个就不用说了，go对grpc支持很友好。
3. 如果是自定义的tcp协议，可以复用network/skynet/gate.go，具体例子参照network/skynet/cluster.go NewCluster方法，实现自己的网络相关回调方法。底层已经使用了netpoll，不需要自己处理多路复用。
* SetOnAccept  接受连接
* SetOnConnect 连接建立
* SetClose     连接断开
* SetOnUnpack  解网络包，目前网络包的格式为2个字节(大端)包长度+包体
* SetOnMsg     解网络包后的消息处理

## 性能测试
1. macosx 12线程 QPS 15w 未能榨干CPU
```bash
# 启动处理进程
# example/cluster1
make cluster1
# 启动请求进程
# example/cluster2  
make cluster2
```

## 一些优化的措施
1. 目前主要是限制goroutine数量，pc的核心数是有限的，x核n线程，理论上同时只能处理x*n个协程。协程数量多未必处理就快，反而增加了协程调度开销。当然如果任务是等待时间远大于处理时间，例如定时任务等，则需要开协程进行处理。不过也可以进行优化，通过时间轮，将多个定时任务放入到有限个数的时间轮协程进行处理。这样也可以限制协程数量。
2.不是来一个客户端连接就开一个网络协程进行处理，而是共用协程，根据机子的runtime.NumCPU进行调整，
3. 处理消息包，不是来一个请求就起一个协程，使用了协程池，目前是每个连接能同时处理2*runtime.NumCPU个请求，目前按测试结果，13个连接，处理进程协程数量维持在26个，证明了并没有用到很多协程，
## TODO

1. 优化netpack，buff使用对象池，减少gc
2. 优雅的热重启
3. 数据与逻辑分离

## 优雅的热重启
![hot_reload](img/hot_reload.jpg)
