package livetv

import (
	"io"

	"bingebandit/internal/stream"
)

// readShared reads a file ffmpeg may be renaming over (see stream.OpenShared).
func readShared(path string) ([]byte, error) {
	f, err := stream.OpenShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
