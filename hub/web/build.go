package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sync"
)

// buildIDLen is how many hex chars of the digest are exposed. 12 chars
// (48 bits) is plenty to distinguish builds.
const buildIDLen = 12

var (
	buildOnce sync.Once
	buildID   string
)

// BuildID returns a short fingerprint of the embedded UI assets.
//
// The assets are compiled into the binary, so this changes exactly when the
// front-end shipped in the binary changes — no manual bump and no git commit
// needed. It is exposed on /api/health to confirm which build is running.
func BuildID() string {
	buildOnce.Do(func() {
		h := sha256.New()
		// WalkDir visits entries in lexically sorted order, so the digest is
		// stable across builds of the same tree.
		_ = fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(FS, path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00%d\x00", path, len(data))
			h.Write(data)
			return nil
		})
		buildID = hex.EncodeToString(h.Sum(nil))[:buildIDLen]
	})
	return buildID
}
