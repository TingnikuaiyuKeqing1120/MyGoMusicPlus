// Command probe exercises MyGo's production updater against built release
// files. The workflow builds this disposable old executable in a private
// directory; successful installation replaces it with the actual player.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

type files struct {
	manifest, archive string
	bad               bool
}

func (f files) Manifest(context.Context, bool) (io.ReadCloser, error) {
	b, err := os.ReadFile(f.manifest)
	if err != nil {
		return nil, err
	}
	if f.bad {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		m["signature"] = "invalid"
		b, err = json.Marshal(m)
		if err != nil {
			return nil, err
		}
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f files) Open(context.Context, string) (io.ReadCloser, error) { return os.Open(f.archive) }

func main() {
	if len(os.Args) != 3 {
		log.Fatal("expected manifest and archive")
	}
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	rel, err := filepath.Rel(os.Getenv("PLUS_PROBE_ROOT"), exe)
	if err != nil || !filepath.IsLocal(rel) || !strings.HasPrefix(filepath.Base(os.Getenv("PLUS_PROBE_ROOT")), "mygo-plus-probe-") {
		log.Fatal("probe must run inside its own temporary installation")
	}
	if !mygo.Updater.Enabled() {
		log.Fatal("updater disabled in writable fixture")
	}
	mygo.App.SetVersion("0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, bad := range []bool{true, false} {
		mygo.Updater.SetSource(files{os.Args[1], os.Args[2], bad})
		u, err := mygo.Updater.Check(ctx)
		if err != nil || u == nil {
			log.Fatalf("missing update: %v", err)
		}
		err = u.Install(ctx, nil)
		if bad {
			if err == nil || !strings.Contains(err.Error(), "not signed with the app's key") {
				log.Fatalf("invalid signature: %v", err)
			}
		} else if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("Rejected an invalid signature and installed the signed update.")
}
