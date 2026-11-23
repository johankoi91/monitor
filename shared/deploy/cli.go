package deploy

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func FlagSet(name string, args []string, configure func(*flag.FlagSet) func() error) error {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	run := configure(f)
	if e := f.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	return run()
}
func Usage(w io.Writer, tool string, commands []string) {
	fmt.Fprintf(w, "Usage: %s <%s> [options]\n", tool, strings.Join(commands, "|"))
}
