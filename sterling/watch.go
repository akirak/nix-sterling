package main

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 200 * time.Millisecond

// watchFile reports writes to path on the returned channel. It watches the
// containing directory rather than the file itself so that editors that save
// by renaming a temporary file over the model continue to be noticed.
func watchFile(path string) (<-chan struct{}, func(), error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}
	if err := watcher.Add(filepath.Dir(absolute)); err != nil {
		_ = watcher.Close()
		return nil, nil, err
	}

	changes := make(chan struct{}, 1)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }

	go func() {
		defer watcher.Close()

		var timer *time.Timer
		var timerC <-chan time.Time
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		for {
			select {
			case <-done:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(event.Name) != absolute {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove|fsnotify.Chmod) == 0 {
					continue
				}

				// Coalesce the burst of events produced by a single save.
				if timer == nil {
					timer = time.NewTimer(watchDebounce)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(watchDebounce)
				}
				timerC = timer.C
			case <-timerC:
				timerC = nil
				select {
				case changes <- struct{}{}:
				default:
					// A reload is already pending; coalesce further changes.
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return changes, stop, nil
}
