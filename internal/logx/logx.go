package logx

import (
	"fmt"
	"io"
)

type Logger struct {
	Out     io.Writer
	Verbose bool
}

func (l Logger) Infof(format string, args ...any) {
	fmt.Fprintf(l.Out, format+"\n", args...)
}

func (l Logger) Debugf(format string, args ...any) {
	if !l.Verbose {
		return
	}
	fmt.Fprintf(l.Out, format+"\n", args...)
}
