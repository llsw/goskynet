package log

import (
	"fmt"
	"os"

	utils "github.com/llsw/goskynet/lib/utils"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var (
	level = hlog.LevelDebug
)

type Log struct {
	logFile *os.File
	level   hlog.Level
}

func logInterval(interval int64) (int64, string) {
	tail := utils.Now2DateWithoutSpace()
	if interval == 24*3600 {
		// 每天一个文件
		interval = utils.GetNextDateSec()
		tail = utils.Now2Day()
	} else if interval == 3600 {
		// 每小时一个文件
		interval = utils.GetNextDateSec()
		tail = utils.Now2DateWithoutSpace()
	}
	return interval, tail
}

func (l *Log) generatedLog(path string, interval int64) {
	_, day := logInterval(interval)
	fileName := fmt.Sprintf("%s_%s.log", path, day)
	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		hlog.Errorf("log file:%s creae error: ", fileName, err.Error())
	} else {
		hlog.SetOutput(f)
		oldFile := l.logFile
		l.logFile = f
		hlog.Infof("generatedLog %s", fileName)
		go func() {
			nextHourSec, _ := logInterval(interval)
			utils.DelayFunc(nextHourSec, func() {
				l.generatedLog(path, interval)
			})
		}()

		// 延迟关闭文件, 防止日志没写完
		go func() {
			utils.DelayFunc(120, func() {
				if oldFile != nil {
					oldFile.Close()
				}
			})
		}()
	}
}

func InitLog(path interface{}, lv hlog.Level, interval int64) (l *Log) {
	hlog.SetLevel(lv)
	level = lv
	l = &Log{}
	switch v := path.(type) {
	case string:
		if v != "" {
			l.generatedLog(path.(string), interval)
			go func() {
				nextHourSec, _ := logInterval(interval)
				utils.DelayFunc(nextHourSec, func() {
					l.generatedLog(path.(string), interval)
				})
			}()
		}
	}
	return
}

func GetLogLevel() hlog.Level {
	return level
}

func (l *Log) Close() {
	if l.logFile != nil {
		l.logFile.Close()
	}
}
