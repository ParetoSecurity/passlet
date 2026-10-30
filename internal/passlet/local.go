package passlet

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// StateDir is deliberately short enough for macOS's Unix socket path limit.
func StateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "passlet")
}

func DefaultSocket() string { return filepath.Join(StateDir(), "passlet.sock") }

func DefaultEndpoint() string {
	if endpoint := os.Getenv("PASSLET_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return "unix://" + DefaultSocket()
}

// PrivateDir refuses existing shared directories instead of changing their
// permissions. Unix account isolation does not defend against root or same-UID
// processes. Ancestor directories must be controlled by the account or root.
func PrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("private_directory_must_be_absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return CheckPrivateDir(path)
}

func CheckPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("private_directory_requires_owner_and_mode_0700")
	}
	return nil
}

func ListenUnix(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 103 {
		return nil, errors.New("socket_requires_absolute_path_at_most_103_bytes")
	}
	if err := PrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Hold an advisory lock for the entire listener lifetime, including stale
	// socket recovery. Never remove the lock file: doing so would split the lock
	// between inodes. O_NOFOLLOW prevents a lock-file symlink from being followed.
	fd, err := syscall.Open(path+".lock", syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("socket_lock_unavailable")
	}
	lock := os.NewFile(uintptr(fd), path+".lock")
	ok := false
	defer func() {
		if !ok {
			lock.Close()
		}
	}()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, errors.New("socket_lock_unsafe")
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return nil, errors.New("socket_already_in_use")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			return nil, errors.New("socket_path_unsafe")
		}
		// Also protect live sockets belonging to servers that do not use our lock.
		conn, dialErr := net.DialTimeout("unix", path, ConnectDeadline)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("socket_already_in_use")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			return nil, errors.New("socket_state_unknown")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	ok = true
	return &lockedListener{Listener: l, lock: lock}, nil
}

type lockedListener struct {
	net.Listener
	lock *os.File
	once sync.Once
}

func (l *lockedListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { l.lock.Close() })
	return err
}
