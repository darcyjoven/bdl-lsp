package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sort"
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

func TestTmpRecordScopes(t *testing.T) {
	root := "/code/workspace/tiptop-gp"
	path := root + "/tiptop/ghr/4gl/ghrr034.4gl"
	pool, _ := parser.New()
	defer pool.Close()
	srv := New(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	srv.rootPath = root
	srv.cfg = config.NewManager("file://" + root)
	srv.loadSchemaIndex()
	src, _ := os.ReadFile(path)
	doc := srv.docs.Open("file://"+path, parser.LangBDL, 1, string(src))
	srv.reparseFull(context.Background(), doc)
	fi, _ := srv.indexFor(doc)
	scopes := srv.buildRecordScopes(fi)
	for ctr, sc := range scopes {
		for v, members := range sc.vars {
			if v != "sr1" && v != "sr" && v != "tm" {
				continue
			}
			keys := make([]string, 0, len(members))
			for m := range members {
				keys = append(keys, m)
			}
			sort.Strings(keys)
			t.Logf("scope=%q var=%q type=%q n=%d members=%v", ctr, v, sc.types[v], len(keys), keys)
		}
	}
}
