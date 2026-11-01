package cli

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/snapshot"
	"github.com/ecosyste-ms/search/internal/web"
)

func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: search snapshot|explore [flags]")
	}
	if args[0] == "snapshot" {
		return snapshot.Command(args[1:], out)
	}
	if args[0] != "explore" {
		return fmt.Errorf("unknown command %q; use snapshot or explore", args[0])
	}
	flags := flag.NewFlagSet("explore", flag.ContinueOnError)
	index := flags.String("index", "", "Bleve index directory")
	catalog := flags.String("catalog", "", "public serving catalogue directory")
	addr := flags.String("addr", "127.0.0.1:8080", "HTTP listen address")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *index == "" || *catalog == "" {
		return fmt.Errorf("explore requires -index and -catalog")
	}
	e, err := search.Open(*index, *catalog)
	if err != nil {
		return err
	}
	defer func() { _ = e.Close() }()
	_, _ = fmt.Fprintf(out, "Project search: http://%s/\n", *addr)
	server := &http.Server{Addr: *addr, Handler: web.Handler(e), ReadHeaderTimeout: 5 * time.Second}
	return server.ListenAndServe()
}
