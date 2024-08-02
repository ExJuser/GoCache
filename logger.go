package GoCache

import (
	"log"
	"os"
)

// Logger 当配置中的 `Config.Verbose=true` 时触发日志记录
type Logger interface {
	Printf(format string, v ...interface{})
}

var _ Logger = &log.Logger{}

func DefaultLogger() *log.Logger {
	return log.New(os.Stdout, "", log.LstdFlags)
}

// 传入自定义Logger
func newLogger(custom Logger) Logger {
	if custom != nil {
		return custom
	}
	return DefaultLogger()
}
