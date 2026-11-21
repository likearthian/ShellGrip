//go:build linux

// wslwatch relays inotify changes until its parent's input closes.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const watchMask = unix.IN_CLOSE_WRITE | unix.IN_ATTRIB | unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF

type relay struct {
	fd           int
	single       bool
	root, filter string
	dirs         map[int]string
	paths        map[string]int
}

func (r *relay) add(root string) error {
	return filepath.WalkDir(root, func(name string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !e.IsDir() {
			return nil
		}
		if name != root && e.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if _, ok := r.paths[name]; !ok {
			if len(r.dirs) >= 8192 {
				return fmt.Errorf("inotify directory limit exceeded (8192)")
			}
			wd, err := unix.InotifyAddWatch(r.fd, name, watchMask|unix.IN_ONLYDIR|unix.IN_DONT_FOLLOW)
			if err != nil {
				return err
			}
			r.dirs[wd], r.paths[name] = name, wd
		}
		if r.single {
			return filepath.SkipDir
		}
		return nil
	})
}

func (r *relay) remove(root string) {
	for wd, name := range r.dirs {
		if name == root || strings.HasPrefix(name, root+"/") {
			unix.InotifyRmWatch(r.fd, uint32(wd))
			delete(r.dirs, wd)
			delete(r.paths, name)
		}
	}
}

func run() error {
	if len(os.Args) != 3 || (os.Args[2] != "file" && os.Args[2] != "tree") {
		return fmt.Errorf("usage: wslwatch PATH file|tree")
	}
	name, err := filepath.Abs(os.Args[1])
	if err != nil {
		return err
	}
	r := relay{single: os.Args[2] == "file", dirs: map[int]string{}, paths: map[string]int{}}
	r.root = name
	if r.single {
		r.root, r.filter = filepath.Dir(name), filepath.Base(name)
	}
	r.root, err = filepath.EvalSymlinks(r.root)
	if err != nil {
		return err
	}
	r.fd, err = unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return err
	}
	defer unix.Close(r.fd)
	if err := r.add(r.root); err != nil {
		return err
	}
	fmt.Printf("READY %d\n", os.Getpid())
	fds := []unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}, {Fd: 0, Events: unix.POLLIN | unix.POLLHUP}}
	buf := make([]byte, 65536)
	var first, last time.Time
	for {
		timeout := -1
		if !first.IsZero() {
			due := minTime(last.Add(150*time.Millisecond), first.Add(500*time.Millisecond))
			if !time.Now().Before(due) {
				fmt.Println("CHANGE")
				first, last = time.Time{}, time.Time{}
			} else {
				timeout = int(time.Until(due).Milliseconds()) + 1
			}
		}
		_, err := unix.Poll(fds, timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if fds[1].Revents != 0 {
			n, err := unix.Read(0, buf)
			if n == 0 || err != nil {
				return nil
			}
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := unix.Read(r.fd, buf)
		if errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return err
		}
		for off := 0; off+16 <= n; {
			wd := int(int32(binary.NativeEndian.Uint32(buf[off:])))
			flags := binary.NativeEndian.Uint32(buf[off+4:])
			length := int(binary.NativeEndian.Uint32(buf[off+12:]))
			off += 16
			if length > n-off {
				return fmt.Errorf("invalid inotify record")
			}
			child := strings.TrimRight(string(buf[off:off+length]), "\x00")
			off += length
			if flags&unix.IN_Q_OVERFLOW != 0 {
				r.remove(r.root)
				if err := r.add(r.root); err != nil {
					return err
				}
				first, last = time.Now(), time.Now()
				continue
			}
			parent, ok := r.dirs[wd]
			if !ok {
				continue
			}
			if flags&unix.IN_IGNORED != 0 {
				delete(r.dirs, wd)
				delete(r.paths, parent)
				continue
			}
			if parent == r.root && flags&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
				fmt.Println("CHANGE")
				return fmt.Errorf("watched directory moved or disappeared")
			}
			if child == "node_modules" || r.single && child != r.filter {
				continue
			}
			target := filepath.Join(parent, child)
			if !r.single && flags&unix.IN_ISDIR != 0 {
				if flags&(unix.IN_MOVED_FROM|unix.IN_DELETE) != 0 {
					r.remove(target)
				}
				if flags&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
					if err := r.add(target); err != nil && !os.IsNotExist(err) {
						return err
					}
				}
			}
			last = time.Now()
			if first.IsZero() {
				first = last
			}
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
