package boxaudit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// stateWrite matches SQL that writes the box state or the journal. Only
// pkg/userbox may: a journal row written outside a box transaction is either
// above the committed pts (getDifference does not return it) or collides
// with the next transaction's row (UNIQUE (user_id, pts)), and a counter
// moved outside one breaks the conditional UPDATE of the next.
var stateWrite = regexp.MustCompile("(?is)\\b(insert\\s+(ignore\\s+)?into|replace\\s+into|update|delete\\s+from)\\s+`?(user_state|user_pts_updates)`?\\b")

// Writers finds SQL that writes user_state or user_pts_updates outside
// pkg/userbox, in the Go string literals and the DAO table definitions
// (*.xml) under dir. Tests, test data and third_party are not looked at:
// tests set up states on purpose, migrations are SQL files.
func Writers(dir string) ([]Finding, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	userbox := filepath.Join(root, "pkg", "userbox")
	fset := token.NewFileSet()
	var findings []Finding
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case path == userbox, d.Name() == "testdata", d.Name() == "third_party", path != root && strings.HasPrefix(d.Name(), "."):
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case strings.HasSuffix(path, "_test.go"):
		case strings.HasSuffix(path, ".go"):
			found, err := goWriters(fset, path, rel)
			if err != nil {
				return err
			}
			findings = append(findings, found...)
		case strings.HasSuffix(path, ".xml"):
			found, err := textWriters(path, rel)
			if err != nil {
				return err
			}
			findings = append(findings, found...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i].Pos, findings[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Line < b.Line
	})
	return findings, nil
}

func writerFinding(file string, line int, sql string) Finding {
	return Finding{
		Pos:  token.Position{Filename: file, Line: line},
		Rule: "writer",
		Msg:  "the box state and the journal are written only by pkg/userbox: " + strings.Join(strings.Fields(sql), " "),
	}
}

func goWriters(fset *token.FileSet, path, rel string) ([]Finding, error) {
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if m := stateWrite.FindString(s); m != "" {
			findings = append(findings, writerFinding(rel, fset.Position(lit.Pos()).Line, m))
		}
		return true
	})
	return findings, nil
}

func textWriters(path, rel string) ([]Finding, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, loc := range stateWrite.FindAllIndex(b, -1) {
		line := 1 + strings.Count(string(b[:loc[0]]), "\n")
		findings = append(findings, writerFinding(rel, line, string(b[loc[0]:loc[1]])))
	}
	return findings, nil
}
