package devserver

import (
	"fmt"
	"log"
	"os"
	"time"
)

// ANSI colours, used only when the output is a terminal.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[91m"
	colorGreen  = "\033[92m"
	colorYellow = "\033[93m"
	colorCyan   = "\033[96m"
)

// Log tags. A dev server's output is skimmed, not read, so every line leads
// with what kind of thing happened.
const (
	tagStart   = "START"
	tagBuild   = "BUILD"
	tagWatch   = "WATCH"
	tagServer  = "SERVER"
	tagInfo    = "INFO"
	tagNew     = "NEW"
	tagChanged = "CHANGED"
	tagDeleted = "DELETED"
	tagReload  = "RELOAD"
	tagClient  = "CLIENT"
	tagError   = "ERROR"
	tagStop    = "STOP"
)

// logger writes "[15:04:05] [TAG] message" lines.
type logger struct {
	out   *log.Logger
	color bool
}

// newLogger wraps out, deciding on colour. A caller-supplied logger keeps its
// own flags and prefix, so it can slot into whatever logging a project already
// has; the default one owns the whole line.
func newLogger(out *log.Logger, noColor bool) *logger {
	if out == nil {
		out = log.New(os.Stderr, "", 0)
	}
	return &logger{out: out, color: !noColor && isTerminal(out)}
}

func (l *logger) event(color, tag, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("15:04:05")
	if l.color {
		l.out.Printf("%s[%s] [%s]%s %s", color, ts, tag, colorReset, msg)
		return
	}
	l.out.Printf("[%s] [%s] %s", ts, tag, msg)
}

// isTerminal reports whether writing to out lands on a terminal, so colour
// codes don't end up in a log file or a CI transcript. NO_COLOR is honoured
// because it is the convention every other tool honours.
func isTerminal(out *log.Logger) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	f, ok := out.Writer().(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
