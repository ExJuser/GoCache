package GoCache

import (
	"log"
	"os"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

var _ Logger = &log.Logger{}

func DefaultLogger() *log.Logger {
	return log.New(os.Stdout, "", log.LstdFlags)
}

func newLogger(custom Logger) Logger {
	if custom != nil {
		return custom
	}
	return DefaultLogger()
}
