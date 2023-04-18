package actor

import (
	"database/sql"
	"fmt"
	"reflect"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	_ "github.com/go-sql-driver/mysql"
	share "github.com/llsw/goskynet/lib/share"
	utils "github.com/llsw/goskynet/lib/utils"
	"github.com/pkg/errors"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type (
	MySQLConf struct {
		Host         string `yaml:"host"`
		Port         int    `yaml:"port"`
		Database     string `yaml:"database"`
		User         string `yaml:"user"`
		Password     string `yaml:"password"`
		MaxOpenConns int    `yaml:"maxOpenConns"` // 最大连接数
		MaxIdleConns int    `yaml:"maxIdleConns"` // 最大空闲连接
		MaxLifetime  int    `yaml:"maxLifetime"`  // mysql timeout 单位分钟，要小于mysql设置的time_out
		ReplicaSet   int    `yaml:"replicaSet"`   // 副本数，一个db开多少个service
		Parallel     int    `yaml:"parallel"`     // 每个service同时执行多少个sql
		SqlTimeout   int    `yaml:"sqlTimeout"`   // sql执行超时，单位秒
	}

	MySQL struct {
		db    *gorm.DB
		sqlDb *sql.DB
		req   share.ReqChan
		conf  *MySQLConf
		cruds map[string]map[string]*share.Method
		gp    *share.GroutinePool
	}
	MySQLCrud func(db *gorm.DB, sqlDb *sql.DB, args ...interface{}) *share.Res
)

// ===必须实现===
func (act *MySQL) Init(name string, pid *actor.PID) (err error) {
	return
}

func (act *MySQL) Start(name string, pid *actor.PID) {
	act.Open()
}

func (act *MySQL) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===

func (act *MySQL) dispatch() {
	for a := range act.req {
		// 设置执行超时要换成goroutine,
		// 如果并发很高的话groutine太多
		// 可以使用goroutine对象池
		// 在池子里面获取到goroutine再开启goroutine
		// 这样能防止高并发下groutine暴涨
		act.gp.Job(act.conf.SqlTimeout, func(args ...interface{}) {
			a := args[0].(*share.Req)
			a.Res <- a.Act()
		}, func(err error, args ...interface{}) {
			a := args[0].(*share.Req)
			a.Res <- &share.Res{
				Err: errors.Wrap(err, "crud error"),
			}
		}, a)
	}
}

// func (act *MySQL) Demo() {
// u := model.User{}
// filds := []string{"ID", "code", "userName", "nickName"}
// // 这种方式会报错sql: expected 0 arguments, go 3
// // Select("ID", "code", "userName", "nickName")
// if err := act.db.Select(filds).Take(&u).Error; err != nil {
// 	hlog.Errorf("call ")
// 	return
// }
// hlog.Debugf("user:%v", &u)
// }

func (act *MySQL) Call(dao string, crud string,
	args ...interface{}) (resChan share.ResChan) {
	resChan = make(share.ResChan)
	var cb share.Act = func() (res *share.Res) {
		defer share.Recover(func(err error) {
			res = &share.Res{Err: err}
			// resChan <- res
		})
		if d, ok := act.cruds[dao]; ok {
			if f, ok := d[crud]; ok {
				l := len(args)
				wrap := make([]interface{}, l+3)
				wrap[0] = act
				wrap[1] = act.db
				wrap[2] = act.sqlDb
				for i := 0; i < l; i++ {
					wrap[i+3] = args[i]
				}
				res = f.Call(wrap...)
			} else {
				res = &share.Res{
					Err: fmt.Errorf("crud:%s not found", crud),
				}
			}
		} else {
			res = &share.Res{
				Err: fmt.Errorf("dao:%s not found", dao),
			}
		}
		return
	}
	action := &share.Req{
		Act: cb,
		Res: resChan,
	}
	act.req <- action
	return
}

func (act *MySQL) Open() {
	for i := 0; i < act.conf.Parallel; i++ {
		go act.dispatch()
	}
}

func open(conf *MySQLConf) (db *gorm.DB, sqlDb *sql.DB) {
	var err error
	dialector := mysql.Open(fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?"+
			"charset=utf8mb4&parseTime=True&loc=Local",
		conf.User, conf.Password,
		conf.Host, conf.Port, conf.Database,
	))
	db, err = gorm.Open(
		dialector,
		&gorm.Config{
			SkipDefaultTransaction: true,
			PrepareStmt:            true,
		},
	)
	if err != nil {
		hlog.Fatalf("mysql init failed, err: ", err)
		return
	}
	sqlDb, err = db.DB()
	if err != nil {
		hlog.Fatalf("mysql get db failed, err: ", err)
		return
	}
	sqlDb.SetMaxOpenConns(conf.MaxOpenConns)
	sqlDb.SetMaxIdleConns(conf.MaxIdleConns)
	// mysql default conn timeout=8h, should < mysql_timeout
	sqlDb.SetConnMaxLifetime(
		time.Minute * time.Duration(conf.MaxLifetime),
	)
	err = sqlDb.Ping()
	if err != nil {
		hlog.Fatalf("mysql ping failed, err: ", err)
	}
	hlog.Infof("mysql conn pool has initiated.")
	return
}

func NewMySQLService(svcName string, conf *MySQLConf, receiver ...interface{}) {
	for i := 0; i < conf.ReplicaSet; i++ {
		db, sqlDb := open(conf)
		cruds := make(map[string]map[string]*share.Method)

		for _, v := range receiver {
			val := reflect.Indirect(reflect.ValueOf(v))
			dao := val.Type().Name()
			cruds[dao] = utils.Register(v)
		}

		svc := &MySQL{
			db:    db,
			sqlDb: sqlDb,
			conf:  conf,
			req:   make(share.ReqChan, conf.Parallel),
			cruds: cruds,
			gp:    share.GreateGroutinePool(10),
		}
		skynet.NewService(svcName, svc)
	}
}
