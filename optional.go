package retryfs

import (
	"io/fs"
	"time"

	"github.com/go-git/go-billy/v5"
)

// Chmod implements billy.Change
func (rfs *RetryFS) Chmod(name string, mode fs.FileMode) error {
	changer, ok := rfs.fs.(billy.Change)
	if !ok {
		return &fs.PathError{Op: "chmod", Path: name, Err: fs.ErrInvalid}
	}

	return rfs.retry(OpChmod, func() error {
		return changer.Chmod(name, mode)
	})
}

// Chtimes implements billy.Change
func (rfs *RetryFS) Chtimes(name string, atime, mtime time.Time) error {
	changer, ok := rfs.fs.(billy.Change)
	if !ok {
		return &fs.PathError{Op: "chtimes", Path: name, Err: fs.ErrInvalid}
	}

	return rfs.retry(OpChtimes, func() error {
		return changer.Chtimes(name, atime, mtime)
	})
}

// Lchown implements billy.Change
func (rfs *RetryFS) Lchown(name string, uid, gid int) error {
	changer, ok := rfs.fs.(billy.Change)
	if !ok {
		return &fs.PathError{Op: "lchown", Path: name, Err: fs.ErrInvalid}
	}

	return rfs.retry(OpLchown, func() error {
		return changer.Lchown(name, uid, gid)
	})
}

// Chown implements billy.Change
func (rfs *RetryFS) Chown(name string, uid, gid int) error {
	changer, ok := rfs.fs.(billy.Change)
	if !ok {
		return &fs.PathError{Op: "chown", Path: name, Err: fs.ErrInvalid}
	}

	return rfs.retry(OpChown, func() error {
		return changer.Chown(name, uid, gid)
	})
}
