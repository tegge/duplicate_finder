package cache

import (
	"database/sql"
	"errors"
	"os"
	"syscall"
	"time"
)

func StatInode(path string) (uint64, int64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, errors.New("unsupported stat type")
	}
	return st.Ino, info.ModTime().Unix(), info.Size(), nil
}

func TryDB(db *sql.DB, path string) (string, bool) {
	row := db.QueryRow(`SELECT inode,mtime,size,hash FROM files WHERE path = ?`, path)

	var inodeDB uint64
	var mtimeDB int64
	var sizeDB int64
	var hashDB string

	if err := row.Scan(&inodeDB, &mtimeDB, &sizeDB, &hashDB); err != nil {
		return "", false
	}

	inode, mtime, size, err := StatInode(path)
	if err != nil {
		return "", false
	}

	if inode == inodeDB && mtime == mtimeDB && size == sizeDB {
		return hashDB, true
	}
	return "", false
}

func UpdateDB(db *sql.DB, path, hashVal string) {
	inode, mtime, size, err := StatInode(path)
	if err != nil {
		return
	}
	_, _ = db.Exec(
		`INSERT OR REPLACE INTO files(path,inode,mtime,size,hash) VALUES(?,?,?,?,?)`,
		path, inode, mtime, size, hashVal,
	)
}

// Configure sets WAL journal mode and tuning pragmas on db.
// Call once after opening the database.
func Configure(db *sql.DB) {
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA cache_size=10000`,
		`PRAGMA temp_store=MEMORY`,
	} {
		_, _ = db.Exec(pragma)
	}
}

// CacheEntry is a pending hash→file mapping to be written by RunBatchWriter.
type CacheEntry struct {
	Path string
	Hash string
}

// RunBatchWriter drains entries from ch and writes them to db in batched
// transactions (up to batchSize rows per commit, or every flushInterval).
// Returns when ch is closed.
func RunBatchWriter(db *sql.DB, ch <-chan CacheEntry) {
	const batchSize = 200
	const flushInterval = 2 * time.Second

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	buf := make([]CacheEntry, 0, batchSize)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		tx, err := db.Begin()
		if err != nil {
			buf = buf[:0]
			return
		}
		stmt, err := tx.Prepare(`INSERT OR REPLACE INTO files(path,inode,mtime,size,hash) VALUES(?,?,?,?,?)`)
		if err != nil {
			_ = tx.Rollback()
			buf = buf[:0]
			return
		}
		for _, e := range buf {
			inode, mtime, size, sErr := StatInode(e.Path)
			if sErr != nil {
				continue
			}
			_, _ = stmt.Exec(e.Path, inode, mtime, size, e.Hash)
		}
		_ = stmt.Close()
		_ = tx.Commit()
		buf = buf[:0]
	}

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				flush()
				return
			}
			buf = append(buf, e)
			if len(buf) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
