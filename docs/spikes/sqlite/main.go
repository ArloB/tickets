// Command sqlite-spike is throwaway Phase 0 scaffolding. It verifies the
// claims docs/spikes/README.md and the Phase 0 plan need from
// modernc.org/sqlite before ADR 0003 commits to it: WAL mode, foreign
// keys, FTS5 (including external-content tables driven by triggers and
// snippet()/bm25()), and concurrent writer/reader behavior under
// busy_timeout. Cross-compilation (assertion 7) is checked separately by
// build.sh / build.ps1 in this directory, since it is a build-time
// property, not a runtime one.
//
// Deleted, along with the rest of docs/spikes/, once both spike reports
// are PASS (Phase 0 verification item 8).
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type result struct {
	n    int
	name string
	pass bool
	note string
}

var results []result

func check(n int, name string, pass bool, note string) {
	results = append(results, result{n, name, pass, note})
	status := "PASS"
	if !pass {
		status = "FAIL"
	}
	fmt.Printf("[%d] %-70s %s  %s\n", n, name, status, note)
}

func main() {
	dir, err := os.MkdirTemp("", "sqlite-spike-*")
	if err != nil {
		fmt.Println("setup failed:", err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	dbPath := filepath.Join(dir, "spike.db")
	dsn := dbPath + "?_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Println("open failed:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1) // WAL still allows concurrent readers with one writer conn pooled separately below

	// --- 1. WAL mode ---
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		check(1, "PRAGMA journal_mode=WAL", false, err.Error())
	} else {
		check(1, "PRAGMA journal_mode=WAL", mode == "wal", "returned: "+mode)
	}

	// --- 2. foreign_keys enforcement ---
	mustExec(db, `PRAGMA foreign_keys=ON`)
	mustExec(db, `CREATE TABLE parent(id INTEGER PRIMARY KEY)`)
	mustExec(db, `CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id))`)
	_, fkErr := db.Exec(`INSERT INTO child(id, parent_id) VALUES (1, 999)`)
	check(2, "PRAGMA foreign_keys=ON rejects violating insert", fkErr != nil, fmt.Sprintf("insert err: %v", fkErr))

	// --- 3. FTS5 virtual table creation ---
	_, fts5Err := db.Exec(`CREATE VIRTUAL TABLE docs_fts USING fts5(title, body)`)
	check(3, "CREATE VIRTUAL TABLE ... USING fts5(...)", fts5Err == nil, fmt.Sprintf("%v", fts5Err))
	if fts5Err != nil {
		printSummaryAndExit()
	}

	// --- 4. MATCH query, snippet(), bm25() ---
	mustExec(db, `INSERT INTO docs_fts(rowid, title, body) VALUES (1, 'Fix the parser', 'The parser fails on empty input')`)
	mustExec(db, `INSERT INTO docs_fts(rowid, title, body) VALUES (2, 'Unrelated', 'Nothing to see here')`)
	matchOK, snippetText, err := runMatchQuery(db)
	check(4, "MATCH query + snippet() + bm25()", matchOK, fmt.Sprintf("snippet=%q err=%v", snippetText, err))

	// --- 5. external-content FTS5 table + sync triggers in one transaction ---
	extOK := runExternalContentTest(db)
	check(5, "external-content FTS5 table + triggers, transactional", extOK.pass, extOK.note)

	// --- 6. concurrent writer + reader under WAL with busy_timeout ---
	concOK := runConcurrencyTest(dsn)
	check(6, "concurrent writer+reader under WAL, busy_timeout=5000", concOK.pass, concOK.note)

	printSummaryAndExit()
}

// runMatchQuery closes its rows before returning, unlike a bare
// `defer rows.Close()` in main would, which matters here because db has
// SetMaxOpenConns(1): a connection held open by unclosed rows would
// starve every later db.Begin() call for the rest of the process.
func runMatchQuery(db *sql.DB) (ok bool, snippetText string, err error) {
	rows, err := db.Query(`SELECT rowid, snippet(docs_fts, 1, '[', ']', '...', 8), bm25(docs_fts) FROM docs_fts WHERE docs_fts MATCH 'parser' ORDER BY rank`)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, "", rows.Err()
	}
	var rowid int
	var bm25 float64
	if err := rows.Scan(&rowid, &snippetText, &bm25); err != nil {
		return false, "", err
	}
	return true, snippetText, rows.Err()
}

