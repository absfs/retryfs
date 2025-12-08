package retryfs

import (
	"github.com/absfs/absfs"
	"github.com/absfs/memfs"
)

// mustNewMemFS creates a new memfs or panics on error (helper for tests)
func mustNewMemFS() absfs.FileSystem {
	fs, err := memfs.NewFS()
	if err != nil {
		panic(err)
	}
	return fs
}
