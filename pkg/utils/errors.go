package utils

import (
	stdlog "log"
)

// customization points
var fatalf = stdlog.Fatalf // print fatal message
var printf = stdlog.Printf // print simple message

// IgnoreError simple helper that just prints error to log and ignores it
func IgnoreError(err error) {
	if err != nil { // unlikely
		printf("ERROR IGNORED: %s", err)
	}
}

// IgnoreErrorOn simple helper that is aimed to use with `defer`
func IgnoreErrorOn(f func() error) {
	IgnoreError(f())
}

// FatalOnError simple helper that just prints error to logs and calls os.Exit(1)
func FatalOnError(err error) {
	if err != nil { // unlikely
		fatalf("ERROR: %s", err) // os.Exit(1)
	}
}

// PanicOnError simple helper that panic on non-nil error
func PanicOnError(err error) {
	if err != nil { // unlikely
		panic(err)
	}
}

// FatalOnPanic does simple panic recover that is aimed to use with `defer`
// On panic it prints the error message to standard log and calls os.Exit(1)
func FatalOnPanic() {
	if err := recover(); err != nil {
		fatalf("UNHANDLED PANIC: %v", err) // os.Exit(1)
	}
}
