//go:build linux

package compat

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Numbers from 424 on are the same on every CPU.
const sysOpenat2 = 437

// atFdcwd as a variable: the constant is negative, and uintptr() of a
// negative constant doesn't compile, while a variable converts (wraps).
var atFdcwd = unix.AT_FDCWD

// RunOne makes one syscall with harmless arguments and returns its result:
// "ok" or the errno name. A seccomp kill never returns: the parent
// (RunAll) sees the signal. `andronix __probe one <name>` inside a distro.
func RunOne(name string) string {
	dot, _ := unix.BytePtrFromString("/")
	var r1 uintptr
	var e syscall.Errno
	switch name {
	case "statx":
		var st unix.Statx_t
		_, _, e = unix.Syscall6(unix.SYS_STATX, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), 0, unix.STATX_BASIC_STATS, uintptr(unsafe.Pointer(&st)), 0)
	case "statx_mnt_id":
		// Mount ids from statx arrived in Linux 5.8; systemd 256+ needs them
		// (or name_to_handle_at) for every path. "missing" when not given.
		var st unix.Statx_t
		_, _, e = unix.Syscall6(unix.SYS_STATX, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), 0, unix.STATX_BASIC_STATS|unix.STATX_MNT_ID, uintptr(unsafe.Pointer(&st)), 0)
		if e == 0 && st.Mask&unix.STATX_MNT_ID == 0 {
			return "missing"
		}
	case "openat2":
		how := unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC}
		r1, _, e = unix.Syscall6(sysOpenat2, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
		closeFd(r1, e)
	case "faccessat2":
		_, _, e = unix.Syscall6(unix.SYS_FACCESSAT2, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), unix.F_OK, unix.AT_EACCESS, 0, 0)
	case "fchmodat2":
		f, err := os.CreateTemp("", "andronix-probe-")
		if err != nil {
			return "setup-failed"
		}
		f.Close()
		defer os.Remove(f.Name())
		p, _ := unix.BytePtrFromString(f.Name())
		_, _, e = unix.Syscall6(unix.SYS_FCHMODAT2, uintptr(atFdcwd), uintptr(unsafe.Pointer(p)), 0o600, 0, 0, 0)
	case "close_range":
		_, _, e = unix.Syscall(unix.SYS_CLOSE_RANGE, 1000, 1000, 0)
	case "open_tree":
		r1, _, e = unix.Syscall(unix.SYS_OPEN_TREE, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), unix.OPEN_TREE_CLOEXEC)
		closeFd(r1, e)
	case "name_to_handle_at":
		// handle_bytes 0: a kernel that has it answers EOVERFLOW with the size.
		var h struct {
			bytes, typ uint32
			f          [128]byte
		}
		var mnt int32
		_, _, e = unix.Syscall6(unix.SYS_NAME_TO_HANDLE_AT, uintptr(atFdcwd), uintptr(unsafe.Pointer(dot)), uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&mnt)), 0, 0)
		if e == unix.EOVERFLOW {
			e = 0
		}
	case "memfd_create":
		n, _ := unix.BytePtrFromString("andronix-probe")
		r1, _, e = unix.Syscall(unix.SYS_MEMFD_CREATE, uintptr(unsafe.Pointer(n)), unix.MFD_CLOEXEC, 0)
		closeFd(r1, e)
	case "clone3":
		// NULL args and size 0: a kernel that has it says EINVAL, never forks.
		_, _, e = unix.Syscall(unix.SYS_CLONE3, 0, 0, 0)
		if e == unix.EINVAL {
			e = 0
		}
	case "seccomp":
		action := uint32(unix.SECCOMP_RET_KILL_PROCESS)
		_, _, e = unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, uintptr(unsafe.Pointer(&action)))
	default:
		return "unknown"
	}
	if e == 0 {
		return "ok"
	}
	return unix.ErrnoName(e)
}

func closeFd(r1 uintptr, e syscall.Errno) {
	if e == 0 {
		unix.Close(int(r1))
	}
}