func mustExec(db *sql.DB, query string) {
	if _, err := db.Exec(query); err != nil {
		fmt.Printf("setup statement failed (%q): %v\n", query, err)
		os.Exit(1)
	}
}

func runExternalContentTest(db *sql.DB) result {
	tx, err := db.Begin()
	if err != nil {
		return result{pass: false, note: err.Error()}
	}
	defer func() { _ = tx.Rollback() }()

	stmts := []string{
		`CREATE TABLE items(id INTEGER PRIMARY KEY, title TEXT, body TEXT)`,
		`CREATE VIRTUAL TABLE items_fts USING fts5(title, body, content='items', content_rowid='id')`,
		`CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
			INSERT INTO items_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
		END`,
		`CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
			INSERT INTO items_fts(items_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
		END`,
		`INSERT INTO items(id, title, body) VALUES (1, 'Fix the parser', 'Parser edge cases')`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return result{pass: false, note: fmt.Sprintf("stmt failed (%q): %v", s, err)}
		}
	}

	var cnt int
	if err := tx.QueryRow(`SELECT count(*) FROM items_fts WHERE items_fts MATCH 'parser'`).Scan(&cnt); err != nil {
		return result{pass: false, note: err.Error()}
	}
	if cnt != 1 {
		return result{pass: false, note: fmt.Sprintf("expected 1 match inside transaction, got %d", cnt)}
	}

	if _, err := tx.Exec(`DELETE FROM items WHERE id=1`); err != nil {
		return result{pass: false, note: "delete trigger failed: " + err.Error()}
	}
	if err := tx.QueryRow(`SELECT count(*) FROM items_fts WHERE items_fts MATCH 'parser'`).Scan(&cnt); err != nil {
		return result{pass: false, note: err.Error()}
	}
	if cnt != 0 {
		return result{pass: false, note: fmt.Sprintf("expected 0 matches after delete-trigger sync, got %d", cnt)}
	}

	if err := tx.Commit(); err != nil {
		return result{pass: false, note: "commit failed: " + err.Error()}
	}
	return result{pass: true, note: "insert+delete sync triggers observed correctly inside one transaction"}
}

func runConcurrencyTest(dsn string) result {
	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return result{pass: false, note: err.Error()}
	}
	defer func() { _ = writer.Close() }()
	writer.SetMaxOpenConns(1)

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		return result{pass: false, note: err.Error()}
	}
	defer func() { _ = reader.Close() }()

	if _, err := writer.Exec(`CREATE TABLE IF NOT EXISTS conc(id INTEGER PRIMARY KEY, v INTEGER)`); err != nil {
		return result{pass: false, note: err.Error()}
	}

	const n = 200
	var wg sync.WaitGroup
	errs := make(chan error, n*2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := writer.Exec(`INSERT INTO conc(v) VALUES (?)`, i); err != nil {
				errs <- fmt.Errorf("write %d: %w", i, err)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			var cnt int
			if err := reader.QueryRow(`SELECT count(*) FROM conc`).Scan(&cnt); err != nil {
				errs <- fmt.Errorf("read %d: %w", i, err)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	close(errs)

	var failures []error
	for e := range errs {
		failures = append(failures, e)
	}
	if len(failures) > 0 {
		return result{pass: false, note: fmt.Sprintf("%d errors, first: %v", len(failures), failures[0])}
	}
	return result{pass: true, note: fmt.Sprintf("%d concurrent writes + %d concurrent reads, no SQLITE_BUSY", n, n)}
}

func printSummaryAndExit() {
	fail := 0
	for _, r := range results {
		if !r.pass {
			fail++
		}
	}
	fmt.Printf("\n%d/%d assertions passed\n", len(results)-fail, len(results))
	if fail > 0 {
		os.Exit(1)
	}
}
