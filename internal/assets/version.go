package assets

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const shortHashLength = 12

// Version returns a short hash representing the paths and contents of the provided asset files.
//
// Each file contributes its length-prefixed path followed by the SHA-256 digest of its contents.
// The digest is fixed width, so no split of bytes between files can collide with another.
func Version(paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no asset paths provided")
	}

	hasher := sha256.New()
	for _, path := range paths {
		digest, err := fileDigest(path)
		if err != nil {
			return "", err
		}

		var pathLen [8]byte
		binary.BigEndian.PutUint64(pathLen[:], uint64(len(path)))
		hasher.Write(pathLen[:])
		hasher.Write([]byte(path))
		hasher.Write(digest)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	return sum[:shortHashLength], nil
}

func fileDigest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	fileHasher := sha256.New()
	_, copyErr := io.Copy(fileHasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("read %s: %w", path, copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s: %w", path, closeErr)
	}

	return fileHasher.Sum(nil), nil
}
