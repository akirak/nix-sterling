package main

import (
	"os"
	"sync"
	"time"
)

const (
	watchPollInterval = 300 * time.Millisecond
	watchSettleDelay  = 200 * time.Millisecond
	watchSettleLimit  = 2 * time.Second
)

// fileSnapshot is the subset of file metadata used to notice edits. Editors
// commonly replace a file atomically, so both the modification time and size
// are tracked.
type fileSnapshot struct {
	exists  bool
	modTime time.Time
	size    int64
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSnapshot{}, nil
		}
		return fileSnapshot{}, err
	}
	return fileSnapshot{
		exists:  true,
		modTime: info.ModTime(),
		size:    info.Size(),
	}, nil
}

// watchFile reports writes to path on the returned channel. It polls instead of
// relying on a platform-specific notification API so the launcher remains a
// dependency-free static binary. A change is reported only after the file has
// stopped changing, which avoids validating (and restarting on) a partially
// written file.
func watchFile(path string) (<-chan struct{}, func(), error) {
	initial, err := snapshotFile(path)
	if err != nil {
		return nil, nil, err
	}

	changes := make(chan struct{}, 1)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(watchPollInterval)
		defer ticker.Stop()

		last := initial
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}

			current, err := snapshotFile(path)
			if err != nil || current == last {
				continue
			}

			// Wait until the writer has finished before considering the file
			// changed. The limit keeps a continuously-written file (or a
			// process that never closes it) from blocking reloads forever.
			deadline := time.Now().Add(watchSettleLimit)
			for time.Now().Before(deadline) {
				select {
				case <-done:
					return
				case <-time.After(watchSettleDelay):
				}

				next, err := snapshotFile(path)
				if err != nil {
					break
				}
				if next == current {
					break
				}
				current = next
			}

			if current == last {
				continue
			}
			last = current

			select {
			case changes <- struct{}{}:
			default:
				// A reload is already pending; coalesce further changes.
			}
		}
	}()

	return changes, stop, nil
}
