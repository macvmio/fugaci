package ctxio

import (
	"context"
	"io"
	"os"

	"github.com/fsnotify/fsnotify"
)

type TailReader struct {
	ctx      context.Context
	file     *os.File
	watcher  *fsnotify.Watcher
	filename string
}

func NewTailReader(ctx context.Context, filename string) (*TailReader, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Ensure the watcher is closed if an error occurs
	defer func() {
		if err != nil {
			watcher.Close()
		}
	}()

	err = watcher.Add(filename)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return &TailReader{
		ctx:      ctx,
		file:     file,
		watcher:  watcher,
		filename: filename,
	}, nil
}

func (tr *TailReader) Read(p []byte) (int, error) {
	for {
		select {
		case <-tr.ctx.Done():
			return 0, tr.ctx.Err()
		default:
			n, err := tr.file.Read(p)
			if err == io.EOF {
				// Wait for a write event
				select {
				case event := <-tr.watcher.Events:
					if event.Op&fsnotify.Write == fsnotify.Write {
						// Continue reading after write event
						continue
					}
					// Handle file truncation (e.g., log rotation)
					if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
						// Reopen the file
						tr.file.Close()
						file, err := os.Open(tr.filename)
						if err != nil {
							return 0, err
						}
						tr.file = file
						continue
					}
				case err := <-tr.watcher.Errors:
					return 0, err
				case <-tr.ctx.Done():
					return 0, tr.ctx.Err()
				}
			}
			return n, err
		}
	}
}

func (tr *TailReader) Close() error {
	var err1, err2 error
	if tr.watcher != nil {
		err1 = tr.watcher.Close()
	}
	if tr.file != nil {
		err2 = tr.file.Close()
	}
	if err1 != nil {
		return err1
	}
	return err2
}
