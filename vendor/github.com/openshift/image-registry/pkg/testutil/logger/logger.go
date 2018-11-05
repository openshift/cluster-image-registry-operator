package logger

import (
	"fmt"
	"reflect"
)

type Logger interface {
	Reset()
	Printf(format string, v ...interface{})
	Compare(want []string) error
}

func New() Logger {
	return &logger{}
}

type logger struct {
	records []string
}

func (l *logger) Reset() {
	l.records = nil
}

func (l *logger) Printf(format string, v ...interface{}) {
	l.records = append(l.records, fmt.Sprintf(format, v...))
}

func (l *logger) Compare(want []string) error {
	if len(l.records) == 0 && len(want) == 0 {
		return nil
	}
	if !reflect.DeepEqual(l.records, want) {
		return fmt.Errorf("got %#+v, want %#+v", l.records, want)
	}
	return nil
}
