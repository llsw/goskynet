package actor

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	_ "github.com/go-sql-driver/mysql"
	share "github.com/llsw/goskynet/lib/share"
	utils "github.com/llsw/goskynet/lib/utils"
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
	}

	MySQL struct {
		db    *gorm.DB
		sqlDb *sql.DB
		req   share.ReqChan
		conf  *MySQLConf
		curds map[string]*share.Method
	}
)

// ===必须实现===
func (act *MySQL) Init(name string, pid *actor.PID) (err error) {
	return
}

func (act *MySQL) Start(name string, pid *actor.PID) {
}

func (act *MySQL) Stop(name string, pid *actor.PID) (err error) {
	return
}

// ===必须实现===

// ===自定义消息处理方法===
type User struct {
	ID       int
	Code     int
	UserName string
	NickName string
}

func (act *MySQL) dispatch() {
	for a := range act.req {
		res := a.Act()
		a.Res <- res
	}
}

func (act *MySQL) Call(curdName string,
	args ...interface{}) (resChan share.ResChan) {
	resChan = make(share.ResChan)
	var crud share.Act = func() (res *share.Res) {
		defer utils.Recover(func(err error) {
			done := &share.Res{Err: err}
			resChan <- done
		})
		if f, ok := act.curds[curdName]; ok {
			l := len(args) + 2
			wrap := make([]interface{}, l)
			wrap[0] = act.db
			wrap[1] = act.sqlDb
			for i := 2; i < l; i++ {
				wrap[i] = args[i-2]
			}
			res = f.Call(wrap...)
		} else {
			res = &share.Res{
				Err: fmt.Errorf("curd:%s not found", curdName),
			}
		}
		return
	}
	action := &share.Req{
		Act: crud,
		Res: resChan,
	}
	act.req <- action

	// u := model.User{}
	// filds := []string{"ID", "code", "userName", "nickName"}
	// // 这种方式会报错sql: expected 0 arguments, go 3
	// // Select("ID", "code", "userName", "nickName")
	// if err := act.db.Select(filds).Take(&u).Error; err != nil {
	// 	hlog.Errorf("call ")
	// 	return
	// }
	// hlog.Debugf("user:%v", &u)
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

func NewMySQLService(svcName string, conf *MySQLConf, receiver interface{}) {
	for i := 0; i < conf.ReplicaSet; i++ {
		db, sqlDb := open(conf)
		svc := &MySQL{
			db:    db,
			sqlDb: sqlDb,
			conf:  conf,
			req:   make(share.ReqChan, conf.Parallel),
			curds: utils.Register(receiver),
		}
		skynet.NewService(svcName, svc)
	}
}
