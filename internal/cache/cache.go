package cache

import (
	"database/sql"
	"errors"
	"os"
	"syscall"
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
