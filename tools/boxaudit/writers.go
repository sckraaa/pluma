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

// tableWrite matches SQL that writes a table of the box: the box state,
// the journals (pts and seq) or the events. Only pkg/userbox writes the
// state and the journals: a journal row written outside a box transaction
// is either above the committed pts or seq (getDifference does not return
// it) or collides with the next transaction's row (UNIQUE (user_id, pts),
// PRIMARY KEY (user_id, seq)), and a counter moved outside one breaks the
// conditional UPDATE of the next. The events
// (mq_outbox, ADR 0012) are written by pkg/userbox in box transactions and
// by pkg/outbox (the relay, Insert, the operator's commands): a row written
// elsewhere could be published before the change it describes commits, or
// a decision of the consumer could be overwritten.
var tableWrite = regexp.MustCompile("(?is)\\b(insert\\s+(ignore\\s+)?into|replace\\s+into|update|delete\\s+from)\\s+`?(user_state|user_pts_updates|user_seq_updates|mq_outbox)`?\\b")

// The counters and the journal of a channel box (ADR 0034, p. 2) are
// written only by pkg/channelbox: the journal, a new row of channels (its
// counters start there), and pts or last_message_id of channels. The other
// columns of channels are written by the services, so an UPDATE of channels
// is a finding only when its SET clause names a counter.
var (
	channelJournalWrite = regexp.MustCompile("(?is)\\b(?:insert\\s+(?:(?:low_priority|delayed|high_priority|ignore)\\s+)*(?:into\\s+)?" +
		"|replace\\s+(?:(?:low_priority|delayed)\\s+)*(?:into\\s+)?" +
		"|update\\s+(?:(?:low_priority|ignore)\\s+)*" +
		"|delete\\s+(?:(?:low_priority|quick|ignore)\\s+)*(?:\\w+\\s+)?from\\s+)`?channel_pts_updates\\b`?")
	channelsInsert = regexp.MustCompile("(?is)\\b(?:insert\\s+(?:(?:low_priority|delayed|high_priority|ignore)\\s+)*(?:into\\s+)?" +
		"|replace\\s+(?:(?:low_priority|delayed)\\s+)*(?:into\\s+)?)`?channels\\b`?")
	channelsUpdate = regexp.MustCompile("(?is)\\bupdate\\s+(?:(?:low_priority|ignore)\\s+)*`?channels\\b`?(?:\\s+(?:as\\s+)?\\w+)?\\s+set\\b")
	// setClauseEnd ends the SET clause of an UPDATE.
	setClauseEnd = regexp.MustCompile(`(?is)\bwhere\b|\border\s+by\b|\blimit\b|;|</`)
	// counterSet is an assignment of a counter in a SET clause, the column
	// bare, quoted or qualified by the table or its alias.
	counterSet = regexp.MustCompile("(?is)(?:^|[\\s,])(?:`?\\w+`?\\.)?`?(?:pts|last_message_id)`?\\s*=")
)

// channelWrites returns the spans of text that write the counters or the
// journal of a channel box.
func channelWrites(text string) [][2]int {
	var spans [][2]int
	for _, re := range []*regexp.Regexp{channelJournalWrite, channelsInsert} {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			spans = append(spans, [2]int{loc[0], loc[1]})
		}
	}
	for _, loc := range channelsUpdate.FindAllStringIndex(text, -1) {
		end := len(text)
		if e := setClauseEnd.FindStringIndex(text[loc[1]:]); e != nil {
			end = loc[1] + e[0]
		}
		if counterSet.MatchString(text[loc[1]:end]) {
			spans = append(spans, [2]int{loc[0], end})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	return spans
}

// channelWriters may write the counters and the journal of a channel box.
var channelWriters = []string{"pkg/channelbox"}

// writers are the packages that may write each table, relative to the root.
var writers = map[string][]string{
	"user_state":       {"pkg/userbox"},
	"user_pts_updates": {"pkg/userbox"},
	"user_seq_updates": {"pkg/userbox"},
	"mq_outbox":        {"pkg/userbox", "pkg/outbox"},
}

// allowed tells whether the file rel may write the table of a match.
func allowed(rel string, match []string) bool {
	return inDirs(rel, writers[strings.ToLower(match[3])])
}

// inDirs tells whether the file rel is in one of the directories.
func inDirs(rel string, dirs []string) bool {
	dir := filepath.ToSlash(filepath.Dir(rel))
	for _, w := range dirs {
		if dir == w {
			return true
		}
	}
	return false
}

// Writers finds SQL that writes user_state, a journal or mq_outbox
// outside the packages allowed to, in the Go string literals and the DAO
// table definitions (*.xml) under dir. Tests, test data and third_party are
// not looked at: tests set up states on purpose, migrations are SQL files.
func Writers(dir string) ([]Finding, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var findings []Finding
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case d.Name() == "testdata", d.Name() == "third_party", path != root && strings.HasPrefix(d.Name(), "."):
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
		Msg:  "the box state, the journal and the events are written only by their packages (pkg/userbox, pkg/channelbox, pkg/outbox): " + strings.Join(strings.Fields(sql), " "),
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
		if m := tableWrite.FindStringSubmatch(s); m != nil && !allowed(rel, m) {
			findings = append(findings, writerFinding(rel, fset.Position(lit.Pos()).Line, m[0]))
		}
		if !inDirs(rel, channelWriters) {
			for _, sp := range channelWrites(s) {
				findings = append(findings, writerFinding(rel, fset.Position(lit.Pos()).Line, s[sp[0]:sp[1]]))
			}
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
	for _, loc := range tableWrite.FindAllSubmatchIndex(b, -1) {
		m := []string{string(b[loc[0]:loc[1]]), "", "", string(b[loc[6]:loc[7]])}
		if allowed(rel, m) {
			continue
		}
		line := 1 + strings.Count(string(b[:loc[0]]), "\n")
		findings = append(findings, writerFinding(rel, line, m[0]))
	}
	if !inDirs(rel, channelWriters) {
		text := string(b)
		for _, sp := range channelWrites(text) {
			line := 1 + strings.Count(text[:sp[0]], "\n")
			findings = append(findings, writerFinding(rel, line, text[sp[0]:sp[1]]))
		}
	}
	return findings, nil
}
